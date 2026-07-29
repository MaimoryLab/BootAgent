package install

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/testutil"
)

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	var oneAgentErr *oerr.Error
	if !errors.As(err, &oneAgentErr) {
		t.Fatalf("err = %v, want an *oerr.Error", err)
	}
	if oneAgentErr.Code != want {
		t.Errorf("code = %q, want %q", oneAgentErr.Code, want)
	}
}

func agentFor(t *testing.T, id string) catalog.Agent {
	t.Helper()
	agent, present := catalog.MustLoad().Agent(id)
	if !present {
		t.Fatalf("%s is not in the manifest", id)
	}
	return agent
}

// installRuntime builds a runtime where the Agent is absent, npm and uv exist,
// and the integrity query answers from the real manifest.
func installRuntime(t *testing.T, responders ...testutil.Responder) (*runtime.Runtime, *testutil.RecordingRunner) {
	t.Helper()
	all := append([]testutil.Responder{
		testutil.NpmIntegrityResponder(t),
		testutil.Succeed("", "install"),
		testutil.Succeed("Python 3.12.9\n", "--version"),
	}, responders...)
	recorder := testutil.NewRecordingRunner(t, all...)
	rt := runtime.New(
		runtime.WithHome(t.TempDir()),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{"HOME": t.TempDir()}),
		runtime.WithLookup(testutil.FakeLookup(map[string]string{
			"npm":        "/usr/bin/npm",
			"uv":         "/usr/bin/uv",
			"python3.12": "/usr/bin/python3.12",
		})),
		runtime.WithRunner(recorder.Runner()),
	)
	return rt, recorder
}

func TestTheLockedVersionIsWhatGetsInstalled(t *testing.T) {
	// The pin is the whole point: an install that resolves to whatever is current
	// makes the release unreproducible and the licence audit wrong.
	rt, recorder := installRuntime(t)
	agent := agentFor(t, "codex")

	result, err := LockedAgent(rt, "codex", agent, Options{Timeout: time.Minute})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Installed || result.Version != agent.Package.Version {
		t.Fatalf("result = %+v, want the locked version", result)
	}
	call, found := recorder.FindCall("npm", "install", "-g")
	if !found {
		t.Fatal("no npm install was issued")
	}
	want := agent.Package.Name + "@" + agent.Package.Version
	if !strings.Contains(call.Command(), want) {
		t.Errorf("install command = %q, want the pinned spec %q", call.Command(), want)
	}
}

func TestTheIntegrityIsCheckedBeforeAnythingIsInstalled(t *testing.T) {
	// The manifest recorded a sha512 that nothing read, so the version was pinned
	// and the bytes were not. The order matters: querying after installing would
	// verify a package already on the machine.
	rt, recorder := installRuntime(t)
	if _, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	commands := []string{}
	for _, call := range recorder.Calls() {
		commands = append(commands, call.Command())
	}
	viewAt, installAt := -1, -1
	for index, command := range commands {
		if strings.Contains(command, "dist.integrity") && viewAt < 0 {
			viewAt = index
		}
		if strings.Contains(command, "install -g") && installAt < 0 {
			installAt = index
		}
	}
	if viewAt < 0 {
		t.Fatalf("the integrity was never queried; calls were %v", commands)
	}
	if installAt < 0 || viewAt > installAt {
		t.Fatalf("integrity checked at %d, install at %d; the check must come first", viewAt, installAt)
	}
}

func TestAChecksumMismatchFailsClosedAndNamesBothValues(t *testing.T) {
	// A mismatch means the registry is serving something other than the locked
	// release. Proceeding would install it; a vague message would leave the user
	// unable to tell what happened.
	rt, _ := installRuntime(t, testutil.Succeed("sha512-somethingelse\n", "dist.integrity"))
	agent := agentFor(t, "codex")

	_, err := LockedAgent(rt, "codex", agent, Options{Timeout: time.Minute})
	if err == nil {
		t.Fatal("a checksum mismatch must fail the install")
	}
	assertCode(t, err, "AGENT_INSTALL_FAILED")
	if !strings.Contains(err.Error(), agent.Package.Integrity) {
		t.Errorf("message = %q, want it to name the expected value", err.Error())
	}
	if !strings.Contains(err.Error(), "sha512-somethingelse") {
		t.Errorf("message = %q, want it to name what the registry reported", err.Error())
	}
}

func TestAnAbsentChecksumFromTheRegistryIsStillAMismatch(t *testing.T) {
	// A registry that answers with nothing has not proven anything, so this must
	// not be read as agreement.
	rt, _ := installRuntime(t, testutil.Succeed("\n", "dist.integrity"))
	_, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute})
	if err == nil {
		t.Fatal("an empty checksum must fail the install")
	}
	if !strings.Contains(err.Error(), "(none)") {
		t.Errorf("message = %q, want it to say the registry reported nothing", err.Error())
	}
}

func TestAPackageMissingFromTheRegistryIsReportedAsRetryable(t *testing.T) {
	rt, _ := installRuntime(t, testutil.Fail(1, "npm ERR! 404", "dist.integrity"))
	_, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute})
	if err == nil {
		t.Fatal("a missing package must fail the install")
	}
	var oneAgentErr *oerr.Error
	if errors.As(err, &oneAgentErr) && !oneAgentErr.Retryable {
		t.Error("a registry that does not have the package may have it later")
	}
}

func TestTheIntegrityCheckIsSkippedForLatestBecauseItCannotApply(t *testing.T) {
	// The manifest's checksum describes the locked version, not whatever floats
	// at the tag. Comparing them would fail every --latest install.
	rt, recorder := installRuntime(t)
	if _, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Latest: true, Timeout: time.Minute}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, found := recorder.FindCall("dist.integrity"); found {
		t.Error("the integrity was queried for a floating tag, where it cannot apply")
	}
}

func TestALatestInstallReportsNoVersionRatherThanGuessing(t *testing.T) {
	rt, _ := installRuntime(t)
	result, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Latest: true, Timeout: time.Minute})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Version != "" {
		t.Errorf("version = %q; what floats at the tag is not known without asking", result.Version)
	}
	if result.LockedVersion == "" {
		t.Error("the pin should still be reported, so the difference is visible")
	}
}

func TestAnAgentAlreadyPresentIsLeftAloneUnlessThePinIsEnforced(t *testing.T) {
	// Reinstalling would replace a version the user may have chosen deliberately.
	recorder := testutil.NewRecordingRunner(t,
		testutil.NpmIntegrityResponder(t),
		testutil.Succeed("codex-cli 0.100.0\n", "--version"),
		testutil.Succeed("", "install"),
	)
	rt := runtime.New(
		runtime.WithHome(t.TempDir()),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{}),
		runtime.WithLookup(testutil.FakeLookup(map[string]string{
			"npm":   "/usr/bin/npm",
			"codex": "/usr/bin/codex",
		})),
		runtime.WithRunner(recorder.Runner()),
	)

	result, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Installed {
		t.Error("an Agent that is already present should not be reinstalled")
	}
	if result.Version != "0.100.0" {
		t.Errorf("version = %q, want what is actually installed", result.Version)
	}
	if _, found := recorder.FindCall("install", "-g"); found {
		t.Error("an install was issued for an Agent already present")
	}
}

func TestEnforcingThePinReinstallsAWrongVersionAndSkipsAMatchingOne(t *testing.T) {
	agent := agentFor(t, "codex")
	for _, testCase := range []struct {
		name          string
		reported      string
		wantInstalled bool
	}{
		{"wrong version is replaced", "codex-cli 0.100.0\n", true},
		{"matching version is left alone", "codex-cli " + agent.Package.Version + "\n", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := testutil.NewRecordingRunner(t,
				testutil.NpmIntegrityResponder(t),
				testutil.Succeed(testCase.reported, "--version"),
				testutil.Succeed("", "install"),
			)
			rt := runtime.New(
				runtime.WithHome(t.TempDir()),
				runtime.WithOSID("linux"),
				runtime.WithEnv(map[string]string{}),
				runtime.WithLookup(testutil.FakeLookup(map[string]string{
					"npm":   "/usr/bin/npm",
					"codex": "/usr/bin/codex",
				})),
				runtime.WithRunner(recorder.Runner()),
			)
			result, err := LockedAgent(rt, "codex", agent, Options{EnforceLocked: true, Timeout: time.Minute})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Installed != testCase.wantInstalled {
				t.Errorf("installed = %v, want %v", result.Installed, testCase.wantInstalled)
			}
		})
	}
}

func TestAMirrorIsPassedThroughTheEnvironmentAndReported(t *testing.T) {
	// Reported so the origin of what is on the machine is visible rather than
	// implied -- a user who chose a mirror should be able to see that they did.
	rt, recorder := installRuntime(t)
	result, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{
		Registry: "npmmirror", Timeout: time.Minute,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mirror, _ := catalog.MirrorByID("npmmirror")
	if result.Registry != mirror.Registry {
		t.Errorf("registry = %q, want %q", result.Registry, mirror.Registry)
	}
	call, found := recorder.FindCall("install", "-g")
	if !found {
		t.Fatal("no install was issued")
	}
	if call.Env["npm_config_registry"] != mirror.Registry {
		t.Errorf("the install did not see the mirror: env = %v", call.Env)
	}
}

func TestTheOfficialRegistryIsNotPassedThroughTheEnvironment(t *testing.T) {
	// npm's own default. Setting it explicitly would override a user's .npmrc
	// for no reason.
	rt, recorder := installRuntime(t)
	if _, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call, _ := recorder.FindCall("install", "-g")
	if _, present := call.Env["npm_config_registry"]; present {
		t.Error("the official registry should not be forced into the environment")
	}
}

func TestTheIntegrityIsCheckedAgainstTheMirrorItWillInstallFrom(t *testing.T) {
	// Checking the official registry and installing from a mirror would prove
	// nothing about the bytes actually fetched.
	rt, recorder := installRuntime(t)
	if _, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{
		Registry: "npmmirror", Timeout: time.Minute,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call, found := recorder.FindCall("dist.integrity")
	if !found {
		t.Fatal("no integrity query was issued")
	}
	mirror, _ := catalog.MirrorByID("npmmirror")
	if !strings.Contains(call.Command(), "--registry="+mirror.Registry) {
		t.Errorf("integrity query = %q, want it aimed at the mirror", call.Command())
	}
}

func TestAnInstallerFailureIsSummarisedWithoutTheCredential(t *testing.T) {
	// npm echoes its environment on some failures, and the environment carries
	// the key.
	const secret = "sk-must-not-be-logged"
	recorder := testutil.NewRecordingRunner(t,
		testutil.NpmIntegrityResponder(t),
		testutil.Fail(1, "npm ERR! failed\nnpm ERR! env ONEAGENT_API_KEY_CODEX="+secret+"\n", "install", "-g"),
	)
	rt := runtime.New(
		runtime.WithHome(t.TempDir()),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{"ONEAGENT_API_KEY_CODEX": secret}),
		runtime.WithLookup(testutil.FakeLookup(map[string]string{"npm": "/usr/bin/npm"})),
		runtime.WithRunner(recorder.Runner()),
	)

	_, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute})
	if err == nil {
		t.Fatal("a failing installer must fail the install")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the credential reached the error message: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("message = %q, want the redaction to be visible", err.Error())
	}
}

func TestAnInstallTimeoutIsReportedAsTimeoutAndRetryable(t *testing.T) {
	recorder := testutil.NewRecordingRunner(t,
		testutil.NpmIntegrityResponder(t),
		testutil.Error(&runtime.TimeoutError{Argv: []string{"npm", "install"}, FromCaller: true}, "install", "-g"),
	)
	rt := runtime.New(
		runtime.WithHome(t.TempDir()),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{}),
		runtime.WithLookup(testutil.FakeLookup(map[string]string{"npm": "/usr/bin/npm"})),
		runtime.WithRunner(recorder.Runner()),
	)
	_, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Millisecond})
	if err == nil {
		t.Fatal("a timeout must fail the install")
	}
	assertCode(t, err, "TIMEOUT")
	var oneAgentErr *oerr.Error
	if errors.As(err, &oneAgentErr) && !oneAgentErr.Retryable {
		t.Error("a timeout is worth retrying")
	}
}

func TestAnInstallerThatCannotStartIsDistinguishedFromOneThatFails(t *testing.T) {
	// Different causes, different fixes: a missing executable is a broken
	// environment, a non-zero exit is a failed install.
	recorder := testutil.NewRecordingRunner(t,
		testutil.NpmIntegrityResponder(t),
		testutil.Error(&runtime.StartError{Argv: []string{"npm"}, Err: errors.New("not executable")}, "install", "-g"),
	)
	rt := runtime.New(
		runtime.WithHome(t.TempDir()),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{}),
		runtime.WithLookup(testutil.FakeLookup(map[string]string{"npm": "/usr/bin/npm"})),
		runtime.WithRunner(recorder.Runner()),
	)
	_, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute})
	if err == nil {
		t.Fatal("a start failure must fail the install")
	}
	assertCode(t, err, "AGENT_INSTALL_FAILED")
	if !strings.Contains(err.Error(), "Cannot start") {
		t.Errorf("message = %q, want it to say the installer could not start", err.Error())
	}
}

func TestNoCredentialEverReachesACommandLine(t *testing.T) {
	// The key travels through the environment, never argv, because argv is
	// visible to every process on the machine.
	const secret = "sk-never-on-argv"
	rt, recorder := installRuntime(t)
	rt.Env = map[string]string{"ONEAGENT_API_KEY_CODEX": secret}
	if _, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	recorder.AssertNoCallContains(t, secret)
}

func TestNoInstallGoesThroughAShell(t *testing.T) {
	// A package name comes from the manifest, but the principle holds regardless:
	// argv is a list, so nothing can be interpreted as shell.
	rt, recorder := installRuntime(t)
	if _, err := LockedAgent(rt, "codex", agentFor(t, "codex"), Options{Timeout: time.Minute}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, call := range recorder.Calls() {
		if len(call.Argv) < 2 {
			t.Errorf("call %v does not look like an argv list", call.Argv)
		}
		for _, forbidden := range []string{"sh -c", "cmd /c", "bash -c", "powershell -Command"} {
			if strings.Contains(call.Command(), forbidden) {
				t.Errorf("call went through a shell: %s", call.Command())
			}
		}
	}
}

func TestAidersInstallNeverDownloadsAPythonRuntime(t *testing.T) {
	// The product boundary: OneAgent configures an environment, it does not
	// install language runtimes behind the user's back.
	rt, recorder := installRuntime(t)
	agent := agentFor(t, "aider")
	if agent.Package == nil || agent.Package.Manager != "uv" {
		t.Skip("aider no longer installs through uv")
	}
	if _, err := LockedAgent(rt, "aider", agent, Options{Timeout: time.Minute}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call, found := recorder.FindCall("uv", "tool", "install")
	if !found {
		t.Fatal("no uv install was issued")
	}
	if !strings.Contains(call.Command(), "--no-python-downloads") {
		t.Errorf("install command = %q, want --no-python-downloads", call.Command())
	}
	if !strings.Contains(call.Command(), agent.Package.Name+"=="+agent.Package.Version) {
		t.Errorf("install command = %q, want the pinned spec", call.Command())
	}
}

func TestWithoutPython312AidersInstallIsRefusedRatherThanAttempted(t *testing.T) {
	// Attempting it would fail inside uv with a message about a Python version,
	// which does not tell the user what to do.
	recorder := testutil.NewRecordingRunner(t, testutil.Succeed("Python 3.14.1\n", "--version"))
	rt := runtime.New(
		runtime.WithHome(t.TempDir()),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{}),
		runtime.WithLookup(testutil.FakeLookup(map[string]string{
			"uv":      "/usr/bin/uv",
			"python3": "/usr/bin/python3",
		})),
		runtime.WithRunner(recorder.Runner()),
	)
	_, err := LockedAgent(rt, "aider", agentFor(t, "aider"), Options{Timeout: time.Minute})
	if err == nil {
		t.Fatal("aider must not be installed without Python 3.12")
	}
	assertCode(t, err, "PREREQUISITE_MISSING")
	if !strings.Contains(err.Error(), "will not download Python") {
		t.Errorf("message = %q, want it to say OneAgent will not download Python", err.Error())
	}
}

func TestAMissingPackageManagerIsAPrerequisiteFailure(t *testing.T) {
	for _, testCase := range []struct{ agentID, missing string }{
		{"codex", "npm"},
		{"aider", "uv"},
	} {
		recorder := testutil.NewRecordingRunner(t, testutil.Succeed("", ""))
		rt := runtime.New(
			runtime.WithHome(t.TempDir()),
			runtime.WithOSID("linux"),
			runtime.WithEnv(map[string]string{}),
			runtime.WithLookup(testutil.FakeLookup(map[string]string{})),
			runtime.WithRunner(recorder.Runner()),
		)
		_, err := LockedAgent(rt, testCase.agentID, agentFor(t, testCase.agentID), Options{Timeout: time.Minute})
		if err == nil {
			t.Fatalf("%s: a missing %s must fail", testCase.agentID, testCase.missing)
		}
		assertCode(t, err, "PREREQUISITE_MISSING")
		if !strings.Contains(err.Error(), testCase.missing) {
			t.Errorf("%s: message = %q, want it to name %s", testCase.agentID, err.Error(), testCase.missing)
		}
	}
}

func TestAWindowsPrerequisiteFromTheManifestIsEnforced(t *testing.T) {
	// Declared per Agent so a new Agent's Windows requirement is added to the
	// lock rather than to this code.
	agent := agentFor(t, "claude-code")
	if len(agent.WindowsPrerequisites) == 0 {
		t.Skip("claude-code declares no Windows prerequisites")
	}
	recorder := testutil.NewRecordingRunner(t, testutil.NpmIntegrityResponder(t), testutil.Succeed("", "install"))
	rt := runtime.New(
		runtime.WithHome(t.TempDir()),
		runtime.WithOSID("windows"),
		runtime.WithEnv(map[string]string{}),
		// npm is present, the declared prerequisite is not.
		runtime.WithLookup(testutil.FakeLookup(map[string]string{"npm": `C:\npm.cmd`})),
		runtime.WithRunner(recorder.Runner()),
	)
	_, err := LockedAgent(rt, "claude-code", agent, Options{Timeout: time.Minute})
	if err == nil {
		t.Fatal("a missing Windows prerequisite must fail the install")
	}
	assertCode(t, err, "PREREQUISITE_MISSING")
	if !strings.Contains(err.Error(), agent.WindowsPrerequisites[0]) {
		t.Errorf("message = %q, want it to name %q", err.Error(), agent.WindowsPrerequisites[0])
	}
}

func TestTheSamePrerequisiteIsNotRequiredOnPosix(t *testing.T) {
	// windows_prerequisites means what it says; enforcing it everywhere would
	// block installs that work.
	rt, _ := installRuntime(t)
	if _, err := LockedAgent(rt, "claude-code", agentFor(t, "claude-code"), Options{Timeout: time.Minute}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnAgentWithNoPackageCannotBeInstalled(t *testing.T) {
	// Guide-only Agents have no package, and inventing an install for them would
	// cross the boundary that says OneAgent only shows their instructions.
	rt, _ := installRuntime(t)
	manifest := catalog.MustLoad()
	for id, agent := range manifest.Agents {
		if agent.Package != nil {
			continue
		}
		if _, err := LockedAgent(rt, id, agent, Options{Timeout: time.Minute}); err == nil {
			t.Errorf("%s has no package but was installed", id)
		} else {
			assertCode(t, err, "PREREQUISITE_MISSING")
		}
	}
}

func TestEveryAutoAgentCanBeInstalledWithTheDoublesInPlace(t *testing.T) {
	// A manifest entry whose install path does not work is a user-visible
	// failure, and this is the cheapest place to notice.
	manifest := catalog.MustLoad()
	for _, id := range manifest.AutoAgents() {
		t.Run(id, func(t *testing.T) {
			rt, _ := installRuntime(t)
			result, err := LockedAgent(rt, id, agentFor(t, id), Options{Timeout: time.Minute})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.Installed {
				t.Error("nothing was installed")
			}
		})
	}
}
