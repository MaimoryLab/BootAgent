//go:build unix

package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The shape behind the reported failure: the command exits 0 but leaves a
// process holding its stdout. Run used to read the pipe until that process
// ended, so a finished `npm update -g` sat until the stall watchdog called the
// successful install stalled.
func TestOSRunnerReturnsWhenACommandExitsButLeavesItsPipesOpen(t *testing.T) {
	pidFile := leftoverPIDFile(t)
	runner := OSRunner{StallTimeout: 3 * time.Second, PipeDrainDelay: 200 * time.Millisecond}
	started := time.Now()
	result, err := runner.Run(context.Background(), []string{"/bin/sh", "-c", `echo updated; sleep 30 & echo $! > "$1"; exit 0`, "sh", pidFile}, nil, time.Minute)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: the command itself exited 0", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "updated") {
		t.Errorf("stdout = %q, want the output written before the command exited", result.Stdout)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Run() took %s; it waited on the process left behind instead of the command", elapsed.Round(time.Millisecond))
	}
}

// The reported failure in its narrowest form: quiet for most of the stall
// timeout, then exit 0 with a leftover process holding the pipes. The stall
// timeout runs out during the drain delay, after the command has finished. A
// finished command is a success, not a stall.
func TestOSRunnerDoesNotCallAFinishedCommandStalledDuringTheDrain(t *testing.T) {
	pidFile := leftoverPIDFile(t)
	runner := OSRunner{StallTimeout: 1200 * time.Millisecond, PipeDrainDelay: 2 * time.Second}
	result, err := runner.Run(context.Background(), []string{"/bin/sh", "-c", `sleep 0.9; sleep 30 & echo $! > "$1"; exit 0`, "sh", pidFile}, nil, time.Minute)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: the command exited 0 before the stall timeout", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
}

// A failing command keeps its own exit code even when it, too, left something
// holding the pipes: the drain delay must not turn a failure into success.
func TestOSRunnerKeepsAFailureExitCodeWhenPipesAreLeftOpen(t *testing.T) {
	pidFile := leftoverPIDFile(t)
	runner := OSRunner{StallTimeout: 3 * time.Second, PipeDrainDelay: 200 * time.Millisecond}
	result, err := runner.Run(context.Background(), []string{"/bin/sh", "-c", `sleep 30 & echo $! > "$1"; exit 7`, "sh", pidFile}, nil, time.Minute)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil with the exit code reported", err)
	}
	if result.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", result.ExitCode)
	}
}

// A stall has to stop everything the command started. Killing only the leader
// left `sleep` running with the pipe open, so Run returned when it finished --
// for npm, when the install the user had already been told failed completed
// anyway, racing whatever they retried.
func TestOSRunnerStopsTheWholeTreeWhenACommandStalls(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	runner := OSRunner{StallTimeout: 300 * time.Millisecond, PipeDrainDelay: 200 * time.Millisecond}
	started := time.Now()
	_, err := runner.Run(context.Background(), []string{"/bin/sh", "-c", `sleep 30 & echo $! > "$1"; wait`, "sh", pidFile}, nil, time.Minute)
	elapsed := time.Since(started)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("Run() error = %v, want ErrStalled", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Run() took %s after a 300ms stall; it waited on a process it should have stopped", elapsed.Round(time.Millisecond))
	}
	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("read child pid: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Fatalf("parse child pid %q: %v", data, convErr)
	}
	if !processGone(pid, 2*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("child %d outlived the stalled command", pid)
	}
}

// Cancellation by the user reaches the same tree as a stall does.
func TestOSRunnerStopsTheWholeTreeWhenCancelled(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for range 100 {
			if _, err := os.Stat(pidFile); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	runner := OSRunner{StallTimeout: -1, PipeDrainDelay: 200 * time.Millisecond}
	_, err := runner.Run(ctx, []string{"/bin/sh", "-c", `sleep 30 & echo $! > "$1"; wait`, "sh", pidFile}, nil, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("read child pid: %v", readErr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if !processGone(pid, 2*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("child %d outlived the cancelled command", pid)
	}
}

// leftoverPIDFile names a file the command writes the PID of the process it
// deliberately leaves behind into. That process outlives the command by
// design -- a normal exit must not reach it -- so the test stops it itself.
func leftoverPIDFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "leftover.pid")
	t.Cleanup(func() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return path
}

// processGone reports whether pid stopped existing within wait. A killed child
// of an exited shell is reparented and reaped by init, so it disappears rather
// than lingering as a zombie of this test.
func processGone(pid int, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
