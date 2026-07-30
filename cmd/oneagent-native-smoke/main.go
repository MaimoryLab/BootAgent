package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func main() {
	binary := flag.String("binary", "", "path to the native OneAgent desktop binary")
	timeout := flag.Duration("timeout", 20*time.Second, "maximum time to wait for GetStatus")
	flag.Parse()
	if err := run(*binary, *timeout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(binary string, timeout time.Duration) error {
	if binary == "" {
		return fmt.Errorf("-binary is required")
	}
	if runtime.GOOS == "windows" && filepath.Ext(binary) == "" {
		if _, err := os.Stat(binary + ".exe"); err == nil {
			binary += ".exe"
		}
	}
	home, err := os.MkdirTemp("", "oneagent-native-smoke-")
	if err != nil {
		return fmt.Errorf("create temporary HOME: %w", err)
	}
	defer os.RemoveAll(home)
	result := filepath.Join(home, "get-status")
	command := exec.Command(binary)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"ONEAGENT_HOME="+home,
		"ONEAGENT_NATIVE_SMOKE=1",
		"ONEAGENT_NATIVE_SMOKE_RESULT="+result,
	)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start desktop app: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("desktop app exited before GetStatus: %w", err)
		}
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-exited
		return fmt.Errorf("timed out waiting for the desktop GetStatus binding")
	}
	if content, err := os.ReadFile(result); err != nil || string(content) != "ok\n" {
		return fmt.Errorf("desktop app exited without calling GetStatus through the binding")
	}
	return nil
}
