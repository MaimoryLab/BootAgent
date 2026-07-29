package testutil

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

func run(t *testing.T, runner *RecordingRunner, argv ...string) runtime.Result {
	t.Helper()
	result, err := runner.Runner()(context.Background(), argv, runtime.RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error running %v: %v", argv, err)
	}
	return result
}

func TestTheIntegrityResponderAnswersFromTheRealManifest(t *testing.T) {
	// The value has to come from the manifest, not a constant: a responder
	// returning generic success would let a broken comparison pass, which is
	// how the Python suite once went green while the check was wrong.
	lock := LoadLock(t)
	codex := lock.Agents["codex"]
	if codex.Package == nil || codex.Package.Integrity == "" {
		t.Fatal("the manifest records no integrity for codex")
	}

	runner := NewRecordingRunner(t, NpmIntegrityResponder(t))
	spec := codex.Package.Name + "@" + codex.Package.Version
	result := run(t, runner, "npm", "view", spec, "dist.integrity", "--registry=https://registry.npmjs.org")

	if got := trim(result.Stdout); got != codex.Package.Integrity {
		t.Fatalf("integrity = %q, want %q", got, codex.Package.Integrity)
	}
}

func TestTheIntegrityResponderHandlesScopedPackageNames(t *testing.T) {
	// @openai/codex@0.145.0 has two @ signs; splitting on the first would ask
	// about a package called "" and quietly answer 404.
	runner := NewRecordingRunner(t, NpmIntegrityResponder(t))
	lock := LoadLock(t)
	for id, agent := range lock.Agents {
		if agent.Package == nil || agent.Package.Manager != "npm" {
			continue
		}
		spec := agent.Package.Name + "@" + agent.Package.Version
		result := run(t, runner, "npm", "view", spec, "dist.integrity")
		if trim(result.Stdout) != agent.Package.Integrity {
			t.Errorf("%s: integrity = %q, want %q", id, trim(result.Stdout), agent.Package.Integrity)
		}
	}
}

func TestAnUnknownPackageAnswersLikeNpmDoesRatherThanSucceeding(t *testing.T) {
	runner := NewRecordingRunner(t, NpmIntegrityResponder(t))
	result := run(t, runner, "npm", "view", "not-a-real-package@1.0.0", "dist.integrity")
	if result.ExitCode == 0 {
		t.Fatal("an unknown package must not answer with success")
	}
}

func TestAnUnhandledCommandFailsTheTestRatherThanPassing(t *testing.T) {
	// The guarantee this package exists for: the core issuing a command nobody
	// anticipated is a finding, not a detail to smooth over.
	inner := NewFakeReporter(nil)
	runner := NewRecordingRunner(inner)
	_, _ = runner.Runner()(context.Background(), []string{"npm", "install", "-g", "surprise"}, runtime.RunOptions{})
	if !inner.Failed() {
		t.Fatal("an unanticipated command should fail the test, not return success")
	}
	if !strings.Contains(inner.Messages()[0], "npm install -g surprise") {
		t.Errorf("the failure should name the command; got %q", inner.Messages()[0])
	}
}

func TestAFallbackAllowsDeliberatelyLooseTests(t *testing.T) {
	runner := NewRecordingRunner(t)
	runner.Fallback = func(_ []string) (runtime.Result, error, bool) {
		return runtime.Result{Stdout: "ok\n"}, nil, true
	}
	if got := run(t, runner, "anything", "at", "all").Stdout; got != "ok\n" {
		t.Fatalf("stdout = %q, want the fallback answer", got)
	}
}

func TestLaterRespondersOverrideEarlierOnes(t *testing.T) {
	runner := NewRecordingRunner(t, Succeed("first\n", "npm"))
	runner.Respond(Succeed("second\n", "npm"))
	if got := run(t, runner, "npm", "--version").Stdout; got != "second\n" {
		t.Fatalf("stdout = %q, want the later responder to win", got)
	}
}

func TestEveryCallRecordsItsOwnEnvironment(t *testing.T) {
	// Which variables one specific command saw is the assertion that matters,
	// because the credential reaches an Agent through the environment.
	runner := NewRecordingRunner(t, Succeed("ok\n", "npm"))
	fn := runner.Runner()
	_, _ = fn(context.Background(), []string{"npm", "install"}, runtime.RunOptions{Env: map[string]string{"A": "1"}})
	_, _ = fn(context.Background(), []string{"npm", "view"}, runtime.RunOptions{Env: map[string]string{"B": "2"}})

	calls := runner.Calls()
	if len(calls) != 2 {
		t.Fatalf("recorded %d calls, want 2", len(calls))
	}
	if calls[0].Env["A"] != "1" || calls[0].Env["B"] != "" {
		t.Errorf("first call env = %v, want only A", calls[0].Env)
	}
	if calls[1].Env["B"] != "2" || calls[1].Env["A"] != "" {
		t.Errorf("second call env = %v, want only B", calls[1].Env)
	}
}

func TestRecordedArgvIsCopiedSoLaterMutationCannotRewriteHistory(t *testing.T) {
	runner := NewRecordingRunner(t, Succeed("ok\n", "npm"))
	argv := []string{"npm", "install", "pkg"}
	_, _ = runner.Runner()(context.Background(), argv, runtime.RunOptions{})
	argv[2] = "rewritten"
	if got := runner.Calls()[0].Argv[2]; got != "pkg" {
		t.Fatalf("recorded argv = %q, want the value at call time", got)
	}
}

func TestAssertNoCallContainsCatchesACredentialOnACommandLine(t *testing.T) {
	inner := NewFakeReporter(nil)
	runner := NewRecordingRunner(inner, Succeed("ok\n", "npm"))
	_, _ = runner.Runner()(context.Background(), []string{"npm", "config", "set", "//x/:_authToken=sk-leaked"}, runtime.RunOptions{})
	runner.AssertNoCallContains(inner, "sk-leaked")
	if !inner.Failed() {
		t.Fatal("a credential on a command line must fail the assertion")
	}
}

func TestAssertNoCallContainsPassesWhenTheSecretIsAbsent(t *testing.T) {
	// The counterpart matters: an assertion that always fails is as useless as
	// one that always passes.
	inner := NewFakeReporter(nil)
	runner := NewRecordingRunner(inner, Succeed("ok\n", "npm"))
	_, _ = runner.Runner()(context.Background(), []string{"npm", "install", "-g", "pkg"}, runtime.RunOptions{})
	runner.AssertNoCallContains(inner, "sk-leaked")
	if inner.Failed() {
		t.Fatalf("clean commands should pass; got %v", inner.Messages())
	}
}

func TestAssertNoCallContainsRefusesAnEmptySecret(t *testing.T) {
	// Searching for "" matches every command, so the assertion would report a
	// leak that is not there -- or, depending on the order of checks, pass
	// vacuously. Either way it proves nothing and should be refused.
	inner := NewFakeReporter(nil)
	NewRecordingRunner(inner).AssertNoCallContains(inner, "")
	if !inner.Failed() {
		t.Fatal("an empty secret should be refused rather than silently passing")
	}
}

func TestFindCallMatchesOnAllFragments(t *testing.T) {
	runner := NewRecordingRunner(t, Succeed("ok\n", "npm"))
	fn := runner.Runner()
	_, _ = fn(context.Background(), []string{"npm", "install", "-g", "@openai/codex@0.145.0"}, runtime.RunOptions{})
	if _, found := runner.FindCall("npm", "install", "@openai/codex"); !found {
		t.Error("expected to find the install call")
	}
	if _, found := runner.FindCall("npm", "uninstall"); found {
		t.Error("matched a call that was never made")
	}
}

func TestFailAndErrorRespondersReachTheFailureBranches(t *testing.T) {
	runner := NewRecordingRunner(t,
		Fail(1, "npm ERR! 404", "npm", "view"),
		Error(&runtime.TimeoutError{Argv: []string{"npm", "install"}}, "npm", "install"),
	)
	fn := runner.Runner()

	result, err := fn(context.Background(), []string{"npm", "view", "x"}, runtime.RunOptions{})
	if err != nil || result.ExitCode != 1 || result.Stderr == "" {
		t.Errorf("view: result=%+v err=%v, want a non-zero exit with stderr", result, err)
	}

	_, err = fn(context.Background(), []string{"npm", "install", "x"}, runtime.RunOptions{})
	if !runtime.IsTimeout(err) {
		t.Errorf("install: err=%v, want a timeout", err)
	}
}

func TestFakeLookupDistinguishesAbsentFromPresent(t *testing.T) {
	lookup := FakeLookup(map[string]string{"npm": "/usr/bin/npm"})
	if path, found := lookup("npm"); !found || path != "/usr/bin/npm" {
		t.Errorf("npm: path=%q found=%v", path, found)
	}
	// Reaching the missing-prerequisite branches depends on this.
	if _, found := lookup("uv"); found {
		t.Error("uv should be absent from this lookup")
	}
}

func TestStandardToolsCoversTheManagersTheManifestNames(t *testing.T) {
	lookup := StandardTools()
	for _, name := range []string{"npm", "uv", "icacls"} {
		if _, found := lookup(name); !found {
			t.Errorf("%s should be present on a standard test machine", name)
		}
	}
}

func TestRepoRootFindsTheManifestWithoutEncodingPackageDepth(t *testing.T) {
	// Walking up rather than counting ".." means a package can move without
	// every test in it needing an edit.
	root := RepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "agents.lock.json")); err != nil {
		t.Fatalf("RepoRoot returned %q, which holds no manifest: %v", root, err)
	}
}

func TestTheFakeReporterStopsWhereARealTestWould(t *testing.T) {
	// Without this, a double would keep running past the point a real test
	// would have aborted, and any assertion after it would be meaningless.
	reached := false
	stopped := false
	func() {
		defer func() { recover() }()
		reporter := NewFakeReporter(func() { stopped = true; panic("halt") })
		reporter.Fatalf("stop here")
		reached = true
	}()
	if !stopped {
		t.Error("Fatal should run the stop function")
	}
	if reached {
		t.Error("execution continued past Fatalf")
	}
}

func trim(text string) string {
	for len(text) > 0 && (text[len(text)-1] == '\n' || text[len(text)-1] == '\r') {
		text = text[:len(text)-1]
	}
	return text
}
