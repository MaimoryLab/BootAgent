// Package process provides the narrow subprocess boundary shared by the Go
// installer and CLI. It accepts argv arrays only and keeps command output
// bounded before higher layers decide what may be shown to a user.
package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

type Output struct {
	Kind   string   `json:"kind"`
	Args   []string `json:"args,omitempty"`
	Stream string   `json:"stream,omitempty"`
	Text   string   `json:"text,omitempty"`
}

type OutputListener func(Output)

type StreamingRunner interface {
	RunWithOutput(context.Context, []string, map[string]string, time.Duration, OutputListener) (Result, error)
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
	maps.Copy(values, env)
	return OSRunner{Env: values}
}

// LookPath resolves a command against this runner's own PATH rather than the
// OneAgent process PATH. That distinction is what makes a runtime installed
// into a private directory usable: Run() passes r.Env to the child, so a lookup
// against the parent process environment would report a managed npm or uv as
// missing and the installer would keep asking to install it again.
func (r OSRunner) LookPath(command string) (string, bool) {
	if r.Lookup != nil {
		return r.Lookup(command)
	}
	search := r.Env["PATH"]
	if search == "" {
		search = r.Env["Path"] // Windows callers may carry the native casing.
	}
	if search == "" || search == os.Getenv("PATH") {
		path, err := exec.LookPath(command)
		return path, err == nil
	}
	return lookPathIn(search, command)
}

// lookPathIn mirrors exec.LookPath's directory walk against an explicit search
// path. exec.LookPath itself always reads the process environment, so an
// injected PATH cannot be honored through it.
func lookPathIn(search, command string) (string, bool) {
	if command == "" {
		return "", false
	}
	if strings.ContainsAny(command, `/\`) {
		if executable(command) {
			return command, true
		}
		return "", false
	}
	for _, directory := range filepath.SplitList(search) {
		if directory == "" {
			directory = "."
		}
		for _, candidate := range commandNames(command) {
			path := filepath.Join(directory, candidate)
			if executable(path) {
				return path, true
			}
		}
	}
	return "", false
}

// commandNames expands a bare command to the Windows executable extensions. On
// other platforms the command name is used as given.
func commandNames(command string) []string {
	if runtime.GOOS != "windows" {
		return []string{command}
	}
	if filepath.Ext(command) != "" {
		return []string{command}
	}
	extensions := os.Getenv("PATHEXT")
	if extensions == "" {
		extensions = ".COM;.EXE;.BAT;.CMD"
	}
	names := make([]string, 0, 5)
	for _, extension := range filepath.SplitList(extensions) {
		if extension = strings.TrimSpace(extension); extension != "" {
			names = append(names, command+strings.ToLower(extension))
		}
	}
	return append(names, command)
}

func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

func (r OSRunner) Run(ctx context.Context, argv []string, overrides map[string]string, timeout time.Duration) (Result, error) {
	return r.RunWithOutput(ctx, argv, overrides, timeout, nil)
}

func (r OSRunner) RunWithOutput(ctx context.Context, argv []string, overrides map[string]string, timeout time.Duration, listener OutputListener) (Result, error) {
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
	command.Stdout = &streamWriter{stream: "stdout", buffer: stdout, listener: listener}
	command.Stderr = &streamWriter{stream: "stderr", buffer: stderr, listener: listener}
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

type streamWriter struct {
	stream   string
	buffer   *boundedBuffer
	listener OutputListener
}

func (w *streamWriter) Write(data []byte) (int, error) {
	before := w.buffer.buffer.Len()
	n, err := w.buffer.Write(data)
	accepted := w.buffer.buffer.Len() - before
	if w.listener != nil && accepted > 0 {
		w.listener(Output{Kind: "output", Stream: w.stream, Text: string(data[:accepted])})
	}
	return n, err
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
	maps.Copy(values, base)
	maps.Copy(values, overrides)
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
