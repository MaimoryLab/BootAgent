//go:build !windows

package process

import "os/exec"

// HideWindow is a no-op outside Windows: no other platform allocates a console
// window for a child process.
func HideWindow(*exec.Cmd) {}
