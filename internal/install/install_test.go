package install

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
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
	if strings.Contains(strings.Join(argv, " "), "dist.integrity") {
		return r.integrityResult(argv), nil
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

func (r *fakeInstallRunner) integrityResult(argv []string) process.Result {
	name := ""
	if len(argv) > 2 {
		spec := argv[2]
		if index := strings.LastIndex(spec, "@"); index > 0 {
			spec = spec[:index]
		}
		name = spec
	}
	for _, id := range catalog.AgentIDs(mustManifest()) {
		agent := mustManifest().Agents[id]
		if agent.Package != nil && agent.Package.Name == name && agent.Package.Integrity != nil {
			return process.Result{Args: append([]string(nil), argv...), ExitCode: 0, Stdout: *agent.Package.Integrity + "\n"}
		}
	}
	return process.Result{Args: append([]string(nil), argv...), ExitCode: 0}
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
	for _, value := range argv {
		if value == wanted {
			return true
		}
	}
	return false
}

func runtimeForInstall(runner process.Runner, osID string, env map[string]string) Runtime {
	return Runtime{
		Home:     "/tmp/oneagent-install",
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

func TestInstallLockedNPMUsesPinnedIntegrityAndMirrorEnvironment(t *testing.T) {
	manifest := mustManifest()
	agent := manifest.Agents["codex"]
	runner := &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm"}}
	runtime := runtimeForInstall(runner, "linux", map[string]string{"PATH": "/bin"})
	result, err := InstallLockedAgent(context.Background(), runtime, "codex", agent, Options{EnforceLocked: true, Registry: "npmmirror"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || result.Version != agent.Package.Version || result.Registry != "https://registry.npmmirror.com/" {
		t.Fatalf("install result = %#v", result)
	}
	if len(runner.envs) != 2 || runner.envs[0]["npm_config_registry"] != "" || runner.envs[1]["npm_config_registry"] != "https://registry.npmmirror.com/" {
		t.Fatalf("runner environments = %#v", runner.envs)
	}
	if !reflect.DeepEqual(runner.lastCall, []string{"/fake/npm", "install", "-g", "@openai/codex@0.145.0"}) {
		t.Fatalf("last command = %#v", runner.lastCall)
	}
}

func TestInstallLockedAgentShortCircuitsAndSupportsLatest(t *testing.T) {
	manifest := mustManifest()
	agent := manifest.Agents["codex"]
	runner := &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"}, version: agent.Package.Version}
	runtime := runtimeForInstall(runner, "linux", nil)
	result, err := InstallLockedAgent(context.Background(), runtime, "codex", agent, Options{EnforceLocked: true})
	if err != nil || result.Installed || result.Version != agent.Package.Version || len(runner.calls) != 1 {
		t.Fatalf("unlocked version result = %#v, err=%v, calls=%#v", result, err, runner.calls)
	}
	runner = &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"}}
	runtime.Runner = runner
	result, err = InstallLockedAgent(context.Background(), runtime, "codex", agent, Options{EnforceLocked: false})
	if err != nil || result.Installed || result.Version != "0.0.1" {
		t.Fatalf("non-enforced result = %#v, err=%v", result, err)
	}
	runner = &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm"}}
	runtime.Runner = runner
	result, err = InstallLockedAgent(context.Background(), runtime, "codex", agent, Options{EnforceLocked: true, Latest: true})
	if err != nil || !result.Installed || result.Version != "" {
		t.Fatalf("latest result = %#v, err=%v", result, err)
	}
	if !reflect.DeepEqual(runner.lastCall, []string{"/fake/npm", "install", "-g", "@openai/codex"}) {
		t.Fatalf("latest command = %#v", runner.lastCall)
	}
}

func TestInstallLockedAgentSupportsUVAndPythonBoundaries(t *testing.T) {
	manifest := mustManifest()
	agent := manifest.Agents["aider"]
	runner := &fakeInstallRunner{paths: map[string]string{"uv": "/fake/uv", "python3.12": "/fake/python"}}
	runtime := runtimeForInstall(runner, "linux", nil)
	result, err := InstallLockedAgent(context.Background(), runtime, "aider", agent, Options{EnforceLocked: true})
	if err != nil || !result.Installed {
		t.Fatalf("uv result = %#v, err=%v", result, err)
	}
	if !reflect.DeepEqual(runner.lastCall, []string{"/fake/uv", "tool", "install", "--force", "--python", "/fake/python", "--no-python-downloads", "aider-chat==0.86.2"}) {
		t.Fatalf("uv command = %#v", runner.lastCall)
	}

	pythonRunner := &fakeInstallRunner{paths: map[string]string{"python3": "/fake/python3"}}
	pythonRunner.run = func(argv []string, _ map[string]string) (process.Result, error) {
		return process.Result{Args: argv, ExitCode: 0, Stdout: "Python 3.12.9"}, nil
	}
	pythonRuntime := runtimeForInstall(pythonRunner, "linux", nil)
	if got, err := ResolvePython312(context.Background(), pythonRuntime); err != nil || got != "/fake/python3" {
		t.Fatalf("python3 resolution = %q, %v", got, err)
	}

	windowsRunner := &fakeInstallRunner{paths: map[string]string{"py": "py.exe"}}
	windowsRunner.run = func(argv []string, _ map[string]string) (process.Result, error) {
		return process.Result{Args: argv, ExitCode: 0, Stdout: "Python 3.12.7"}, nil
	}
	windowsRuntime := runtimeForInstall(windowsRunner, "windows", nil)
	if got, err := ResolvePython312(context.Background(), windowsRuntime); err != nil || got != "3.12" {
		t.Fatalf("py launcher resolution = %q, %v", got, err)
	}
}

func TestInstallPrerequisitesAndFailuresAreStableAndRedacted(t *testing.T) {
	manifest := mustManifest()
	agent := manifest.Agents["codex"]
	missing := &fakeInstallRunner{paths: map[string]string{}}
	_, err := InstallLockedAgent(context.Background(), runtimeForInstall(missing, "linux", nil), "codex", agent, Options{EnforceLocked: true})
	if err == nil || oneerrors.As(err).Code != oneerrors.PrerequisiteMissing || !strings.Contains(err.Error(), "npm") {
		t.Fatalf("missing npm error = %v", err)
	}

	secret := "install-secret"
	failing := &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm"}}
	failing.run = func(argv []string, _ map[string]string) (process.Result, error) {
		if strings.Contains(strings.Join(argv, " "), "dist.integrity") {
			return failing.integrityResult(argv), nil
		}
		return process.Result{Args: argv, ExitCode: 9, Stderr: "failed with " + secret}, nil
	}
	runtime := runtimeForInstall(failing, "linux", map[string]string{"API_KEY": secret})
	_, err = InstallLockedAgent(context.Background(), runtime, "codex", agent, Options{EnforceLocked: true})
	if err == nil || oneerrors.As(err).Code != oneerrors.AgentInstallFailed || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("redacted install failure = %v", err)
	}

	timed := &fakeInstallRunner{paths: map[string]string{"npm": "/fake/npm"}}
	timed.run = func(argv []string, _ map[string]string) (process.Result, error) {
		if strings.Contains(strings.Join(argv, " "), "dist.integrity") {
			return timed.integrityResult(argv), nil
		}
		return process.Result{Args: argv, ExitCode: -1}, context.DeadlineExceeded
	}
	_, err = InstallLockedAgent(context.Background(), runtimeForInstall(timed, "linux", nil), "codex", agent, Options{EnforceLocked: true})
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("timeout install error = %v", err)
	}

	if err := VerifyNPMIntegrity(context.Background(), runtime, "/fake/npm", "pkg@1", "sha512-expected", "https://registry.example/", time.Second); err == nil {
		t.Fatal("mismatched integrity unexpectedly succeeded")
	}
}

func TestResolvePython312MissingIsPrerequisiteError(t *testing.T) {
	runner := &fakeInstallRunner{paths: map[string]string{}}
	_, err := ResolvePython312(context.Background(), runtimeForInstall(runner, "linux", nil))
	if err == nil || oneerrors.As(err).Code != oneerrors.PrerequisiteMissing {
		t.Fatalf("missing Python error = %v", err)
	}
}

func TestInstallerFailureDetailLimitsLinesAndLength(t *testing.T) {
	result := process.Result{Stderr: "one\ntwo\nthree\nfour\n" + strings.Repeat("x", 700)}
	detail := installerFailureDetail(result, nil)
	if strings.Contains(detail, "one") || !strings.Contains(detail, "four") || len([]rune(detail)) > 600 {
		t.Fatalf("failure detail = %q", detail)
	}
}
