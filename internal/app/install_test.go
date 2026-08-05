package app

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/install"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

func TestInstallResultWirePreservesFieldPresence(t *testing.T) {
	configured, err := json.Marshal(AgentInstallResult{
		Agent: "codex", Status: "configured", Config: "", Installed: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configured), `{"agent":"codex","status":"configured","config":"","installed":false,"version":null,"lockedVersion":null,"retryable":false}`; got != want {
		t.Fatalf("configured wire = %s, want %s", got, want)
	}

	checkOnly, err := json.Marshal(AgentInstallResult{
		Agent: "codex", Status: "skipped", Installed: false, Version: "1.0.0", checkOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(checkOnly), `{"agent":"codex","status":"skipped","installed":false,"version":"1.0.0","lockedVersion":null,"retryable":false}`; got != want {
		t.Fatalf("check-only wire = %s, want %s", got, want)
	}

	guide, err := json.Marshal(AgentInstallResult{Agent: "gemini-cli", Status: "guide-only", Message: "use login"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(guide), `{"agent":"gemini-cli","status":"guide-only","message":"use login","retryable":false}`; got != want {
		t.Fatalf("guide wire = %s, want %s", got, want)
	}
}

type installAppRunner struct {
	paths map[string]string
	calls [][]string
	envs  []map[string]string
}

func (r *installAppRunner) LookPath(command string) (string, bool) {
	path, ok := r.paths[command]
	return path, ok
}

func (r *installAppRunner) Run(_ context.Context, argv []string, env map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	copyEnv := make(map[string]string, len(env))
	maps.Copy(copyEnv, env)
	r.envs = append(r.envs, copyEnv)
	if len(argv) > 1 && argv[1] == "--version" {
		return process.Result{Args: argv, ExitCode: 0, Stdout: "tool 1.0.0"}, nil
	}
	return process.Result{Args: argv, ExitCode: 0}, nil
}

type installAppDoer func(*http.Request) (*http.Response, error)

func (d installAppDoer) Do(request *http.Request) (*http.Response, error) { return d(request) }

func installAppResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func installCore(t *testing.T, home string, runner process.Runner, doer provider.HTTPDoer) *UseCases {
	t.Helper()
	return NewUseCasesWithProviderClient(StatusOptions{
		Home:        home,
		Platform:    platform.For("linux", "amd64"),
		Runner:      runner,
		Environment: map[string]string{"HOME": home},
	}, provider.NewClient(doer))
}

func installOptions(agents ...string) InstallAgentsOptions {
	return InstallAgentsOptions{
		Agents: agents, Provider: "ppio", APIKey: "install-secret", Model: "model-a",
		Configure: true, SkipTest: true, Timeout: 30 * time.Second,
	}
}

func TestInstallAgentsWritesAllManagedAdaptersAndPublishesProfileLast(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{
		"codex": "/fake/codex", "claude": "/fake/claude", "opencode": "/fake/opencode",
		"kilo": "/fake/kilo", "aider": "/fake/aider", "npm": "/fake/npm", "uv": "/fake/uv",
	}}
	core := installCore(t, home, runner, installAppDoer(func(*http.Request) (*http.Response, error) {
		return installAppResponse(http.StatusNoContent, ""), nil
	}))
	result, err := core.InstallAgents(context.Background(), installOptions("codex", "claude-code", "opencode", "kilo-cli", "aider"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || len(result.Results) != 5 {
		t.Fatalf("install result = %#v", result)
	}
	for _, item := range result.Results {
		if item.Status != "configured" || item.Config == "" {
			t.Errorf("result = %#v", item)
		}
	}
	for _, path := range []string{
		filepath.Join(home, ".codex", "config.toml"),
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, ".config", "kilo", "kilo.jsonc"),
		filepath.Join(home, ".oneagent", "aider.env"),
		filepath.Join(home, ".oneagent", "profile.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s: %v", path, err)
		}
	}
	if strings.Contains(result.Log, "install-secret") || strings.Contains(result.Next, "install-secret") {
		t.Fatal("API key leaked through install result")
	}
	active := core.profiles.LoadActive()
	if active.Profile == nil || active.ID != "default" || active.Profile.Protocol != "responses" {
		t.Fatalf("active profile = %#v", active)
	}
	if binding, err := core.profiles.ReadAgentBinding("codex"); err != nil || binding == nil || binding.ProfileRef != "default" {
		t.Fatalf("default profile binding = %#v, %v", binding, err)
	}
}

// An npm install creates the managed global prefix, so the directory holding
// the Agent CLI first exists at the end of the Agent install. Recording the
// login PATH only after a runtime install left `node` resolvable in the user's
// own terminal while `codex` was not on PATH at all.
func TestInstallAgentsRecordsTheGlobalPrefixOnPath(t *testing.T) {
	home := t.TempDir()
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Runtimes["node"]
	artifact := entry.Artifacts[catalog.RuntimeArtifactKey("linux", "x64")]
	nodeBin := filepath.Join(home, ".oneagent", "runtimes", "node", "v"+entry.Version, artifact.BinDir)
	globalBin := install.GlobalBinDir(home, "linux")
	for _, directory := range []string{nodeBin, globalBin} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(nodeBin, ".oneagent-runtime-ok"), []byte(entry.Version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// npm resolves, so no runtime bootstrap runs; codex does not, so the Agent
	// install itself is what has to persist the PATH entry.
	runner := &installAppRunner{paths: map[string]string{"npm": filepath.Join(nodeBin, "npm")}}
	core := installCore(t, home, runner, nil)
	options := installOptions("codex")
	options.InstallAgent = true
	if _, err := core.InstallAgents(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile(filepath.Join(home, ".profile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(profile), "OneAgent managed runtimes") {
		t.Fatalf("install did not record the runtime PATH block: %q", profile)
	}
	script, err := os.ReadFile(filepath.Join(home, ".oneagent", "runtimes", "runtime-path.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), globalBin) {
		t.Fatalf("PATH script omits the global prefix that holds codex: %q", script)
	}
}

func TestInstallAgentsAppliesNamedProfileWithItsSavedKey(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{"codex": "/fake/codex"}}
	core := installCore(t, home, runner, nil)
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Label: "Team", Provider: "ppio", APIKey: "profile-secret", Model: "model-a",
	}); err != nil {
		t.Fatal(err)
	}
	options := installOptions("codex")
	options.APIKey = ""
	options.ProfileID = "team"
	result, err := core.InstallAgents(context.Background(), options)
	if err != nil || !result.OK {
		t.Fatalf("named profile install = %#v, %v", result, err)
	}
	active := core.profiles.LoadActive()
	binding, bindingErr := core.profiles.ReadAgentBinding("codex")
	if active.ID != "team" || bindingErr != nil || binding == nil || binding.ProfileRef != "team" {
		t.Fatalf("named profile state = active %#v, binding %#v, %v", active, binding, bindingErr)
	}
	auth, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil || !strings.Contains(string(auth), "profile-secret") {
		t.Fatalf("named profile credential was not applied: %q, %v", auth, err)
	}
}

func TestInstallAgentsPrefersProviderKeyOverLegacyProfileSecret(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{"codex": "/fake/codex"}}
	core := installCore(t, home, runner, nil)
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Provider: "ppio", APIKey: "legacy-secret", Model: "model-a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := core.providers.SaveKey(context.Background(), "ppio", "provider-secret"); err != nil {
		t.Fatal(err)
	}
	options := installOptions("codex")
	options.APIKey = ""
	options.ProfileID = "team"
	result, err := core.InstallAgents(context.Background(), options)
	if err != nil || !result.OK {
		t.Fatalf("provider-key install = %#v, %v", result, err)
	}
	auth, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil || !strings.Contains(string(auth), "provider-secret") || strings.Contains(string(auth), "legacy-secret") {
		t.Fatalf("provider key was not authoritative: %q, %v", auth, err)
	}
}

func TestInstallAgentsRefusesInvalidRequestBeforeWriting(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{"codex": "/fake/codex"}}
	core := installCore(t, home, runner, nil)
	options := installOptions("codex")
	options.InstallAgent = true
	options.AgentVersion = "1.2.3@other"
	if _, err := core.InstallAgents(context.Background(), options); err == nil {
		t.Fatal("invalid request unexpectedly succeeded")
	}
	entries, err := os.ReadDir(home)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid request left files: %v", entries)
	}
}

func TestInstallAgentsDoesNotPublishProfileWhenProbeFails(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{"codex": "/fake/codex"}}
	core := installCore(t, home, runner, installAppDoer(func(*http.Request) (*http.Response, error) {
		return installAppResponse(http.StatusUnauthorized, `{"error":"bad key"}`), nil
	}))
	options := installOptions("codex")
	options.SkipTest = false
	result, err := core.InstallAgents(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || len(result.Results) != 1 || result.Results[0].Status != "failed" {
		t.Fatalf("failed install result = %#v", result)
	}
	if active := core.profiles.LoadActive(); active.Profile != nil || active.ID != "" {
		t.Fatalf("failed install published active profile: %#v", active)
	}
}

func TestInstallAgentsSharpenModelDiagnosis(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{"codex": "/fake/codex"}}
	core := installCore(t, home, runner, installAppDoer(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet {
			return installAppResponse(http.StatusOK, `{"data":[{"id":"real-model"}]}`), nil
		}
		return installAppResponse(http.StatusNotFound, ""), nil
	}))
	options := installOptions("codex")
	options.SkipTest = false
	result, err := core.InstallAgents(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Probe == nil || pointerString(result.Probe.ErrorCode) != "MODELS_UNSUPPORTED" || !strings.Contains(result.Probe.Message, "real-model") {
		t.Fatalf("diagnosis = %#v", result.Probe)
	}
}
