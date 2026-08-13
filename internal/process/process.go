// Package process provides the installer's narrow subprocess boundary. It
// accepts argv arrays only and bounds command output before higher layers
// decide what may be shown to a user.
package process

import (
	"bytes"
	"context"
	"errors"
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

// DefaultStallTimeout bounds how long a command may produce nothing before it is
// treated as hung. It replaces a wall-clock budget as the primary limit: an app
// store does not give up on a slow download, it gives up on a stopped one, and a
// fixed budget only ever punished users on slow links.
//
// Deliberately generous. `npm install -g` runs without --no-progress, and npm
// says very little on a non-TTY, so a healthy install can be quiet for tens of
// seconds while it fetches a large tarball. Tightening this to something like
// 30s would start failing exactly the installs this change exists to rescue.
const DefaultStallTimeout = 180 * time.Second

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
	Kind string `json:"kind"`
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

// CopyWithStallTimeout copies a download and fails if no bytes arrive for
// stallTimeout, reporting progress like CopyWithProgress does.
//
// io.Copy cannot be interrupted, so a body that blocks forever in Read would
// otherwise hang until the caller's wall-clock deadline. Rather than bound the
// total, this bounds the gap between reads: a slow-but-moving transfer runs as
// long as it needs, and a dead one ends at the first idle window.
//
// stallTimeout <= 0 disables the check and copies straight through, so callers
// with their own bound keep the previous behaviour. listener may be nil.
func CopyWithStallTimeout(ctx context.Context, destination io.Writer, source io.Reader, total int64, target string, listener OutputListener, stallTimeout time.Duration) (int64, error) {
	if stallTimeout <= 0 {
		return CopyWithProgress(destination, source, total, target, listener)
	}
	tracked := &stallReader{ctx: ctx, source: source, timeout: stallTimeout}
	tracked.touch()
	stop := tracked.watch()
	defer stop()
	written, err := CopyWithProgress(destination, tracked, total, target, listener)
	// The reader records why it was interrupted, which is more specific than the
	// generic "read failed" io.Copy surfaces.
	if reason := tracked.reason(); reason != nil {
		return written, reason
	}
	return written, err
}

// ErrStalled reports a transfer or command that stopped producing bytes. It is
// distinct from context.DeadlineExceeded so a caller can tell "the network went
// quiet" from "the overall budget ran out".
var ErrStalled = errors.New("transfer stalled: no data received within the stall timeout")

// StallGuardedBody applies the same stall bound as CopyWithStallTimeout to a
// body the caller does not copy itself.
//
// CopyWithStallTimeout owns the copy loop, which is no use when the reading
// happens somewhere we do not control -- a dependency that takes an
// *http.Client and streams the response internally. Wrapping the body is the
// only hook left, so this hands back a ReadCloser that fails with ErrStalled
// once no bytes have arrived for stallTimeout, and reports the context's error
// if that is cancelled first.
//
// Closing the returned value stops the watchdog and closes body. stallTimeout
// <= 0 returns body unchanged, so a caller with its own bound is unaffected.
func StallGuardedBody(ctx context.Context, body io.ReadCloser, stallTimeout time.Duration) io.ReadCloser {
	if body == nil || stallTimeout <= 0 {
		return body
	}
	tracked := &stallReader{ctx: ctx, source: body, timeout: stallTimeout}
	tracked.touch()
	return &guardedBody{tracked: tracked, body: body, stop: tracked.watch()}
}

type guardedBody struct {
	tracked *stallReader
	body    io.ReadCloser
	stop    func()
	once    sync.Once
}

func (g *guardedBody) Read(buffer []byte) (int, error) { return g.tracked.Read(buffer) }

func (g *guardedBody) Close() error {
	// Once, because the watchdog's stop channel is closed rather than sent on,
	// and http.Client may close a body more than once on the error paths.
	g.once.Do(g.stop)
	return g.body.Close()
}

// stallReader wraps a body so a watchdog can observe whether reads are still
// arriving. It does not interrupt Read itself -- that is impossible for an
// arbitrary io.Reader -- it closes the underlying body when one is available,
// which is what unblocks a stalled HTTP read.
type stallReader struct {
	ctx     context.Context
	source  io.Reader
	timeout time.Duration

	mu       sync.Mutex
	last     time.Time
	stalled  bool
	canceled bool
}

func (r *stallReader) Read(buffer []byte) (int, error) {
	// Checked before the read so an already-tripped watchdog stops the copy even
	// if the body happens to return buffered bytes.
	if reason := r.reason(); reason != nil {
		return 0, reason
	}
	n, err := r.source.Read(buffer)
	if n > 0 {
		r.touch()
	}
	if reason := r.reason(); reason != nil {
		return n, reason
	}
	return n, err
}

func (r *stallReader) touch() {
	r.mu.Lock()
	r.last = time.Now()
	r.mu.Unlock()
}

func (r *stallReader) reason() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stalled {
		return ErrStalled
	}
	if r.canceled {
		return r.ctx.Err()
	}
	return nil
}

// watch polls instead of arming a timer per read: a 50 MB download is hundreds
// of thousands of reads, and resetting a timer on each one costs more than
// checking a timestamp a few times a second.
func (r *stallReader) watch() func() {
	done := make(chan struct{})
	// Checking several times per stall window keeps the overshoot small without
	// making the poll itself noticeable.
	interval := max(r.timeout/4, 50*time.Millisecond)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-r.ctx.Done():
				r.mu.Lock()
				r.canceled = true
				r.mu.Unlock()
				r.interrupt()
				return
			case <-ticker.C:
				r.mu.Lock()
				idle := time.Since(r.last)
				r.mu.Unlock()
				if idle < r.timeout {
					continue
				}
				r.mu.Lock()
				r.stalled = true
				r.mu.Unlock()
				r.interrupt()
				return
			}
		}
	}()
	return func() { close(done) }
}

// interrupt unblocks a Read that is parked in the network stack. Closing the
// body is the only thing that does that; a flag alone would not be seen until
// the read returned on its own, which is the case we are trying to escape.
func (r *stallReader) interrupt() {
	if closer, ok := r.source.(io.Closer); ok {
		_ = closer.Close()
	}
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
	// StallTimeout overrides DefaultStallTimeout. Zero means the default; a
	// negative value disables stall detection. Tests set it so they can assert
	// the behaviour without waiting minutes.
	StallTimeout time.Duration
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
// BootAgent process PATH. That distinction is what makes a runtime installed
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
	// A separate cancel for the watchdog, so a stall ends the command without
	// waiting for the wall-clock budget the deadline above still enforces.
	runContext, stopForStall := context.WithCancel(runContext)
	defer stopForStall()
	command := exec.CommandContext(runContext, argv[0], argv[1:]...)
	HideWindow(command)
	command.Env = mergeEnvironment(r.Env, overrides)
	stdout := &boundedBuffer{limit: MaxOutputBytes}
	stderr := &boundedBuffer{limit: MaxOutputBytes}
	var streamLock sync.Mutex
	activity := &activityClock{}
	activity.touch()
	command.Stdout = &streamWriter{stream: "stdout", buffer: stdout, listener: listener, mu: &streamLock, activity: activity}
	command.Stderr = &streamWriter{stream: "stderr", buffer: stderr, listener: listener, mu: &streamLock, activity: activity}
	stalled := watchForStall(runContext, activity, r.stallTimeout(), stopForStall)
	err := command.Run()
	stalled.stop()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	// Reported ahead of the generic "killed" error, because the caller cannot
	// otherwise distinguish a stall from a user cancellation: both arrive as a
	// cancelled context on the same ctx. The captured output goes back with it --
	// whatever the command said before going quiet is the only clue to where it
	// got stuck, and a bare error would strip exactly the diagnostic this change
	// exists to provide.
	if stalled.tripped() {
		return result, ErrStalled
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

// activityClock records when a command last produced output. Separate from
// streamWriter because stdout and stderr each have their own writer but share
// one liveness signal: output on either stream means the command is alive.
type activityClock struct {
	mu   sync.Mutex
	last time.Time
}

func (c *activityClock) touch() {
	c.mu.Lock()
	c.last = time.Now()
	c.mu.Unlock()
}

func (c *activityClock) idle() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.last)
}

type stallWatch struct {
	done    chan struct{}
	fired   chan struct{}
	stopped sync.Once
}

func (w *stallWatch) stop() { w.stopped.Do(func() { close(w.done) }) }

func (w *stallWatch) tripped() bool {
	select {
	case <-w.fired:
		return true
	default:
		return false
	}
}

// watchForStall cancels a command that has produced nothing for timeout. A
// zero or negative timeout disables the watchdog.
func watchForStall(ctx context.Context, activity *activityClock, timeout time.Duration, cancel context.CancelFunc) *stallWatch {
	watch := &stallWatch{done: make(chan struct{}), fired: make(chan struct{})}
	if timeout <= 0 {
		return watch
	}
	interval := max(timeout/4, 50*time.Millisecond)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-watch.done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if activity.idle() < timeout {
					continue
				}
				close(watch.fired)
				cancel()
				return
			}
		}
	}()
	return watch
}

func (r OSRunner) stallTimeout() time.Duration {
	if r.StallTimeout != 0 {
		return r.StallTimeout
	}
	return DefaultStallTimeout
}

type streamWriter struct {
	stream   string
	buffer   *boundedBuffer
	listener OutputListener
	// Shared by the stdout and stderr writers of one command, so a listener sees
	// one chunk at a time. Each writer has its own buffer, but a lock per writer
	// would not serialise anything — the two goroutines would take different
	// locks and still enter the listener together.
	mu       *sync.Mutex
	activity *activityClock
}

func (w *streamWriter) Write(data []byte) (int, error) {
	// Recorded before anything can discard the data, and unconditionally. Once
	// boundedBuffer hits MaxOutputBytes it accepts nothing and the listener stops
	// being called, so keying liveness off either of those would report a
	// chatty-but-healthy command as stalled the moment it passed 1 MB.
	if w.activity != nil {
		w.activity.touch()
	}
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
