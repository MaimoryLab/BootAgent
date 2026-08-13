package install

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

type fakeInstallRunner struct {
	paths    map[string]string
	calls    [][]string
	lastCall []string
	envs     []map[string]string
	run      func([]string, map[string]string) (process.Result, error)
	version  string
}

func (r *fakeInstallRunner) LookPath(command string) (string, bool) {
	path, ok := r.paths[command]
	return path, ok
}

func (r *fakeInstallRunner) Run(_ context.Context, argv []string, env map[string]string, _ time.Duration) (process.Result, error) {
	runnerArgs := append([]string(nil), argv...)
	r.calls = append(r.calls, runnerArgs)
	r.lastCall = runnerArgs
	r.envs = append(r.envs, cloneEnv(env))
	if r.run != nil {
		return r.run(argv, env)
	}
	if containsArg(argv, "--version") {
		version := r.version
		if version == "" {
			version = "0.0.1"
		}
		return process.Result{Args: runnerArgs, ExitCode: 0, Stdout: "tool " + version}, nil
	}
	return r.installResult(argv), nil
}

func (r *fakeInstallRunner) installResult(argv []string) process.Result {
	return process.Result{Args: append([]string(nil), argv...), ExitCode: 0}
}

func mustManifest() catalog.Manifest {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		panic(err)
	}
	return manifest
}

func containsArg(argv []string, wanted string) bool {
	return slices.Contains(argv, wanted)
}

func runtimeForInstall(runner process.Runner, osID string, env map[string]string) Runtime {
	return Runtime{
		Home:     "/tmp/bootagent-install",
		Platform: platform.Info{OS: osID, Arch: "x64", Shell: "bash"},
		Env:      env,
		Runner:   runner,
	}
}

func TestVersionFromOutputAndInstalledVersion(t *testing.T) {
	for input, want := range map[string]string{
		"codex-cli 0.145.0":               "0.145.0",
		"v2.1.217\n":                      "2.1.217",
		"release 1.2.3-beta.1+build":      "1.2.3-beta.1+build",
		"version 12.4.0 and 9.0.0":        "12.4.0",
		"no version":                      "",
		"embedded 10.20 is not a version": "",
	} {
		if got := VersionFromOutput(input); got != want {
			t.Errorf("VersionFromOutput(%q) = %q, want %q", input, got, want)
		}
	}
	runner := &fakeInstallRunner{paths: map[string]string{"agent": "/bin/agent"}}
	runtime := runtimeForInstall(runner, "linux", nil)
	agent := catalog.Agent{Command: "agent", VersionArgs: []string{"--version"}}
	if got := InstalledVersion(context.Background(), runtime, agent); got != "0.0.1" {
		t.Fatalf("InstalledVersion() = %q", got)
	}
}

func TestResolveRegistryValidatesHTTPSAndMirrors(t *testing.T) {
	if got, err := ResolveRegistry(""); err != nil || got != "https://registry.npmjs.org/" {
		t.Fatalf("default registry = %q, %v", got, err)
	}
	if got, err := ResolveRegistry("official"); err != nil || got != "https://registry.npmjs.org/" {
		t.Fatalf("official registry = %q, %v", got, err)
	}
	if got, err := ResolveRegistry("https://npm.example.com"); err != nil || got != "https://npm.example.com/" {
		t.Fatalf("custom registry = %q, %v", got, err)
	}
	for _, value := range []string{"http://npm.example.com", "https://user:secret@npm.example.com", "https://npm.example.com/\nheader"} {
		if _, err := ResolveRegistry(value); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
			t.Errorf("invalid registry %q returned %v", value, err)
		}
	}
}

func TestInstallAgentDefaultsToLatestAndSupportsExactVersion(t *testing.T) {
	manifest := mustManifest()
	agent := manifest.Agents["codex"]
	runner := &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm"}}
	runtime := runtimeForInstall(runner, "linux", map[string]string{"PATH": "/bin"})
	result, err := InstallAgent(context.Background(), runtime, agent, Options{Registry: "npmmirror"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || result.Version != "" || result.Registry != "https://registry.npmmirror.com/" {
		t.Fatalf("install result = %#v", result)
	}
	if len(runner.envs) != 1 || runner.envs[0]["npm_config_registry"] != "https://registry.npmmirror.com/" {
		t.Fatalf("runner environments = %#v", runner.envs)
	}
	if !reflect.DeepEqual(runner.lastCall, []string{"/fake/npm", "install", "-g", "--registry=https://registry.npmmirror.com/", "@openai/codex"}) {
		t.Fatalf("last command = %#v", runner.lastCall)
	}

	runner = &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"}, version: "1.0.0"}
	runtime.Runner = runner
	result, err = InstallAgent(context.Background(), runtime, agent, Options{Version: "1.2.3"})
	if err != nil || !result.Installed || result.Version != "1.2.3" || len(runner.calls) != 2 {
		t.Fatalf("exact version result = %#v, err=%v", result, err)
	}
	if !reflect.DeepEqual(runner.lastCall, []string{"/fake/npm", "install", "-g", "@openai/codex@1.2.3"}) {
		t.Fatalf("exact version command = %#v", runner.lastCall)
	}

	runner = &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"}, version: "1.2.3"}
	runtime.Runner = runner
	result, err = InstallAgent(context.Background(), runtime, agent, Options{})
	if err != nil || result.Installed || result.Version != "1.2.3" || len(runner.calls) != 1 {
		t.Fatalf("existing version result = %#v, err=%v, calls=%#v", result, err, runner.calls)
	}
}

func TestInstallAgentSupportsAiderRuntimeBoundary(t *testing.T) {
	manifest := mustManifest()
	agent := manifest.Agents["aider"]
	runner := &fakeInstallRunner{paths: map[string]string{"uv": "/fake/uv", "python3.12": "/fake/python"}}
	runtime := runtimeForInstall(runner, "linux", nil)
	result, err := InstallAgent(context.Background(), runtime, agent, Options{Version: "0.86.2"})
	if err != nil || !result.Installed {
		t.Fatalf("uv result = %#v, err=%v", result, err)
	}
	// uv resolves the interpreter itself: BootAgent requests 3.12 and lets uv
	// reuse a system Python or download a managed one into its own root.
	if !reflect.DeepEqual(runner.lastCall, []string{"/fake/uv", "tool", "install", "--force", "--python", "3.12", "aider-chat==0.86.2"}) {
		t.Fatalf("uv command = %#v", runner.lastCall)
	}
	installEnv := runner.envs[len(runner.envs)-1]
	if !strings.HasSuffix(installEnv["UV_PYTHON_INSTALL_DIR"], "/.bootagent/runtimes/python") {
		t.Fatalf("uv python install dir = %q", installEnv["UV_PYTHON_INSTALL_DIR"])
	}
	if !strings.HasSuffix(installEnv["UV_TOOL_BIN_DIR"], "/.bootagent/runtimes/global/bin") {
		t.Fatalf("uv tool bin dir = %q", installEnv["UV_TOOL_BIN_DIR"])
	}
}

func TestInstallPrerequisitesAndFailuresAreStableAndRedacted(t *testing.T) {
	manifest := mustManifest()
	agent := manifest.Agents["codex"]
	missing := &fakeInstallRunner{paths: map[string]string{}}
	_, err := InstallAgent(context.Background(), runtimeForInstall(missing, "linux", nil), agent, Options{})
	if err == nil || oneerrors.As(err).Code != oneerrors.PrerequisiteMissing || !strings.Contains(err.Error(), "npm") {
		t.Fatalf("missing npm error = %v", err)
	}
	// Retryable is what decides whether the activation page renders a retry
	// button at all. A missing runtime is the case a user can most easily fix by
	// hand, and it used to be the one offering no way back.
	if !oneerrors.As(err).Retryable {
		t.Fatalf("missing npm should be retryable: %+v", oneerrors.As(err))
	}
	// The guidance is the whole reason this message differs from the bare one the
	// manager switch used to raise.
	if !strings.Contains(err.Error(), "Node.js") || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("missing npm message should name the runtime and the retry: %q", err.Error())
	}

	secret := "install-secret"
	failing := &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm"}}
	failing.run = func(argv []string, _ map[string]string) (process.Result, error) {
		return process.Result{Args: argv, ExitCode: 9, Stderr: "failed with " + secret}, nil
	}
	runtime := runtimeForInstall(failing, "linux", map[string]string{"API_KEY": secret})
	_, err = InstallAgent(context.Background(), runtime, agent, Options{})
	if err == nil || oneerrors.As(err).Code != oneerrors.AgentInstallFailed || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("redacted install failure = %v", err)
	}

	timed := &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm"}}
	timed.run = func(argv []string, _ map[string]string) (process.Result, error) {
		return process.Result{Args: argv, ExitCode: -1}, context.DeadlineExceeded
	}
	_, err = InstallAgent(context.Background(), runtimeForInstall(timed, "linux", nil), agent, Options{})
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("timeout install error = %v", err)
	}

	if _, err = InstallAgent(context.Background(), runtime, agent, Options{Version: "1.2.3@other"}); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("invalid version error = %v", err)
	}
}

func TestInstallerFailureDetailLimitsLinesAndLength(t *testing.T) {
	result := process.Result{Stderr: "one\ntwo\nthree\nfour\n" + strings.Repeat("x", 700)}
	detail := installerFailureDetail(result, nil)
	if strings.Contains(detail, "one") || !strings.Contains(detail, "four") || len([]rune(detail)) > 600 {
		t.Fatalf("failure detail = %q", detail)
	}
}

// A manifest problem and a missing tool are both PrerequisiteMissing, but only
// one of them changes if the user does something and presses retry. The retry
// button is gated on Retryable, so conflating them would either hide the button
// where it helps or offer it where it cannot.
func TestOnlyFixablePrerequisitesAreRetryable(t *testing.T) {
	manifest := mustManifest()
	agent := manifest.Agents["codex"]

	missingTool := &fakeInstallRunner{paths: map[string]string{}}
	_, err := InstallAgent(context.Background(), runtimeForInstall(missingTool, "linux", nil), agent, Options{})
	if err == nil || !oneerrors.As(err).Retryable {
		t.Fatalf("a missing runtime is fixable, so it must be retryable: %v", err)
	}

	// No package contract: retrying runs the same code over the same manifest.
	noContract := agent
	noContract.Package = nil
	_, err = InstallAgent(context.Background(), runtimeForInstall(missingTool, "linux", nil), noContract, Options{})
	converted := oneerrors.As(err)
	if err == nil || converted.Code != oneerrors.PrerequisiteMissing {
		t.Fatalf("missing package contract error = %v", err)
	}
	if converted.Retryable {
		t.Fatalf("a manifest without an install contract cannot be fixed by retrying: %+v", converted)
	}
}
