//go:build unix

package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// processTree makes cancellation reach every process a command starts, not only
// the one exec started. The command leads its own process group, so the group
// ID is its PID, and a cancelled command is killed as a group.
//
// Without this a cancelled `sh -c 'x & wait'`, or an npm whose postinstall
// script forked, loses only its leader: the orphans keep running and keep the
// stdout pipe open, so Wait blocks until they finish on their own.
type processTree struct {
	command *exec.Cmd
}

func newProcessTree(command *exec.Cmd) *processTree {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setpgid = true
	tree := &processTree{command: command}
	command.Cancel = tree.terminate
	return tree
}

// attach has nothing to do here: the group exists from the moment Start forks.
func (t *processTree) attach() {}

func (t *processTree) release() {}

func (t *processTree) terminate() error {
	process := t.command.Process
	if process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		// The whole group is already gone, which Wait reads as "exited on its
		// own" rather than as a failed cancellation.
		return os.ErrProcessDone
	}
	if err != nil {
		// A group the kernel would not signal still leaves the leader, which is
		// what the default Cancel kills.
		return process.Kill()
	}
	return nil
}
