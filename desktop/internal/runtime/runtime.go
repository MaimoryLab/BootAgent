// Package runtime holds every side effect the core is allowed to have.
//
// Nothing below this package calls exec, looks at the real environment or
// resolves the real home directory: it goes through a Runtime, and tests
// replace the fields. That is what lets the suite cover four platforms and two
// package managers without touching the machine it runs on, and it is the
// reason the Python installer reaches 100% branch coverage.
package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// Result is what a finished subprocess reports. Stdout and Stderr are captured
// rather than inherited so a failure summary can be redacted before it is
// shown or logged.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// RunOptions carries the per-call parts of a subprocess invocation. Env
// replaces the process environment rather than adding to it, so a test can
// prove that a credential was not passed through.
type RunOptions struct {
	Env     map[string]string
	Timeout time.Duration
	Dir     string
}

// Runner starts a subprocess. argv is always a list: no shell, no string
// interpolation, so a value taken from a config file or a provider response
// cannot become a command.
type Runner func(ctx context.Context, argv []string, opts RunOptions) (Result, error)

// LookupFn resolves an executable on PATH. The bool distinguishes "absent"
// from "present at an empty path", which a plain string cannot.
type LookupFn func(name string) (string, bool)

// Runtime is the injection seam. Its fields correspond one-to-one with the
// Python dataclass, so a ported test replaces the same five things.
type Runtime struct {
	Home  string
	OSID  string
	Run   Runner
	Which LookupFn
	Env   map[string]string
}

// TimeoutError reports that the subprocess was killed for exceeding its
// deadline. Callers distinguish it from a start failure because the two map to
// different error codes -- and not always the same ones: installing an Agent
// times out as TIMEOUT, while reading a checksum times out as
// AGENT_INSTALL_FAILED. Deciding that here would flatten a distinction the
// call sites make deliberately.
type TimeoutError struct {
	Argv []string
	// Timeout is the limit this call imposed, and is meaningful only when
	// FromCaller is true. A parent context expiring produces the same
	// DeadlineExceeded, and naming a limit that was never applied would send
	// whoever reads the message looking for the wrong cause.
	Timeout    time.Duration
	FromCaller bool
}

func (e *TimeoutError) Error() string {
	command := strings.Join(e.Argv, " ")
	if e.FromCaller {
		return "command exceeded its " + e.Timeout.String() + " limit: " + command
	}
	return "command cancelled by an enclosing deadline: " + command
}

// StartError reports that the subprocess could not be started at all -- the
// executable is missing, or not executable. This is not the same as a non-zero
// exit, which comes back as a Result with no error.
type StartError struct {
	Argv []string
	Err  error
}

func (e *StartError) Error() string {
	return "cannot start command: " + strings.Join(e.Argv, " ") + ": " + e.Err.Error()
}

func (e *StartError) Unwrap() error { return e.Err }

// IsTimeout reports whether err is a subprocess deadline failure.
func IsTimeout(err error) bool {
	var timeout *TimeoutError
	return errors.As(err, &timeout)
}

// IsStartFailure reports whether err is a failure to launch the subprocess.
func IsStartFailure(err error) bool {
	var start *StartError
	return errors.As(err, &start)
}

// Option adjusts what New builds. Tests pass fakes; production passes nothing
// and gets the real system.
type Option func(*Runtime)

// WithHome overrides the resolved home directory.
func WithHome(home string) Option {
	return func(r *Runtime) { r.Home = home }
}

// WithOSID overrides the detected platform, which is how one test process
// exercises macOS, Windows and Linux paths.
func WithOSID(osID string) Option {
	return func(r *Runtime) { r.OSID = osID }
}

// WithRunner replaces the subprocess runner.
func WithRunner(runner Runner) Option {
	return func(r *Runtime) { r.Run = runner }
}

// WithLookup replaces PATH resolution.
func WithLookup(lookup LookupFn) Option {
	return func(r *Runtime) { r.Which = lookup }
}

// WithEnv replaces the environment snapshot. The map is copied, so a caller
// cannot mutate the Runtime's view afterwards.
func WithEnv(env map[string]string) Option {
	return func(r *Runtime) {
		copied := make(map[string]string, len(env))
		for key, value := range env {
			copied[key] = value
		}
		r.Env = copied
	}
}

// New builds a Runtime, applying options over the real system defaults. Home is
// resolved after the options run so that WithOSID and WithEnv affect it -- a
// test setting OSID to "windows" gets Windows home resolution.
func New(opts ...Option) *Runtime {
	rt := &Runtime{
		Run:   ExecRunner,
		Which: LookPath,
	}
	for _, opt := range opts {
		opt(rt)
	}
	if rt.Env == nil {
		rt.Env = Environ()
	}
	if rt.OSID == "" {
		rt.OSID = CurrentOSID()
	}
	if rt.Home == "" {
		rt.Home = ResolveHome(rt.Env, rt.OSID)
	}
	return rt
}

// OSIDFor collapses a Go GOOS value onto the three names the manifest uses.
// Anything that is not darwin or windows is treated as linux, matching
// current_platform() in catalog.py.
//
// Taking GOOS as an argument rather than reading it is what lets one test
// process check all three mappings. Reading the constant directly would leave
// two thirds of this uncovered on any single machine, and the coverage gate
// would then depend on the CI matrix rather than on the tests.
func OSIDFor(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// CurrentOSID reports the platform this process is running on.
func CurrentOSID() string { return OSIDFor(goruntime.GOOS) }

// ArchFor reports arm64 or x64, the only two values the manifest declares.
func ArchFor(goarch string) string {
	if goarch == "arm64" {
		return "arm64"
	}
	return "x64"
}

// CurrentArch reports the architecture this process is running on.
func CurrentArch() string { return ArchFor(goruntime.GOARCH) }

// ShellFor names the shell whose syntax the credential file must use.
func ShellFor(osID string) string {
	if osID == "windows" {
		return "powershell"
	}
	return "bash"
}

// Environ snapshots the process environment.
func Environ() map[string]string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		if index := strings.IndexByte(entry, '='); index > 0 {
			values[entry[:index]] = entry[index+1:]
		}
	}
	return values
}

// ResolveHome mirrors resolve_home in catalog.py, including the order.
// ONEAGENT_HOME wins so a cleanroom can redirect everything with one variable;
// on Windows the native profile is preferred over HOME so a Git Bash session
// does not send configuration to a POSIX-style path the Agents never read.
func ResolveHome(env map[string]string, osID string) string {
	if value := env["ONEAGENT_HOME"]; value != "" {
		return expandUser(value)
	}
	if osID == "windows" {
		if value := env["USERPROFILE"]; value != "" {
			return value
		}
		if drive, path := env["HOMEDRIVE"], env["HOMEPATH"]; drive != "" && path != "" {
			return drive + path
		}
	}
	if value := env["HOME"]; value != "" {
		return value
	}
	return systemHome()
}

// systemHome is the last resort, and it deliberately tries twice.
//
// Python reaches this point through Path.home(), which falls back to the passwd
// database when HOME is unset. os.UserHomeDir reads $HOME alone and errors
// otherwise, so stopping there would make Go resolve no home in cases where
// Python resolves one -- and the difference would not surface as a clear
// failure but as a different exit code from somewhere further up. user.Current
// reads passwd, which restores the equivalence.
// The two lookups are variables so a test can reach the case where both fail.
// That case is unreachable on any machine with a passwd entry, and leaving it
// untested would mean the only branch deciding "there is nowhere to write" is
// also the only one nothing checks.
var (
	userHomeDir = os.UserHomeDir
	currentUser = user.Current
)

func systemHome() string {
	if home, err := userHomeDir(); err == nil && home != "" {
		return home
	}
	if current, err := currentUser(); err == nil && current.HomeDir != "" {
		return current.HomeDir
	}
	// Genuinely nowhere to write. The caller reports this as a missing
	// prerequisite; inventing a path would put configuration somewhere
	// arbitrary and the user would never find it.
	return ""
}

func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home := systemHome()
		if home == "" {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// LookPath is the production PATH lookup.
func LookPath(name string) (string, bool) {
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return resolved, true
}

// ExecRunner is the production subprocess runner. A non-zero exit is a Result,
// not an error: the caller decides what a failing command means, and often
// needs the captured output to say so.
func ExecRunner(ctx context.Context, argv []string, opts RunOptions) (Result, error) {
	if len(argv) == 0 {
		return Result{}, &StartError{Argv: argv, Err: errors.New("empty argv")}
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = opts.Dir
	if opts.Env != nil {
		environ := make([]string, 0, len(opts.Env))
		for key, value := range opts.Env {
			environ = append(environ, key+"="+value)
		}
		cmd.Env = environ
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	// Checked before ExitError: a killed process also reports an exit status,
	// and reading that first would hide the timeout.
	if ctx.Err() == context.DeadlineExceeded {
		// Timeout is only set when this call imposed the deadline. A parent
		// context expiring is also a deadline, and reporting opts.Timeout there
		// would name a limit that was never applied.
		return result, &TimeoutError{Argv: argv, Timeout: opts.Timeout, FromCaller: opts.Timeout > 0}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, &StartError{Argv: argv, Err: err}
}
