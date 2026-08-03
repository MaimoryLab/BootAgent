package process

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestHideWindowSetsCreateNoWindow(t *testing.T) {
	command := exec.Command("cmd", "/c", "exit", "0")
	HideWindow(command)
	if command.SysProcAttr == nil {
		t.Fatal("HideWindow left SysProcAttr nil")
	}
	if command.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW set", command.SysProcAttr.CreationFlags)
	}
}

func TestHideWindowKeepsExistingFlags(t *testing.T) {
	command := exec.Command("cmd", "/c", "exit", "0")
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_UNICODE_ENVIRONMENT}
	HideWindow(command)
	if command.SysProcAttr.CreationFlags&syscall.CREATE_UNICODE_ENVIRONMENT == 0 {
		t.Fatal("HideWindow dropped a pre-existing creation flag")
	}
}
