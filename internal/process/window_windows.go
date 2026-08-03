package process

import (
	"os/exec"
	"syscall"
)

// createNoWindow keeps a console child from allocating its own console. The
// desktop build is a GUI binary with no console to inherit, so without this
// every npm.cmd, uv.exe or icacls call flashes a cmd window at the user.
const createNoWindow = 0x08000000

// HideWindow suppresses the console window of a child process on Windows and
// does nothing elsewhere. Output still reaches the caller through the command's
// Stdout/Stderr writers.
func HideWindow(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= createNoWindow
}
