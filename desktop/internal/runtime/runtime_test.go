package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"
)

func goruntimeGOOS() string { return goruntime.GOOS }

func TestResolveHomePrefersOneAgentHomeOverEverything(t *testing.T) {
	// A cleanroom redirects the whole application with this one variable, so it
	// has to win even when the native profile is also set.
	env := map[string]string{
		"ONEAGENT_HOME": "/tmp/explicit",
		"HOME":          "/home/real",
		"USERPROFILE":   `C:\Users\real`,
	}
	for _, osID := range []string{"linux", "macos", "windows"} {
		if got := ResolveHome(env, osID); got != "/tmp/explicit" {
			t.Errorf("os=%s home=%q, want the explicit override", osID, got)
		}
	}
}

func TestWindowsPrefersTheNativeProfileOverHome(t *testing.T) {
	// Git Bash sets HOME to a POSIX-style path the Agents never read, so
	// honouring it would write configuration where nothing looks for it.
	env := map[string]string{"HOME": "/c/Users/real", "USERPROFILE": `C:\Users\real`}
	if got := ResolveHome(env, "windows"); got != `C:\Users\real` {
		t.Fatalf("home = %q, want the native profile", got)
	}
	if got := ResolveHome(env, "linux"); got != "/c/Users/real" {
		t.Fatalf("on linux home = %q, want HOME", got)
	}
}

func TestWindowsFallsBackToHomedriveAndHomepath(t *testing.T) {
	env := map[string]string{"HOMEDRIVE": "D:", "HOMEPATH": `\Users\real`}
	if got := ResolveHome(env, "windows"); got != `D:\Users\real` {
		t.Fatalf("home = %q, want the drive and path joined", got)
	}
	// One half alone is not enough to build a path from.
	if got := ResolveHome(map[string]string{"HOMEDRIVE": "D:", "HOME": "/fallback"}, "windows"); got != "/fallback" {
		t.Fatalf("home = %q, want HOME when HOMEPATH is missing", got)
	}
}

func TestResolveHomeFallsBackToTheUserDirectoryWhenNothingIsSet(t *testing.T) {
	want, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home directory in this environment")
	}
	if got := ResolveHome(map[string]string{}, "linux"); got != want {
		t.Fatalf("home = %q, want %q", got, want)
	}
}

func TestNoHomeAnywhereYieldsEmptyRatherThanAGuessedPath(t *testing.T) {
	// os.UserHomeDir reads $HOME on POSIX, so clearing it reaches the last
	// fallback. Returning "" lets the caller report a real prerequisite
	// failure; inventing a path would write configuration somewhere arbitrary.
	if goruntimeGOOS() == "windows" {
		t.Skip("UserHomeDir reads the profile variables on Windows")
	}
	t.Setenv("HOME", "")
	if got := ResolveHome(map[string]string{}, "linux"); got != "" {
		t.Fatalf("home = %q, want empty when nothing declares one", got)
	}
}

func TestATildeThatCannotBeExpandedIsLeftAloneRatherThanMangled(t *testing.T) {
	if goruntimeGOOS() == "windows" {
		t.Skip("UserHomeDir reads the profile variables on Windows")
	}
	t.Setenv("HOME", "")
	// "~/x" with no home to expand against stays as written, so the failure
	// surfaces as a path that obviously was not resolved.
	if got := ResolveHome(map[string]string{"ONEAGENT_HOME": "~/x"}, "linux"); got != "~/x" {
		t.Fatalf("home = %q, want the unexpanded value", got)
	}
	if got := ResolveHome(map[string]string{"ONEAGENT_HOME": "~"}, "linux"); got != "~" {
		t.Fatalf("home = %q, want the unexpanded value", got)
	}
}

func TestABareTildeExpandsToTheHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home directory in this environment")
	}
	if got := ResolveHome(map[string]string{"ONEAGENT_HOME": "~"}, "linux"); got != home {
		t.Fatalf("home = %q, want %q", got, home)
	}
}

func TestAPathWithNoTildeIsUntouched(t *testing.T) {
	if got := ResolveHome(map[string]string{"ONEAGENT_HOME": "/absolute/path"}, "linux"); got != "/absolute/path" {
		t.Fatalf("home = %q, want the path unchanged", got)
	}
}

func TestATildeInOneAgentHomeIsExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home directory in this environment")
	}
	got := ResolveHome(map[string]string{"ONEAGENT_HOME": "~/oneagent-test"}, "linux")
	if want := filepath.Join(home, "oneagent-test"); got != want {
		t.Fatalf("home = %q, want %q", got, want)
	}
}

func TestPlatformNamesCollapseToTheThreeTheManifestUses(t *testing.T) {
	// Every mapping is checked in one process. Reading GOOS directly would make
	// the coverage gate depend on the CI matrix instead of on these cases.
	for goos, want := range map[string]string{
		"darwin":  "macos",
		"windows": "windows",
		"linux":   "linux",
		// An unfamiliar platform gets the POSIX paths rather than an error,
		// which is what catalog.py does.
		"freebsd": "linux",
		"":        "linux",
	} {
		if got := OSIDFor(goos); got != want {
			t.Errorf("OSIDFor(%q) = %q, want %q", goos, got, want)
		}
	}
	if got := CurrentOSID(); got != OSIDFor(goruntimeGOOS()) {
		t.Errorf("CurrentOSID() = %q, inconsistent with OSIDFor", got)
	}
}

func TestArchCollapsesToTheTwoValuesTheManifestDeclares(t *testing.T) {
	for goarch, want := range map[string]string{
		"arm64": "arm64",
		"amd64": "x64",
		"386":   "x64",
	} {
		if got := ArchFor(goarch); got != want {
			t.Errorf("ArchFor(%q) = %q, want %q", goarch, got, want)
		}
	}
	if arch := CurrentArch(); arch != "arm64" && arch != "x64" {
		t.Errorf("CurrentArch() = %q, want arm64 or x64", arch)
	}
}

func TestShellFollowsThePlatformBecauseTheCredentialFileMustParse(t *testing.T) {
	if ShellFor("windows") != "powershell" {
		t.Error("windows must use powershell syntax for the credential file")
	}
	for _, osID := range []string{"macos", "linux", "freebsd"} {
		if ShellFor(osID) != "bash" {
			t.Errorf("os=%s should use bash syntax", osID)
		}
	}
}

func TestEnvironSnapshotsTheProcessEnvironment(t *testing.T) {
	t.Setenv("ONEAGENT_ENVIRON_PROBE", "value=with=equals")
	env := Environ()
	if env["ONEAGENT_ENVIRON_PROBE"] != "value=with=equals" {
		t.Fatalf("probe = %q, want everything after the first = kept", env["ONEAGENT_ENVIRON_PROBE"])
	}
}

func TestTheInjectionOptionsReplaceTheRealSystem(t *testing.T) {
	// The whole point of the package: nothing below it touches the machine.
	called := false
	rt := New(
		WithHome("/injected/home"),
		WithOSID("linux"),
		WithEnv(map[string]string{}),
		WithRunner(func(_ context.Context, _ []string, _ RunOptions) (Result, error) {
			called = true
			return Result{Stdout: "faked\n"}, nil
		}),
		WithLookup(func(name string) (string, bool) { return "/fake/" + name, true }),
	)
	if rt.Home != "/injected/home" {
		t.Errorf("home = %q, want the injected value", rt.Home)
	}
	if _, _ = rt.Run(context.Background(), []string{"npm"}, RunOptions{}); !called {
		t.Error("the injected runner was not used")
	}
	if path, found := rt.Which("npm"); !found || path != "/fake/npm" {
		t.Errorf("which npm = %q/%v, want the injected lookup", path, found)
	}
}

func TestNewFallsBackToTheRealSystemWhenNothingIsInjected(t *testing.T) {
	rt := New()
	if rt.Env == nil || rt.OSID == "" || rt.Run == nil || rt.Which == nil {
		t.Fatalf("New() left a field unset: %+v", rt)
	}
}

func TestStartErrorUnwrapsToItsCause(t *testing.T) {
	cause := errors.New("permission denied")
	err := &StartError{Argv: []string{"npm"}, Err: cause}
	if !errors.Is(err, cause) {
		t.Fatal("StartError should unwrap to the underlying cause")
	}
	if err.Error() == "" {
		t.Error("the message should name the command")
	}
}

func TestNewResolvesHomeAfterOptionsApply(t *testing.T) {
	// Order matters: a test that sets OSID to windows expects Windows home
	// resolution, which only works if home is resolved last.
	rt := New(
		WithOSID("windows"),
		WithEnv(map[string]string{"USERPROFILE": `C:\Users\test`, "HOME": "/should/lose"}),
	)
	if rt.Home != `C:\Users\test` {
		t.Fatalf("home = %q, want the Windows profile", rt.Home)
	}
}

func TestWithEnvCopiesSoTheCallerCannotMutateTheRuntime(t *testing.T) {
	env := map[string]string{"HOME": "/original"}
	rt := New(WithOSID("linux"), WithEnv(env))
	env["HOME"] = "/changed"
	if rt.Env["HOME"] != "/original" {
		t.Fatal("the Runtime shares the caller's map instead of copying it")
	}
}

func TestANonZeroExitIsAResultNotAnError(t *testing.T) {
	// The caller decides what a failing command means, and usually needs the
	// captured output to say so. Returning an error here would throw that away.
	result, err := ExecRunner(context.Background(), []string{"sh", "-c", "echo out; echo err >&2; exit 3"}, RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", result.ExitCode)
	}
	if result.Stdout != "out\n" || result.Stderr != "err\n" {
		t.Errorf("stdout=%q stderr=%q, want both captured", result.Stdout, result.Stderr)
	}
}

func TestAMissingExecutableIsAStartFailure(t *testing.T) {
	// Distinct from a non-zero exit: nothing ran, so there is no output to
	// summarise and the message must name the command instead.
	_, err := ExecRunner(context.Background(), []string{"oneagent-no-such-binary-xyz"}, RunOptions{})
	if !IsStartFailure(err) {
		t.Fatalf("err = %v, want a start failure", err)
	}
	if IsTimeout(err) {
		t.Fatal("a missing executable must not be classified as a timeout")
	}
}

func TestEmptyArgvIsRefusedRatherThanPassedToExec(t *testing.T) {
	_, err := ExecRunner(context.Background(), nil, RunOptions{})
	if !IsStartFailure(err) {
		t.Fatalf("err = %v, want a start failure", err)
	}
}

func TestExceedingTheDeadlineIsATimeoutNotAnExitCode(t *testing.T) {
	// A killed process also reports an exit status. Reading that first would
	// report the timeout as an ordinary failure, and the caller would not know
	// to mark it retryable.
	_, err := ExecRunner(context.Background(), []string{"sleep", "5"}, RunOptions{Timeout: 50 * time.Millisecond})
	if !IsTimeout(err) {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if IsStartFailure(err) {
		t.Fatal("a timeout must not be classified as a start failure")
	}
}

func TestTheRunnerClassifiesTimeoutWithoutChoosingAnErrorCode(t *testing.T) {
	// Installing an Agent times out as TIMEOUT while reading a checksum times
	// out as AGENT_INSTALL_FAILED. If this package picked a code, that
	// deliberate distinction would be flattened.
	err := &TimeoutError{Argv: []string{"npm", "install"}, Timeout: time.Second}
	if IsTimeout(err) != true {
		t.Fatal("IsTimeout must recognise its own type")
	}
	if err.Error() == "" {
		t.Fatal("the message should name the command")
	}
}

func TestEnvReplacesRatherThanExtendsTheProcessEnvironment(t *testing.T) {
	// A test proves a credential was not passed through by asserting the child
	// saw exactly what was handed to it.
	t.Setenv("ONEAGENT_LEAK_CANARY", "must-not-reach-the-child")
	result, err := ExecRunner(
		context.Background(),
		[]string{"sh", "-c", "echo ${ONEAGENT_LEAK_CANARY:-absent}"},
		RunOptions{Env: map[string]string{"PATH": os.Getenv("PATH")}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "absent\n" {
		t.Fatalf("child saw %q, want the parent variable to be absent", result.Stdout)
	}
}

func TestACancelledContextStopsTheSubprocess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExecRunner(ctx, []string{"sleep", "5"}, RunOptions{}); err == nil {
		t.Fatal("a cancelled context should not run the command to completion")
	}
}

func TestLookPathReportsAbsenceSeparatelyFromAnEmptyPath(t *testing.T) {
	if _, found := LookPath("oneagent-no-such-binary-xyz"); found {
		t.Error("a missing executable must report found=false")
	}
	if path, found := LookPath("sh"); !found || path == "" {
		t.Errorf("sh: path=%q found=%v, want a resolved path", path, found)
	}
}

func TestRunOptionsDirIsHonoured(t *testing.T) {
	dir := t.TempDir()
	result, err := ExecRunner(context.Background(), []string{"pwd"}, RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// macOS reports /private/var for /var, so compare resolved paths.
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(trimNewline(result.Stdout))
	if got != want {
		t.Fatalf("pwd = %q, want %q", got, want)
	}
}

func trimNewline(text string) string {
	for len(text) > 0 && (text[len(text)-1] == '\n' || text[len(text)-1] == '\r') {
		text = text[:len(text)-1]
	}
	return text
}
