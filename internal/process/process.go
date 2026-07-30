// Package process provides the narrow subprocess boundary shared by the Go
// installer and CLI. It accepts argv arrays only and keeps command output
// bounded before higher layers decide what may be shown to a user.
package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const MaxOutputBytes = 1 << 20

type Result struct {
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
}

// Runner is deliberately small so install tests can assert exact argv and
// environment without starting a process.
type Runner interface {
	LookPath(string) (string, bool)
	Run(context.Context, []string, map[string]string, time.Duration) (Result, error)
}

type OSRunner struct {
	Env    map[string]string
	Lookup func(string) (string, bool)
}

func Current() OSRunner {
	return OSRunner{Env: environmentFromOS()}
}

func New(env map[string]string) OSRunner {
	values := make(map[string]string, len(env))
	for key, value := range env {
		values[key] = value
	}
	return OSRunner{Env: values}
}

func (r OSRunner) LookPath(command string) (string, bool) {
	if r.Lookup != nil {
		return r.Lookup(command)
	}
	path, err := exec.LookPath(command)
	return path, err == nil
}

func (r OSRunner) Run(ctx context.Context, argv []string, overrides map[string]string, timeout time.Duration) (Result, error) {
	result := Result{Args: append([]string(nil), argv...), ExitCode: -1}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return result, fmt.Errorf("process argv must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runContext := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runContext, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	command := exec.CommandContext(runContext, argv[0], argv[1:]...)
	command.Env = mergeEnvironment(r.Env, overrides)
	stdout := &boundedBuffer{limit: MaxOutputBytes}
	stderr := &boundedBuffer{limit: MaxOutputBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	if runErr := runContext.Err(); runErr != nil {
		return result, runErr
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, err
	}
	if result.ExitCode < 0 {
		result.ExitCode = 0
	}
	return result, nil
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.limit <= 0 {
		return len(data), nil
	}
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		b.truncated = true
		return len(data), nil
	}
	_, _ = b.buffer.Write(data)
	return len(data), nil
}

func (b *boundedBuffer) String() string {
	value := b.buffer.String()
	if b.truncated {
		value += "\n[output truncated]"
	}
	return value
}

func mergeEnvironment(base, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for key, value := range base {
		values[key] = value
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func environmentFromOS() map[string]string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

var _ io.Writer = (*boundedBuffer)(nil)
