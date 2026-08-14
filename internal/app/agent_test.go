package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/config"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/provider"
	"gopkg.in/yaml.v3"
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
	if err != nil || strings.Contains(string(configData), "codex-secret") || !strings.Contains(string(configData), `model_provider = "bootagent"`) {
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

// Kimi Code carries the key in the same file as the endpoint, so activation has
// to produce a config that is complete on its own: the CLI documents that it
// does not read credentials from the environment, and a config without the key
// would leave it failing at startup with nothing to fall back to.
func TestActivateKimiCodeWritesAConfigCompleteOnItsOwn(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	result, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID:  "kimi-code",
		Provider: "ppio",
		APIKey:   "kimi-secret",
		Model:    "model-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".kimi-code", "config.toml")
	if result.Config != path {
		t.Fatalf("Kimi Code config path = %q, want %q", result.Config, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"kimi-secret", "api.ppio.com/openai/v1", `type = "openai"`, `default_model = "bootagent/model-a"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("Kimi Code config missing %q: %s", want, text)
		}
	}
	if wire, err := json.Marshal(result); err != nil || strings.Contains(string(wire), "kimi-secret") {
		t.Fatalf("activation result leaked the key: %s (%v)", wire, err)
	}
	// The binding is what a Provider or Profile edit later reapplies through.
	binding, err := core.profiles.ReadAgentBinding("kimi-code")
	if err != nil || binding == nil || binding.Model != "model-a" || binding.Provider != "ppio" {
		t.Fatalf("Kimi Code binding = %#v, err=%v", binding, err)
	}
	// Switching model must not leave the previous alias behind, or Kimi Code keeps
	// a models entry whose provider no longer describes it.
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "kimi-code", Provider: "ppio", APIKey: "kimi-secret", Model: "model-b",
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "bootagent/model-a") {
		t.Fatalf("model switch kept the previous alias: %s", after)
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
		ID: "team", Provider: "ppio", Model: "model-a", APIKey: "legacy-secret",
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
	aider, err := os.ReadFile(filepath.Join(home, ".bootagent", "aider.env"))
	if err != nil || !strings.Contains(string(aider), "adapter-secret-aider") {
		t.Fatalf("Aider config = %q, err=%v", aider, err)
	}
}

func TestActivateAgentRejectsInvalidInputsAndDoesNotPublishFailedBinding(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	for _, options := range []ActivateAgentOptions{
		// Was {AgentID: "cursor"}, standing for "a guide-only Agent cannot be
		// activated". Cursor is no longer in the catalog, which made it a duplicate
		// of the no-such-agent case below and stopped covering the guide-only rule.
		// There is no guide-only Agent in the catalog to name in its place, so the
		// rule is exercised through the real code path in
		// TestActivateAgentRejectsGuideOnlyAgents instead.
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

// Every auto Agent needs both halves of its adapter: a writer, so activation can
// configure it, and a reader, so the overview can report what was configured.
// Adding only the writer is a silent half-failure -- activation succeeds, then
// the row reports "没有可用的配置解析器" and the binding looks broken. The catalog
// declares the adapter name, and nothing else connects it to either dispatch, so
// this walks the real manifest rather than a hand-kept list of ids.
func TestEveryAutoAgentAdapterCanBeWrittenAndReadBack(t *testing.T) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	for agentID, agent := range manifest.Agents {
		if agent.ConfigMode != "auto" {
			continue
		}
		path := configPath(home, "linux", agent)
		if path == "" {
			t.Errorf("auto Agent %s has no configuration path", agentID)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
			AgentID:  agentID,
			Provider: "ppio",
			APIKey:   "sk-adapter-probe",
			Model:    "model-probe",
		}); err != nil {
			t.Errorf("activate %s: %v", agentID, err)
			continue
		}
		detected := config.DetectFile(path, agent.ConfigAdapter, agent.EnvVars)
		if detected == nil {
			t.Errorf("%s wrote no config to %s", agentID, path)
			continue
		}
		if detected.Unreadable != nil {
			t.Errorf("%s wrote a config its own reader rejects: %s", agentID, *detected.Unreadable)
			continue
		}
		// Aider is the standing exception: its file is a two-line .env holding only
		// the endpoint and the key, so there is no field to carry a model or an
		// ownership marker. The model reaches Aider through the --model argument in
		// nextStep instead. Listed explicitly so a *new* adapter cannot land here
		// silently on the strength of Aider's precedent.
		if agentID == "aider" {
			if detected.BaseURL == "" {
				t.Errorf("aider config did not read back an endpoint")
			}
			continue
		}
		if !detected.ManagedByBootAgent {
			t.Errorf("%s config does not read back as managed by BootAgent", agentID)
		}
		if detected.Model != "model-probe" {
			t.Errorf("%s model round-trip = %q, want model-probe", agentID, detected.Model)
		}
	}
}

// The guide-only rejection in activateAgentLocked has no Agent in the catalog to
// trigger it any more: 363550c removed the last three, and OpenClaw was restored
// as config_mode "auto". The rule still has to hold for the next guide-only entry
// added, so this drives the same code path through a manifest of its own rather
// than leaving the branch uncovered until someone rediscovers it in the UI.
func TestActivateAgentRejectsGuideOnlyAgents(t *testing.T) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for id, agent := range manifest.Agents {
		if agent.ConfigMode != "auto" {
			t.Fatalf("catalog gained guide-only Agent %s; point this test at it instead", id)
		}
	}

	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	// codex is auto, so this proves the guard is what rejects it: same inputs,
	// only ConfigMode differs.
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "codex", Provider: "ppio", APIKey: "sk-guard", Model: "model",
	}); err != nil {
		t.Fatalf("auto Agent should activate: %v", err)
	}
	if got := guideOnlyRejection("codex", catalog.Agent{ConfigMode: "guide"}); got == nil {
		t.Fatal("a guide-only Agent must be rejected before any config is written")
	} else if !strings.Contains(got.Error(), "guide-only") {
		t.Fatalf("guide-only rejection = %v", got)
	}
	if got := guideOnlyRejection("codex", catalog.Agent{ConfigMode: "auto"}); got != nil {
		t.Fatalf("auto Agent must not be rejected: %v", got)
	}
}

func TestDSHRouteProviderIDReturnsBuiltInIDOnlyWhenUnoverridden(t *testing.T) {
	tests := []struct {
		name         string
		target       provider.Entry
		explicitBase string
		want         string
	}{
		{
			name:         "built-in DeepSeek with no override",
			target:       provider.Entry{ID: "deepseek", BuiltIn: true},
			explicitBase: "",
			want:         "deepseek",
		},
		{
			name:         "built-in DeepSeek with whitespace-only override",
			target:       provider.Entry{ID: "deepseek", BuiltIn: true},
			explicitBase: "   ",
			want:         "deepseek",
		},
		{
			name:         "built-in DeepSeek with explicit baseURL override",
			target:       provider.Entry{ID: "deepseek", BuiltIn: true},
			explicitBase: "https://api.gateway.example/v1",
			want:         "",
		},
		{
			name:         "custom Provider (never built-in)",
			target:       provider.Entry{ID: "custom-gateway", BuiltIn: false},
			explicitBase: "",
			want:         "",
		},
		{
			name:         "built-in non-DeepSeek Provider",
			target:       provider.Entry{ID: "anthropic", BuiltIn: true},
			explicitBase: "",
			want:         "anthropic",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dshRouteProviderID(tt.target, tt.explicitBase)
			if got != tt.want {
				t.Errorf("dshRouteProviderID(%+v, %q) = %q, want %q", tt.target, tt.explicitBase, got, tt.want)
			}
		})
	}
}

// The thinking depth travels from the Profile through activation into dsh's
// own settings, and a Profile edit that only changes the depth still reaches
// the bound Agent -- the whole point of carrying it on the Profile is that the
// user adjusts it there and every following Agent picks it up.
func TestDSHActivationCarriesProfileReasoningEffort(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	settingsPath := filepath.Join(home, ".dsh", "settings.yaml")
	readSelection := func() map[string]string {
		t.Helper()
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Selection map[string]string `yaml:"agent-default-model"`
		}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed.Selection
	}

	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "think", Provider: "deepseek", Model: "deepseek-v4-pro",
		APIKey: "sk-think", ReasoningEffort: "max",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "dsh", Provider: "deepseek", ProfileID: "think",
	}); err != nil {
		t.Fatal(err)
	}
	selection := readSelection()
	if selection["provider"] != "deepseek-official" || selection["reasoningEffort"] != "max" {
		t.Fatalf("selection after activation = %v, want official route at max", selection)
	}
	binding, err := core.profiles.ReadAgentBinding("dsh")
	if err != nil || binding == nil || binding.ReasoningEffort != "max" {
		t.Fatalf("binding = %#v, err=%v, want reasoningEffort max", binding, err)
	}

	// An effort-only Profile edit must reach the bound Agent's config.
	result, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "think", Provider: "deepseek", Model: "deepseek-v4-pro",
		ReasoningEffort: "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reapplied) != 1 || result.Reapplied[0] != "dsh" {
		t.Fatalf("effort-only edit did not reapply: %#v", result)
	}
	if selection := readSelection(); selection["reasoningEffort"] != "off" {
		t.Fatalf("selection after effort edit = %v, want off", selection)
	}

	// Removing the depth restores the model's own default behavior: the key
	// leaves the selection and the binding.
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "think", Provider: "deepseek", Model: "deepseek-v4-pro",
	}); err != nil {
		t.Fatal(err)
	}
	if selection := readSelection(); selection["reasoningEffort"] != "" {
		t.Fatalf("removed effort survived in the selection: %v", selection)
	}
	binding, err = core.profiles.ReadAgentBinding("dsh")
	if err != nil || binding == nil || binding.ReasoningEffort != "" {
		t.Fatalf("binding after removal = %#v, err=%v, want empty effort", binding, err)
	}
}

// The vocabulary is llm-deepseek's (off, high, max), so a level pi-ai would
// take must be refused at the Profile edit, before it can break requests.
func TestSaveProfileRejectsAnUnsupportedReasoningEffort(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	_, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "bad-effort", Provider: "deepseek", Model: "deepseek-v4-pro",
		APIKey: "sk-x", ReasoningEffort: "medium",
	})
	if err == nil {
		t.Fatal("an unsupported reasoning effort was accepted at the Profile edit")
	}
	if !strings.Contains(err.Error(), "off, high, max") {
		t.Fatalf("rejection does not name the allowed levels: %v", err)
	}
}
