//go:build !windows

package process

import "os/exec"

func startDetached(argv []string, env []string) error {
	command := exec.Command(argv[0], argv[1:]...)
	command.Env = env
	if err := command.Start(); err != nil {
		return err
	}
	// Nothing waits on this child, so reap it in the background rather than
	// leaving a zombie for the lifetime of the desktop process.
	go func() { _ = command.Wait() }()
	return nil
}
