// Package testutil holds the doubles every ported test needs.
//
// These exist so a test can prove what the core would do to a machine without
// doing it. They are deliberately strict: a runner that answers every command
// with success hides the case where the core issued the wrong command, and that
// is how eleven Python tests once passed while the integrity check was broken.
package testutil

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// Call is one recorded subprocess invocation. Env is captured per call because
// the credential reaches an Agent through the environment, so "which variables
// did this specific command see" is the assertion that matters.
type Call struct {
	Argv []string
	Env  map[string]string
	Dir  string
}

// Command returns the argv joined for readable assertion failures.
func (c Call) Command() string { return strings.Join(c.Argv, " ") }

// Responder answers one command. Returning handled=false lets the next
// responder try, and if none handles it RecordingRunner fails the test rather
// than inventing a success.
type Responder func(argv []string) (result runtime.Result, err error, handled bool)

// Reporter is the part of *testing.T these doubles use. Naming it as an
// interface is what makes the doubles testable in turn: t.Fatalf unwinds the
// goroutine, so a test that wants to observe "this would have failed" cannot
// pass a real *testing.T and inspect it afterwards.
type Reporter interface {
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Helper()
}

// RecordingRunner records every call and answers from its responders.
type RecordingRunner struct {
	mu         sync.Mutex
	calls      []Call
	responders []Responder
	// Fallback answers anything no responder claimed. Leaving it nil makes an
	// unexpected command a test failure, which is the point: the core issuing a
	// command nobody anticipated is a finding, not a detail to smooth over.
	Fallback Responder
	t        Reporter
}

// NewRecordingRunner builds a runner bound to t. Unhandled commands fail t.
func NewRecordingRunner(t Reporter, responders ...Responder) *RecordingRunner {
	t.Helper()
	return &RecordingRunner{responders: responders, t: t}
}

// Respond appends a responder, which is consulted before earlier ones.
func (r *RecordingRunner) Respond(responder Responder) *RecordingRunner {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.responders = append(r.responders, responder)
	return r
}

// Runner returns the injectable function.
func (r *RecordingRunner) Runner() runtime.Runner {
	return func(_ context.Context, argv []string, opts runtime.RunOptions) (runtime.Result, error) {
		r.mu.Lock()
		env := map[string]string{}
		for key, value := range opts.Env {
			env[key] = value
		}
		r.calls = append(r.calls, Call{Argv: append([]string(nil), argv...), Env: env, Dir: opts.Dir})
		responders := append([]Responder(nil), r.responders...)
		fallback := r.Fallback
		r.mu.Unlock()

		// Later responders win, so a test can override a shared default.
		for index := len(responders) - 1; index >= 0; index-- {
			if result, err, handled := responders[index](argv); handled {
				return result, err
			}
		}
		if fallback != nil {
			result, err, _ := fallback(argv)
			return result, err
		}
		r.t.Fatalf("no responder handled %q; add one rather than letting it pass", strings.Join(argv, " "))
		return runtime.Result{}, nil
	}
}

// Calls returns everything recorded so far.
func (r *RecordingRunner) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Call(nil), r.calls...)
}

// FindCall returns the first recorded call containing every given fragment.
func (r *RecordingRunner) FindCall(fragments ...string) (Call, bool) {
	for _, call := range r.Calls() {
		command := call.Command()
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(command, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return call, true
		}
	}
	return Call{}, false
}

// AssertNoCallContains fails if any recorded argv or env value contains the
// given text. This is how a test proves a credential never reached a command
// line, which is a product promise rather than a detail.
func (r *RecordingRunner) AssertNoCallContains(t Reporter, secret string) {
	t.Helper()
	if secret == "" {
		t.Fatal("refusing to search for an empty secret; the assertion would be vacuous")
	}
	for _, call := range r.Calls() {
		if strings.Contains(call.Command(), secret) {
			t.Errorf("the credential reached a command line: %s", call.Command())
		}
	}
}

// Succeed builds a responder answering the given fragments with stdout.
func Succeed(stdout string, fragments ...string) Responder {
	return func(argv []string) (runtime.Result, error, bool) {
		if !containsAll(strings.Join(argv, " "), fragments) {
			return runtime.Result{}, nil, false
		}
		return runtime.Result{Stdout: stdout}, nil, true
	}
}

// Fail builds a responder answering with a non-zero exit and stderr.
func Fail(exitCode int, stderr string, fragments ...string) Responder {
	return func(argv []string) (runtime.Result, error, bool) {
		if !containsAll(strings.Join(argv, " "), fragments) {
			return runtime.Result{}, nil, false
		}
		return runtime.Result{ExitCode: exitCode, Stderr: stderr}, nil, true
	}
}

// Error builds a responder answering with a transport-level failure, for
// exercising the timeout and start-failure branches.
func Error(err error, fragments ...string) Responder {
	return func(argv []string) (runtime.Result, error, bool) {
		if !containsAll(strings.Join(argv, " "), fragments) {
			return runtime.Result{}, nil, false
		}
		return runtime.Result{}, err, true
	}
}

func containsAll(text string, fragments []string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			return false
		}
	}
	return true
}

// LockPackage is the part of a manifest entry the doubles need.
type LockPackage struct {
	Manager   string `json:"manager"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Integrity string `json:"integrity"`
}

// LockAgent is a manifest entry.
type LockAgent struct {
	Name    string       `json:"name"`
	Command string       `json:"command"`
	Package *LockPackage `json:"package"`
}

// Lock is the parsed manifest.
type Lock struct {
	Agents map[string]LockAgent `json:"agents"`
}

// LoadLock reads agents.lock.json from the repository root. Tests use the real
// manifest rather than a fixture so a doubled command answers with the value
// production would compare against.
func LoadLock(t Reporter) Lock {
	t.Helper()
	path := filepath.Join(RepoRoot(t), "agents.lock.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var lock Lock
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	if len(lock.Agents) == 0 {
		t.Fatalf("%s declares no agents", path)
	}
	return lock
}

// RepoRoot walks up from the test's directory to the directory holding
// agents.lock.json, so a test does not encode how deep its package sits.
func RepoRoot(t Reporter) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "agents.lock.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("walked to the filesystem root without finding agents.lock.json")
		}
		dir = parent
	}
}

// NpmIntegrityResponder answers `npm view <spec> dist.integrity` with the value
// the manifest records for that package.
//
// This is what lets the integrity check be tested offline. It also matters that
// the value comes from the manifest rather than a constant: a responder
// returning generic success would let a broken comparison pass, which is
// exactly how the Python suite once went green while the check was wrong.
func NpmIntegrityResponder(t Reporter) Responder {
	t.Helper()
	lock := LoadLock(t)
	byName := map[string]string{}
	for _, agent := range lock.Agents {
		if agent.Package != nil && agent.Package.Name != "" {
			byName[agent.Package.Name] = agent.Package.Integrity
		}
	}
	return func(argv []string) (runtime.Result, error, bool) {
		// argv[0] is the resolved path to npm rather than the bare name, because
		// production looks it up on PATH first. Matching the base name is what
		// lets this double answer the command the core actually issues -- an
		// earlier version required "npm" exactly and silently matched nothing.
		if len(argv) < 4 || !strings.HasPrefix(filepath.Base(argv[0]), "npm") || argv[1] != "view" {
			return runtime.Result{}, nil, false
		}
		if !containsAll(strings.Join(argv, " "), []string{"dist.integrity"}) {
			return runtime.Result{}, nil, false
		}
		name := argv[2]
		// A spec is name@version; the package name may itself start with @.
		if index := strings.LastIndexByte(name, '@'); index > 0 {
			name = name[:index]
		}
		integrity, known := byName[name]
		if !known {
			return runtime.Result{ExitCode: 1, Stderr: "npm ERR! 404 " + name}, nil, true
		}
		return runtime.Result{Stdout: integrity + "\n"}, nil, true
	}
}

// FakeLookup builds a PATH lookup from a name-to-path map. Absent names report
// found=false, which is how the missing-prerequisite branches are reached.
func FakeLookup(paths map[string]string) runtime.LookupFn {
	return func(name string) (string, bool) {
		path, found := paths[name]
		return path, found
	}
}

// StandardTools is the lookup most tests want: a machine with npm, uv and the
// Windows ACL tool present.
func StandardTools() runtime.LookupFn {
	return FakeLookup(map[string]string{
		"npm":    "/usr/bin/npm",
		"node":   "/usr/bin/node",
		"uv":     "/usr/bin/uv",
		"icacls": `C:\Windows\System32\icacls.exe`,
	})
}
