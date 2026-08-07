// Package process provides the installer's narrow subprocess boundary. It
// accepts argv arrays only and bounds command output before higher layers
// decide what may be shown to a user.
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
	"sync"
	"time"
)

const MaxOutputBytes = 1 << 20

type Result struct {
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
}

// Output is one entry in the live install feed. Kind selects which fields carry
// meaning: "command" uses Args, "output" uses Stream and Text, and "progress"
// uses Target, Received and Total.
type Output struct {
	Kind   string   `json:"kind"`
	// Agent identifies the install request that produced command/output events.
	// Progress keeps Target for runtime downloads and uses Agent for ownership.
	Agent  string   `json:"agent,omitempty"`
	Args   []string `json:"args,omitempty"`
	Stream string   `json:"stream,omitempty"`
	Text   string   `json:"text,omitempty"`
	// Target names what is being downloaded, so a listener can attribute a
	// progress event to the row that asked for it.
	Target   string `json:"target,omitempty"`
	Received int64  `json:"received,omitempty"`
	// Total is 0 when the server sends no Content-Length. A listener must then
	// show indeterminate progress rather than dividing by zero.
	Total int64 `json:"total,omitempty"`
}

// OutputListener receives each accepted chunk of a command's output.
//
// Calls are serialised: stdout and stderr are copied by separate goroutines, so
// a listener would otherwise be entered concurrently, and every caller would
// have to synchronise on its own. Implementations may append to a slice or write
// to a channel without locking.
type OutputListener func(Output)

// CopyWithProgress copies a download and reports its written byte count at a
// UI-friendly rate. total <= 0 means the response had no Content-Length.
func CopyWithProgress(destination io.Writer, source io.Reader, total int64, target string, listener OutputListener) (int64, error) {
	if listener == nil {
		return io.Copy(destination, source)
	}
	if total < 0 {
		total = 0
	}
	counter := &progressWriter{total: total, target: target, listener: listener}
	defer counter.flush()
	return io.Copy(io.MultiWriter(destination, counter), source)
}

// Reporting each 32 KB io.Copy chunk would push thousands of events through
// the Wails bridge for one desktop image.
const progressInterval = 200 * time.Millisecond

type progressWriter struct {
	received int64
	total    int64
	target   string
	last     time.Time
	listener OutputListener
}

func (w *progressWriter) Write(data []byte) (int, error) {
	w.received += int64(len(data))
	if time.Since(w.last) >= progressInterval {
		w.last = time.Now()
		w.report()
	}
	return len(data), nil
}

func (w *progressWriter) flush() {
	w.report()
}

func (w *progressWriter) report() {
	w.listener(Output{Kind: "progress", Target: w.target, Received: w.received, Total: w.total})
}

type StreamingRunner interface {
	RunWithOutput(context.Context, []string, map[string]string, time.Duration, OutputListener) (Result, error)
}

// Runner is deliberately small so install tests can assert exact argv and
// environment without starting a process.
type Runner interface {
	LookPath(string) (string, bool)
	Run(context.Context, []string, map[string]string, time.Duration) (Result, error)
}

// Launcher starts a detached child and does not wait for it. Run() is the wrong
// shape for a terminal window: the window outlives the request that opened it,
// and its output belongs to the user, not to a log pane.
type Launcher interface {
	Start(argv []string, overrides map[string]string) error
}

type OSRunner struct {
	Env    map[string]string
	Lookup func(string) (string, bool)
}

func Current() OSRunner {
	environment := environmentFromOS()
	if runtime.GOOS == "darwin" {
		if executable, err := os.Executable(); err == nil && macOSBundleExecutable(executable) {
			if path := loginShellPath(environment); path != "" {
				environment["PATH"] = path
			}
		}
	}
	return OSRunner{Env: environment}
}

func macOSBundleExecutable(path string) bool {
	return strings.Contains(filepath.ToSlash(path), ".app/Contents/MacOS/")
}

func loginShellPath(environment map[string]string) string {
	shell := strings.TrimSpace(environment["SHELL"])
	if shell == "" {
		shell = "/bin/zsh"
	}
	if !filepath.IsAbs(shell) || !executable(shell) {
		return ""
	}

	// ponytail: shell startup gets three seconds; keep the launchd PATH if a
	// user's interactive setup hangs, and add caching only if this is measurable.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, shell, "-lic", `printf '\036%s\037' "$PATH"`)
	command.Env = mergeEnvironment(environment, nil)
	command.WaitDelay = 250 * time.Millisecond
	output, _ := command.Output()
	if ctx.Err() != nil {
		return ""
	}
	start := bytes.LastIndexByte(output, 0x1e)
	if start < 0 {
		return ""
	}
	end := bytes.IndexByte(output[start+1:], 0x1f)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(string(output[start+1 : start+1+end]))
}

// WithEnvironment returns a copy of this runner that resolves commands against
// the given environment. An injected Lookup wins, so a test's resolver is not
// replaced by a PATH change.
func (r OSRunner) WithEnvironment(env map[string]string) Runner {
	if r.Lookup == nil {
		r.Env = env
	}
	return r
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

// Start launches a detached child with this runner's environment and returns as
// soon as it is running. The child keeps its own console: a terminal window is
// the point, so HideWindow is deliberately not applied here.
func (r OSRunner) Start(argv []string, overrides map[string]string) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return fmt.Errorf("process argv must not be empty")
	}
	return startDetached(argv, mergeEnvironment(r.Env, overrides))
}

func (r OSRunner) RunWithOutput(ctx context.Context, argv []string, overrides map[string]string, timeout time.Duration, listener OutputListener) (Result, error) {
	result := Result{Args: append([]string(nil), argv...), ExitCode: -1}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return result, fmt.Errorf("process argv must not be empty")
	}
	runContext := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runContext, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	command := exec.CommandContext(runContext, argv[0], argv[1:]...)
	HideWindow(command)
	command.Env = mergeEnvironment(r.Env, overrides)
	stdout := &boundedBuffer{limit: MaxOutputBytes}
	stderr := &boundedBuffer{limit: MaxOutputBytes}
	var streamLock sync.Mutex
	command.Stdout = &streamWriter{stream: "stdout", buffer: stdout, listener: listener, mu: &streamLock}
	command.Stderr = &streamWriter{stream: "stderr", buffer: stderr, listener: listener, mu: &streamLock}
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
	// Shared by the stdout and stderr writers of one command, so a listener sees
	// one chunk at a time. Each writer has its own buffer, but a lock per writer
	// would not serialise anything — the two goroutines would take different
	// locks and still enter the listener together.
	mu *sync.Mutex
}

func (w *streamWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
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
