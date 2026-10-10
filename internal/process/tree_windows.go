package process

import (
	"os"
	"os/exec"
	"sync"

	"golang.org/x/sys/windows"
)

// processTree makes cancellation reach every process a command starts, not only
// the one exec started. Windows has no process groups to signal, so the command
// is placed in a Job Object and a cancelled command terminates the job.
//
// This matters most for npm: `npm` resolves to npm.cmd, so the process exec
// starts is cmd.exe and the install itself runs in its child node.exe. Killing
// cmd.exe left node.exe running with the stdout pipe open, and Wait blocked
// until that install finished on its own.
//
// The job is not created with KILL_ON_JOB_CLOSE. A command that exits normally
// keeps whatever it deliberately left running, as it always has; only
// cancellation reaches the whole tree.
type processTree struct {
	command *exec.Cmd

	// mu guards job: exec runs Cancel from its own goroutine, which can race the
	// assignment in attach and the close in release.
	mu  sync.Mutex
	job windows.Handle
}

func newProcessTree(command *exec.Cmd) *processTree {
	tree := &processTree{command: command}
	command.Cancel = tree.terminate
	return tree
}

// attach places the started process in a fresh job. A process started before
// the assignment is not in the job -- Go cannot start a child suspended -- but
// cmd.exe has to read and parse the batch file before it starts node.exe, which
// is far longer than the assignment takes. Any failure here leaves the tree
// handling it had before: Cancel falls back to killing the direct child.
func (t *processTree) attach() {
	if t.command.Process == nil {
		return
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(t.command.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	t.mu.Lock()
	t.job = job
	t.mu.Unlock()
}

func (t *processTree) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}

func (t *processTree) terminate() error {
	t.mu.Lock()
	job := t.job
	if job != 0 {
		err := windows.TerminateJobObject(job, 1)
		t.mu.Unlock()
		if err == nil {
			return nil
		}
	} else {
		t.mu.Unlock()
	}
	if t.command.Process == nil {
		return os.ErrProcessDone
	}
	return t.command.Process.Kill()
}
