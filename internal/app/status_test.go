package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

func TestStatusUsesInjectedHomeAndCommandLookup(t *testing.T) {
	home := t.TempDir()
	lookup := func(command string) (string, bool) {
		if command == "npm" || command == "codex" {
			return "/fake/" + command, true
		}
		return "", false
	}
	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   lookup,
	})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.APIVersion != 1 || status.Platform.OS != "linux" {
		t.Fatalf("unexpected status header: %#v", status)
	}
	if !status.Agents["codex"].Installed || !status.Capabilities.CanInstall["codex"] {
		t.Fatalf("injected command lookup was not used: %#v", status.Agents["codex"])
	}
	if status.Paths["profile"] != filepath.Join(home, ".oneagent", "profile.json") {
		t.Fatalf("profile path escaped injected home: %q", status.Paths["profile"])
	}
	// The Task Center renders this directory verbatim. Without it the UI has to
	// spell out "~/.oneagent/logs", which names nothing on Windows.
	if status.Paths["logs"] != CommandLogDir(home) {
		t.Fatalf("logs path = %q, want %q", status.Paths["logs"], CommandLogDir(home))
	}
	wire, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) == "" || hasSubstring(string(wire), "api_key") || hasSubstring(string(wire), "fallback") {
		t.Fatalf("status contains a secret/internal field: %s", wire)
	}
}

func TestStatusReportsExistingConfigWithoutWriting(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("model = 'local'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Lookup: func(string) (string, bool) { return "", false }})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Agents["codex"].Configured {
		t.Fatal("existing config was not observed")
	}
}

func TestStatusProjectsProfilesAndActiveEnvironmentWithoutSecrets(t *testing.T) {
	home := t.TempDir()
	oneagentDir := filepath.Join(home, ".oneagent")
	profilesDir := filepath.Join(oneagentDir, "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	profileJSON := `{"schema_version":2,"id":"team","label":"Team","provider":"ppio","model":"model-a","config_mode":"provider","agent_ids":["codex","opencode"],"created_at":"created","activated_at":"active","api_key":"must-not-escape"}`
	if err := os.WriteFile(filepath.Join(profilesDir, "team.json"), []byte(profileJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(oneagentDir, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oneagentDir, "secrets", "team.env"), []byte("export ONEAGENT_API_KEY=sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oneagentDir, "profile.json"), []byte(`{"schema_version":2,"active":"team"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Profiles) != 1 || status.Profiles[0].ID != "team" {
		t.Fatalf("profile summaries = %#v", status.Profiles)
	}
	if status.ActiveProfile == nil || *status.ActiveProfile != "team" {
		t.Fatalf("active profile = %#v", status.ActiveProfile)
	}
	environment, ok := status.Environment.(map[string]any)
	if !ok || environment["provider"] != "ppio" || environment["model"] != "model-a" {
		t.Fatalf("environment projection = %#v", status.Environment)
	}
	if _, leaked := environment["api_key"]; leaked {
		t.Fatalf("environment exposed a secret: %#v", environment)
	}
	wire, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "sk-secret") || strings.Contains(string(wire), "must-not-escape") {
		t.Fatalf("status contains secret material: %s", wire)
	}
}

func TestSaveProfileUseCaseWritesOnlyPublicSummary(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	summary, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID:         "team",
		Label:      "Team",
		Provider:   "ppio",
		Model:      "model-a",
		APIKey:     "sk-secret",
		ConfigMode: "provider",
		Protocol:   "openai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != "team" || summary.Protocol != "openai" {
		t.Fatalf("saved summary = %#v", summary)
	}
	if err := core.providers.SaveKey(context.Background(), "ppio", "new-provider-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Label: "Renamed", Provider: "ppio", Model: "model-b",
	}); err != nil {
		t.Fatal(err)
	}
	providerEntry, err := core.providers.Get("ppio")
	if err != nil || providerEntry.APIKey != "new-provider-key" {
		t.Fatalf("profile edit replaced Provider key: %q, %v", providerEntry.APIKey, err)
	}
	if err := core.providers.SaveKey(context.Background(), "novita", "novita-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Label: "Novita", Provider: "novita", Model: "model-c",
	}); err != nil {
		t.Fatalf("profile provider switch with Provider key failed: %v", err)
	}
	providerEntry, err = core.providers.Get("novita")
	if err != nil || providerEntry.APIKey != "novita-key" {
		t.Fatalf("provider switch changed Provider key: %q, %v", providerEntry.APIKey, err)
	}
	status, err := core.GetStatus(context.Background())
	if err != nil || len(status.Profiles) != 1 || status.Profiles[0].ID != "team" {
		t.Fatalf("status after save = %#v, err=%v", status, err)
	}
	wire, err := json.Marshal(summary)
	if err != nil || strings.Contains(string(wire), "sk-secret") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("summary leaked secret data: %s (%v)", wire, err)
	}
}

func TestSaveProfileCanSwitchAKeylessProfileProvider(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "draft", Provider: "ppio", Model: "model-a",
	}); err != nil {
		t.Fatalf("keyless Profile create failed: %v", err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "draft", Provider: "novita", Model: "model-b",
	}); err != nil {
		t.Fatalf("keyless Profile provider switch failed: %v", err)
	}
}

func TestSaveProfileUsesProtocolEndpoint(t *testing.T) {
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Lookup: func(string) (string, bool) { return "", false }})
	if _, err := core.SaveProvider(context.Background(), provider.Entry{ID: "anthropic-only", Name: "Anthropic", AnthropicBaseURL: "https://api.example.test/anthropic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{ID: "anthropic", Provider: "anthropic-only", Model: "model", ConfigMode: "provider", Protocol: "anthropic"}); err != nil {
		t.Fatal(err)
	}
}

func TestStatusProjectsAgentBindingsWithoutUnknownFields(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".oneagent", "agents", "codex.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := `{"schema_version":1,"agent_id":"codex","provider":"ppio","base_url":"https://api.ppio.com/responses","model":"model-a","profile_ref":"team","created_at":"created","updated_at":"updated","api_key":"must-not-escape"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Lookup: func(string) (string, bool) { return "", false }})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	agent := status.Agents["codex"]
	if agent.Provider == nil || *agent.Provider != "ppio" || agent.ProfileID == nil || *agent.ProfileID != "team" || agent.Model == nil || *agent.Model != "model-a" || agent.BaseURL == nil || *agent.BaseURL != "https://api.ppio.com/responses" || agent.UpdatedAt == nil || *agent.UpdatedAt != "updated" {
		t.Fatalf("agent binding projection = %#v", agent)
	}
	wire, err := json.Marshal(status)
	if err != nil || strings.Contains(string(wire), "must-not-escape") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("status binding leaked unknown fields: %s (%v)", wire, err)
	}
}

func TestStatusDetectsExternalConfigWithoutSecretsOrGlobalFailure(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	claudePath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`model_provider = "vendor"
model = "gpt-5-mini"
[model_providers.vendor]
base_url = "https://api.other-vendor.com/v1"
api_key = "sk-detected-secret"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Lookup: func(string) (string, bool) { return "", false }})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	codex := status.Agents["codex"]
	if codex.Detected == nil || codex.Detected.BaseURL != "https://api.other-vendor.com/v1" || codex.Detected.Model != "gpt-5-mini" || codex.Detected.ManagedByOneAgent {
		t.Fatalf("codex detected = %#v", codex.Detected)
	}
	claude := status.Agents["claude-code"]
	if claude.Detected == nil || claude.Detected.Unreadable == nil {
		t.Fatalf("claude malformed detection = %#v", claude.Detected)
	}
	wire, err := json.Marshal(status)
	if err != nil || strings.Contains(string(wire), "sk-detected-secret") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("detected status leaked secret data: %s (%v)", wire, err)
	}
}

func TestStatusReportsInstalledVersionFromVersionCommand(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{"codex": "/fake/codex"}}
	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Runner:   runner,
	})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	codex := status.Agents["codex"]
	if !codex.Installed || codex.Version == nil || *codex.Version != "1.0.0" {
		t.Fatalf("installed version not detected: %#v", codex)
	}
	found := false
	for _, call := range runner.calls {
		if reflect.DeepEqual(call, []string{"/fake/codex", "--version"}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("version command was not invoked: %#v", runner.calls)
	}
}

func TestInstalledVersionsProbeConcurrentlyWithBoundedFanout(t *testing.T) {
	runner := &versionProbeRunner{}
	core := NewUseCases(StatusOptions{Runner: runner, Platform: platform.For("linux", "amd64")})
	manifest := catalog.Manifest{Agents: map[string]catalog.Agent{}}
	for index := range versionProbeConcurrency + 1 {
		manifest.Agents[fmt.Sprintf("agent-%d", index)] = catalog.Agent{Command: fmt.Sprintf("cmd-%d", index), ConfigMode: "auto"}
	}
	versions := core.installedVersions(context.Background(), manifest, runner.LookPath)
	if len(versions) != versionProbeConcurrency+1 || runner.peak.Load() != versionProbeConcurrency {
		t.Fatalf("versions=%d peak=%d, want %d and %d", len(versions), runner.peak.Load(), versionProbeConcurrency+1, versionProbeConcurrency)
	}
}

type versionProbeRunner struct {
	inFlight atomic.Int32
	peak     atomic.Int32
}

func (r *versionProbeRunner) LookPath(command string) (string, bool) { return "/fake/" + command, true }

func (r *versionProbeRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	current := r.inFlight.Add(1)
	for current > r.peak.Load() && !r.peak.CompareAndSwap(r.peak.Load(), current) {
	}
	time.Sleep(10 * time.Millisecond)
	r.inFlight.Add(-1)
	return process.Result{Stdout: "tool 1.0.0"}, nil
}

func TestStatusMatchesEmptyLinuxARM64Fixture(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "arm64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	actualData, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	fixtureData, err := os.ReadFile(filepath.Join("testdata", "status-empty-linux-arm64.json"))
	if err != nil {
		t.Fatal(err)
	}
	var actual any
	var expected any
	if err := json.Unmarshal(actualData, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixtureData, &expected); err != nil {
		t.Fatal(err)
	}
	actual = normalizeFixtureHome(actual, home)
	if !reflect.DeepEqual(actual, expected) {
		actualPretty, _ := json.MarshalIndent(actual, "", "  ")
		expectedPretty, _ := json.MarshalIndent(expected, "", "  ")
		t.Fatalf("status diverged from the frozen fixture\nwant:\n%s\ngot:\n%s", expectedPretty, actualPretty)
	}
}

func TestStatusHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewUseCases(StatusOptions{Platform: platform.For("linux", "amd64")}).GetStatus(ctx)
	if err == nil || !hasSubstring(err.Error(), "cancelled") {
		t.Fatalf("cancellation was not mapped: %v", err)
	}
}

func hasSubstring(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

func normalizeFixtureHome(value any, home string) any {
	switch item := value.(type) {
	case string:
		normalized := strings.ReplaceAll(item, home, "${HOME}")
		if strings.HasPrefix(normalized, "${HOME}") {
			return filepath.ToSlash(normalized)
		}
		return normalized
	case []any:
		for index := range item {
			item[index] = normalizeFixtureHome(item[index], home)
		}
	case map[string]any:
		for key := range item {
			item[key] = normalizeFixtureHome(item[key], home)
		}
	}
	return value
}

func TestStatusReportsFirstRunUntilOneAgentDirExists(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Lookup: func(string) (string, bool) { return "", false }})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.FirstRun {
		t.Fatal("a home without ~/.oneagent must report firstRun")
	}
	if err := os.MkdirAll(filepath.Join(home, ".oneagent"), 0o700); err != nil {
		t.Fatal(err)
	}
	status, err = core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.FirstRun {
		t.Fatal("firstRun must clear once ~/.oneagent exists")
	}
}
