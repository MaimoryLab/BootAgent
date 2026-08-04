//go:build windows

package process

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const shellShow = 5 // SW_SHOW

var (
	shell32       = syscall.NewLazyDLL("shell32.dll")
	shellExecuteW = shell32.NewProc("ShellExecuteW")
	// ShellExecuteW inherits the caller's environment but has no environment
	// parameter. ponytail: one process-wide lock is enough for rare launches;
	// use CreateProcessW for per-launch environment isolation if that changes.
	shellEnvironmentMu sync.Mutex
)

func startDetached(argv []string, env []string) error {
	path, hasPath := environmentValue(env, "PATH")
	if !hasPath {
		return shellExecute(argv, env)
	}

	shellEnvironmentMu.Lock()
	defer shellEnvironmentMu.Unlock()

	key, previous, present := currentEnvironmentValue("PATH")
	if key == "" {
		key = "PATH"
	}
	if err := os.Setenv(key, path); err != nil {
		return fmt.Errorf("set launch PATH: %w", err)
	}
	defer func() {
		if present {
			_ = os.Setenv(key, previous)
		} else {
			_ = os.Unsetenv(key)
		}
	}()
	return shellExecute(argv, env)
}

func shellExecute(argv []string, env []string) error {
	file := argv[0]
	if strings.EqualFold(file, "cmd") || strings.EqualFold(file, "cmd.exe") {
		if comspec, ok := environmentValue(env, "ComSpec"); ok && comspec != "" {
			file = comspec
		} else {
			file = "cmd.exe"
		}
	}
	filePtr, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		return fmt.Errorf("encode launch executable: %w", err)
	}
	operationPtr, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("encode launch operation: %w", err)
	}
	// terminalArgv already puts the command in cmd.exe syntax; CRT-escaping the
	// final argument would turn its quotes and percent expansions into literals.
	parameters := strings.Join(argv[1:], " ")
	var parametersPtr *uint16
	if parameters != "" {
		parametersPtr, err = syscall.UTF16PtrFromString(parameters)
		if err != nil {
			return fmt.Errorf("encode launch parameters: %w", err)
		}
	}
	result, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(operationPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(parametersPtr)),
		0,
		shellShow,
	)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW failed for %q (result=%d): %v", file, result, callErr)
	}
	return nil
}

func environmentValue(env []string, name string) (string, bool) {
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, name) {
			return value, true
		}
	}
	return "", false
}

func currentEnvironmentValue(name string) (key, value string, present bool) {
	for _, entry := range os.Environ() {
		currentKey, currentValue, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(currentKey, name) {
			return currentKey, currentValue, true
		}
	}
	return "", "", false
}
