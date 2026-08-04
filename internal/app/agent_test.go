package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

func activationCore(t *testing.T, home string, client *provider.Client, osID string) *UseCases {
	t.Helper()
	return NewUseCasesWithProviderClient(StatusOptions{
		Home:     home,
		Platform: platform.For(osID, "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	}, client)
}

func TestActivateAgentWritesPerAgentStateAndKeepsSecretsOutOfResult(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	result, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID:  "codex",
		Provider: "ppio",
		APIKey:   "codex-secret",
		Model:    "model-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AgentID != "codex" || result.Provider != "ppio" || result.Model != "model-a" {
		t.Fatalf("activation result = %#v", result)
	}
	if !strings.Contains(result.Restart, "codex") || result.Next != "codex" {
		t.Fatalf("activation hints = %#v", result)
	}
	if strings.Contains(result.Restart+result.Next, "codex-secret") {
		t.Fatal("activation hints leaked the API key")
	}

	// Codex authenticates from auth.json beside config.toml, so no environment
	// variable has to be exported before the command works.
	authPath := filepath.Join(home, ".codex", "auth.json")
	authData, err := os.ReadFile(authPath)
	if err != nil || !strings.Contains(string(authData), "codex-secret") || !strings.Contains(string(authData), `"auth_mode": "apikey"`) {
		t.Fatalf("Codex auth.json = %q, err=%v", authData, err)
	}
	configData, err := os.ReadFile(result.Config)
	if err != nil || strings.Contains(string(configData), "codex-secret") || !strings.Contains(string(configData), `model_provider = "oneagent"`) {
		t.Fatalf("Codex config = %q, err=%v", configData, err)
	}
	binding, err := core.profiles.ReadAgentBinding("codex")
	if err != nil || binding == nil || binding.Provider != "ppio" || binding.Model != "model-a" {
		t.Fatalf("binding = %#v, err=%v", binding, err)
	}
	providerEntry, err := core.GetProvider(context.Background(), "ppio")
	if err != nil || providerEntry.APIKey != "codex-secret" {
		t.Fatalf("saved Provider key = %#v, err=%v", providerEntry, err)
	}
	wire, err := json.Marshal(result)
	if err != nil || strings.Contains(string(wire), "codex-secret") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("activation result leaked secret material: %s (%v)", wire, err)
	}

	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID:  "opencode",
		Provider: "novita",
		APIKey:   "other-secret",
		Model:    "model-b",
	}); err != nil {
		t.Fatal(err)
	}
	opencodeConfig, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil || !strings.Contains(string(opencodeConfig), "other-secret") || strings.Contains(string(opencodeConfig), "codex-secret") {
		t.Fatalf("isolated OpenCode config = %q, err=%v", opencodeConfig, err)
	}
	codexAuth, _ := os.ReadFile(authPath)
	if strings.Contains(string(codexAuth), "other-secret") {
		t.Fatal("activating OpenCode changed Codex credentials")
	}
}

func TestActivateAgentReusesProfileKeyAndDiscoversModel(t *testing.T) {
	home := t.TempDir()
	client := provider.NewClient(appProviderDoer(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/openai/v1/models" {
			t.Fatalf("unexpected model discovery request: %s %s", request.Method, request.URL.Path)
		}
		return appProviderResponse(http.StatusOK, `{"data":[{"id":"embed-v1"},{"id":"chat-model"}]}`), nil
	}))
	core := activationCore(t, home, client, "linux")
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID:       "team",
		Provider: "ppio",
		Model:    "saved-model",
		APIKey:   "stored-secret",
		AgentIDs: []string{"opencode"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID:   "opencode",
		Provider:  "ppio",
		ProfileID: "team",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "chat-model" {
		t.Fatalf("resolved model = %q", result.Model)
	}
	binding, err := core.profiles.ReadAgentBinding("opencode")
	if err != nil || binding == nil || binding.ProfileRef != "team" {
		t.Fatalf("profile-linked binding = %#v, err=%v", binding, err)
	}
	// The profile key is the credential OpenCode itself needs, so it belongs in
	// its own private config; the check is that the profile is what supplied it.
	configData, _ := os.ReadFile(result.Config)
	if !strings.Contains(string(configData), "stored-secret") {
		t.Fatalf("OpenCode config did not receive the profile key: %q", configData)
	}
}

func TestActivateAgentPrefersProviderKeyOverLegacyProfileSecret(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Provider: "ppio", Model: "model-a", APIKey: "legacy-secret", AgentIDs: []string{"codex"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := core.providers.SaveKey(context.Background(), "ppio", "provider-secret"); err != nil {
		t.Fatal(err)
	}
	result, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "codex", Provider: "ppio", ProfileID: "team", Model: "model-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil || !strings.Contains(string(auth), "provider-secret") || strings.Contains(string(auth), "legacy-secret") {
		t.Fatalf("provider key was not authoritative: %q, %v", auth, err)
	}
	if result.AgentID != "codex" {
		t.Fatalf("activation result = %#v", result)
	}
}

func TestActivateAgentDispatchesAllManagedAdapters(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	for _, agentID := range []string{"codex", "claude-code", "opencode", "kilo-cli", "aider"} {
		t.Run(agentID, func(t *testing.T) {
			result, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
				AgentID:  agentID,
				Provider: "ppio",
				APIKey:   "adapter-secret-" + agentID,
				Model:    "model-a",
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(result.Config); err != nil {
				t.Fatalf("config %s was not written: %v", result.Config, err)
			}
			if binding, err := core.profiles.ReadAgentBinding(agentID); err != nil || binding == nil {
				t.Fatalf("binding = %#v, err=%v", binding, err)
			}
		})
	}
	claude, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(claude), "adapter-secret-claude-code") {
		t.Fatalf("Claude native config = %q, err=%v", claude, err)
	}
	aider, err := os.ReadFile(filepath.Join(home, ".oneagent", "aider.env"))
	if err != nil || !strings.Contains(string(aider), "adapter-secret-aider") {
		t.Fatalf("Aider config = %q, err=%v", aider, err)
	}
}

func TestActivateAgentRejectsInvalidInputsAndDoesNotPublishFailedBinding(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	for _, options := range []ActivateAgentOptions{
		{AgentID: "cursor", Provider: "ppio", APIKey: "key", Model: "model"},
		{AgentID: "no-such-agent", Provider: "ppio", APIKey: "key", Model: "model"},
		{AgentID: "../escape", Provider: "ppio", APIKey: "key", Model: "model"},
		{AgentID: "codex", Provider: "ppio", Model: "model"},
	} {
		if _, err := core.ActivateAgent(context.Background(), options); err == nil {
			t.Errorf("invalid activation %#v unexpectedly succeeded", options)
		}
	}

	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "{\n  // keep this comment\n  \"theme\": \"dark\"\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID:  "opencode",
		Provider: "ppio",
		APIKey:   "key",
		Model:    "model",
	}); err == nil || !strings.Contains(err.Error(), "JSONC comments") {
		t.Fatalf("JSONC activation error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != original {
		t.Fatalf("failed activation modified JSONC: %q", got)
	}
	if binding, err := core.profiles.ReadAgentBinding("opencode"); err != nil || binding != nil {
		t.Fatalf("failed activation published binding = %#v, err=%v", binding, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := core.ActivateAgent(ctx, ActivateAgentOptions{AgentID: "codex", Provider: "ppio", APIKey: "key", Model: "model"}); err == nil {
		t.Fatal("cancelled activation unexpectedly succeeded")
	}
	if _, err := core.profiles.ReadAgentBinding("codex"); err != nil {
		t.Fatal(err)
	}
}

func TestActivateAgentRejectsUnsupportedPlatform(t *testing.T) {
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.Info{OS: "plan9", Arch: "x64", Shell: "sh"}})
	_, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{AgentID: "codex", Provider: "ppio", APIKey: "key", Model: "model"})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("unsupported platform error = %v", err)
	}
}
