package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/platform"
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
	profileJSON := `{"schema_version":2,"id":"team","label":"Team","provider":"ppio","base_url":"https://api.ppio.com/openai","model":"model-a","config_mode":"provider","agent_ids":["codex","opencode"],"created_at":"created","activated_at":"active","api_key":"must-not-escape"}`
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
	if len(status.Profiles) != 1 || status.Profiles[0].ID != "team" || !status.Profiles[0].HasKey {
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

func TestStatusKeepsLegacyProfileInMemoryAndReportsFailures(t *testing.T) {
	home := t.TempDir()
	oneagentDir := filepath.Join(home, ".oneagent")
	if err := os.MkdirAll(oneagentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"schema_version":1,"provider":"ppio","base_url":"https://api.ppio.com/openai","model":"legacy-model","config_mode":"provider","agent_ids":["codex"],"activated_at":"legacy-time"}`
	if err := os.WriteFile(filepath.Join(oneagentDir, "profile.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Lookup: func(string) (string, bool) { return "", false }})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.ActiveProfile == nil || *status.ActiveProfile != "default" || status.Environment == nil || status.EnvironmentError != nil || len(status.Profiles) != 1 || status.Profiles[0].ID != "default" {
		t.Fatalf("legacy status = %#v", status)
	}

	if err := os.WriteFile(filepath.Join(oneagentDir, "profile.json"), []byte(`{"schema_version":2,"active":"ghost"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.ActiveProfile == nil || *status.ActiveProfile != "ghost" || status.Environment != nil || status.EnvironmentError == nil || !strings.Contains(*status.EnvironmentError, "ghost") {
		t.Fatalf("missing profile status = %#v", status)
	}

	if err := os.MkdirAll(filepath.Join(oneagentDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oneagentDir, "profiles", "ghost.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.EnvironmentError == nil || status.Environment != nil {
		t.Fatalf("corrupt profile status = %#v", status)
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
		AgentIDs:   []string{"opencode", "codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != "team" || !summary.HasKey || !reflect.DeepEqual(summary.AgentIDs, []string{"codex", "opencode"}) {
		t.Fatalf("saved summary = %#v", summary)
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
	if agent.Provider == nil || *agent.Provider != "ppio" || agent.Model == nil || *agent.Model != "model-a" || agent.BaseURL == nil || *agent.BaseURL != "https://api.ppio.com/responses" || agent.UpdatedAt == nil || *agent.UpdatedAt != "updated" {
		t.Fatalf("agent binding projection = %#v", agent)
	}
	wire, err := json.Marshal(status)
	if err != nil || strings.Contains(string(wire), "must-not-escape") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("status binding leaked unknown fields: %s (%v)", wire, err)
	}
}

func TestStatusMatchesPythonEmptyLinuxARM64Fixture(t *testing.T) {
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
		t.Fatalf("status diverged from the frozen Python fixture\nwant:\n%s\ngot:\n%s", expectedPretty, actualPretty)
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
