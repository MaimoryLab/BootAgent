package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

func testWriter(t *testing.T, home, osID string) Writer {
	t.Helper()
	filesystem := securefs.New(securefs.Options{OS: osID})
	return NewWriter(home, osID, filesystem)
}

func TestWriteCodexPreservesUnmanagedTablesAndRoundTrips(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("approval_policy = \"on-request\"\nmodel_provider = \"old\"\nmodel = \"old-model\"\n\n[model_providers.old]\nname = \"Keep Me\"\n\n[model_providers.oneagent]\nname = \"Old\"\nbase_url = \"https://old.example\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteCodex(context.Background(), path, "PPIO", "https://api.ppio.com/openai", "sk-codex-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	// The key belongs in auth.json, not in the config Codex shares with unmanaged
	// settings.
	auth, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil || !strings.Contains(string(auth), "sk-codex-secret") {
		t.Fatalf("Codex auth.json = %q, err=%v", auth, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "approval_policy = \"on-request\"") || !strings.Contains(text, "name = \"Keep Me\"") || !strings.Contains(text, "model_provider = \"oneagent\"") {
		t.Fatalf("merged Codex config = %s", text)
	}
	detected := ReadCodexConfig(text)
	if detected.BaseURL != "https://api.ppio.com/openai" || detected.Model != "model-a" || !detected.ManagedByOneAgent || detected.Unreadable != nil {
		t.Fatalf("round-trip Codex detection = %#v", detected)
	}
	for _, invalid := range []string{"model = \"unterminated\n", "[\"model_providers\".\"oneagent\"]\nname = \"quoted\"\n", "\"model\" = \"quoted\"\n"} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteCodex(context.Background(), path, "PPIO", "https://example.com", "sk-codex-secret", "m"); err == nil {
			t.Fatalf("invalid Codex config unexpectedly succeeded: %q", invalid)
		}
		got, _ := os.ReadFile(path)
		if string(got) != invalid {
			t.Fatalf("invalid Codex config was modified: %q", got)
		}
	}
}

func TestWriteKimiCodePreservesUnmanagedEntriesAndRoundTrips(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".kimi-code", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// The parts OneAgent must not own: a top-level preference, the user's own
	// provider and model, and a stale OneAgent provider plus alias from an
	// earlier write.
	existing := strings.Join([]string{
		`default_permission_mode = "manual"`,
		`default_model = "mine/gpt-4o"`,
		``,
		`[providers.mine]`,
		`type = "openai"`,
		`api_key = "keep-me"`,
		``,
		`[models."mine/gpt-4o"]`,
		`provider = "mine"`,
		`model = "gpt-4o"`,
		``,
		`[providers.oneagent]`,
		`type = "openai"`,
		`base_url = "https://old.example/v1"`,
		`api_key = "old-key"`,
		``,
		`[models."oneagent/old-model"]`,
		`provider = "oneagent"`,
		`model = "old-model"`,
		``,
	}, "\n")
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteKimiCode(context.Background(), path, "https://api.ppio.com/openai", "sk-kimi-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, keep := range []string{`default_permission_mode = "manual"`, `[providers.mine]`, `api_key = "keep-me"`, `[models."mine/gpt-4o"]`} {
		if !strings.Contains(text, keep) {
			t.Fatalf("Kimi Code config dropped an unmanaged entry %q: %s", keep, text)
		}
	}
	// The previous alias has to be gone, or Kimi Code keeps a models entry whose
	// provider no longer describes it.
	if strings.Contains(text, "oneagent/old-model") || strings.Contains(text, "old-key") || strings.Contains(text, "old.example") {
		t.Fatalf("Kimi Code config kept the stale OneAgent entry: %s", text)
	}
	detected := ReadKimiCodeConfig(text)
	// Normalised to the /v1 form Kimi Code's `openai` provider type expects, the
	// same as the OpenCode and Kilo adapters.
	if detected.BaseURL != "https://api.ppio.com/openai/v1" || detected.Model != "model-a" || !detected.ManagedByOneAgent || detected.Unreadable != nil {
		t.Fatalf("round-trip Kimi Code detection = %#v", detected)
	}
	// The key shares the file with the endpoint here, unlike Codex, so the file
	// itself must carry secret permissions.
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Kimi Code config mode = %v, err=%v", info.Mode().Perm(), err)
	}
	if !strings.Contains(text, "sk-kimi-secret") {
		t.Fatalf("Kimi Code config is missing the credential it must carry: %s", text)
	}
}

// A model ID containing a dot would split into nested tables unquoted, and the
// alias always contains a slash, so both headers have to survive a round trip.
func TestWriteKimiCodeQuotesAliasesContainingSeparators(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".kimi-code", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteKimiCode(context.Background(), path, "https://api.example.test/v1", "sk-x", "gpt-4.1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	detected := ReadKimiCodeConfig(string(data))
	if detected.Model != "gpt-4.1" || detected.BaseURL != "https://api.example.test/v1" { // already /v1, so unchanged
		t.Fatalf("dotted model round trip = %#v (%s)", detected, data)
	}
}

func TestWriteKimiCodeRefusesUnsupportedSyntaxWithoutWriting(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".kimi-code", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	for _, invalid := range []string{
		"default_model = \"unterminated\n",
		"[\"providers\".\"oneagent\"]\ntype = \"openai\"\n",
		"\"default_model\" = \"quoted\"\n",
	} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteKimiCode(context.Background(), path, "https://example.com/v1", "sk-x", "m"); err == nil {
			t.Fatalf("invalid Kimi Code config unexpectedly succeeded: %q", invalid)
		}
		got, _ := os.ReadFile(path)
		if string(got) != invalid {
			t.Fatalf("invalid Kimi Code config was modified: %q", got)
		}
	}
}

func TestWriteJSONAdaptersPreserveFieldsAndRejectJSONC(t *testing.T) {
	home := t.TempDir()
	writer := testWriter(t, home, "linux")
	claudePath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte(`{"keep":true,"env":{ "CUSTOM":"value" }}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteClaude(context.Background(), claudePath, "https://anthropic.example", "sk-claude-secret", "model-a", ""); err != nil {
		t.Fatal(err)
	}
	var claude map[string]any
	data, _ := os.ReadFile(claudePath)
	if err := json.Unmarshal(data, &claude); err != nil || claude["keep"] != true {
		t.Fatalf("Claude config = %s, %v", data, err)
	}
	if got := string(data); !strings.Contains(got, "\"keep\": true,\n  \"env\":") {
		t.Fatalf("Claude top-level key order changed: %s", data)
	}
	env := claude["env"].(map[string]any)
	if env["ANTHROPIC_SMALL_FAST_MODEL"] != "model-a" || env["CUSTOM"] != "value" {
		t.Fatalf("Claude env = %#v", env)
	}
	if got := string(data); !strings.Contains(got, "\"CUSTOM\": \"value\",\n    \"ANTHROPIC_BASE_URL\":") {
		t.Fatalf("Claude nested key order changed: %s", data)
	}
	if !strings.Contains(string(data), "sk-claude-secret") {
		t.Fatal("Claude config did not contain its required native credential")
	}

	openPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(openPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(openPath, []byte(`{"keep":true,"provider":{"other":{"x":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteOpenAICompatible(context.Background(), openPath, "https://opencode.ai/config.json", "PPIO", "https://api.ppio.com/openai", "sk-opencode-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	var open map[string]any
	data, _ = os.ReadFile(openPath)
	if err := json.Unmarshal(data, &open); err != nil || open["keep"] != true || open["model"] != "oneagent/model-a" {
		t.Fatalf("OpenCode config = %s, %v", data, err)
	}
	if got := string(data); !strings.Contains(got, "\"keep\": true,\n  \"provider\":") || !strings.Contains(got, "\"provider\":") {
		t.Fatalf("OpenCode top-level key order changed: %s", data)
	}
	providers := open["provider"].(map[string]any)
	if _, ok := providers["other"]; !ok {
		t.Fatalf("unmanaged provider removed: %#v", providers)
	}
	detected := ReadOpenAICompatibleConfig(string(data))
	if detected.BaseURL != "https://api.ppio.com/openai/v1" || detected.Model != "model-a" || !detected.ManagedByOneAgent {
		t.Fatalf("OpenCode round-trip = %#v", detected)
	}
	// The Agent reads its own key from this file, so it must be there — and the
	// file must not be world-readable.
	if !strings.Contains(string(data), "sk-opencode-secret") {
		t.Fatalf("OpenCode config lost its credential: %s", data)
	}
	info, err := os.Stat(openPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("OpenCode config mode = %v, err=%v", info.Mode().Perm(), err)
	}

	if err := os.WriteFile(openPath, []byte("{\n // keep\n \"theme\": \"dark\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteOpenAICompatible(context.Background(), openPath, "schema", "PPIO", "https://api.ppio.com/openai", "k", "m"); err == nil || !strings.Contains(err.Error(), "JSONC comments") {
		t.Fatalf("JSONC write error = %v", err)
	}
}

// OpenClaw is a gateway: OneAgent installs it and points its model provider at
// a Provider, and everything about running it stays with OpenClaw's own
// commands. So the sections a user configures through `openclaw onboard` --
// channels, tools, the daemon -- must come back out of a write untouched. That
// separation is the whole reason this adapter is allowed to exist without
// widening the product boundary, which makes it worth a test of its own rather
// than only a golden file.
func TestWriteOpenClawConfiguresProviderWithoutOwningTheGateway(t *testing.T) {
	home := t.TempDir()
	writer := testWriter(t, home, "linux")
	path := filepath.Join(home, ".openclaw", "openclaw.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := `{"channels":{"discord":{"allowFrom":["user#1"]}},` +
		`"tools":{"profile":"safe","deny":["shell"]},` +
		`"models":{"providers":{"other":{"apiKey":"keep-me"}}},` +
		`"agents":{"defaults":{"workspace":"~/w","model":{"fallbacks":["other/m"]}}}}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteOpenClaw(context.Background(), path, "PPIO", "https://api.ppio.com/openai", "sk-openclaw-secret", "model-a"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("OpenClaw config = %s, %v", data, err)
	}

	// Nothing about pairing or tool permissions is OneAgent's to decide.
	channels, _ := config["channels"].(map[string]any)
	discord, _ := channels["discord"].(map[string]any)
	if allow, _ := discord["allowFrom"].([]any); len(allow) != 1 || allow[0] != "user#1" {
		t.Fatalf("paired channel was rewritten: %s", data)
	}
	tools, _ := config["tools"].(map[string]any)
	if tools["profile"] != "safe" {
		t.Fatalf("tools profile was rewritten: %s", data)
	}
	if deny, _ := tools["deny"].([]any); len(deny) != 1 || deny[0] != "shell" {
		t.Fatalf("tool denials were rewritten: %s", data)
	}

	models, _ := config["models"].(map[string]any)
	providers, _ := models["providers"].(map[string]any)
	other, _ := providers["other"].(map[string]any)
	if other["apiKey"] != "keep-me" {
		t.Fatalf("another provider's credential was lost: %s", data)
	}
	entry, _ := providers["oneagent"].(map[string]any)
	if entry["name"] != "PPIO" || entry["api"] != "openai-completions" {
		t.Fatalf("OpenClaw provider entry = %#v", entry)
	}
	// camelCase, and /v1 appended: this is what OpenClaw reads, and it differs
	// from the snake_case other adapters in this file use.
	if entry["baseUrl"] != "https://api.ppio.com/openai/v1" {
		t.Fatalf("OpenClaw baseUrl = %#v", entry["baseUrl"])
	}
	if entry["apiKey"] != "sk-openclaw-secret" {
		t.Fatalf("OpenClaw config lost its credential: %s", data)
	}
	entryModels, _ := entry["models"].([]any)
	first, _ := entryModels[0].(map[string]any)
	if len(entryModels) != 1 || first["id"] != "model-a" {
		t.Fatalf("OpenClaw model list = %#v", entryModels)
	}

	// A gateway addresses a model as "<provider-key>/<model-id>", so a bare model
	// id here would leave OpenClaw pointing at nothing. The user's own fallbacks
	// must survive alongside it.
	agents, _ := config["agents"].(map[string]any)
	defaults, _ := agents["defaults"].(map[string]any)
	if defaults["workspace"] != "~/w" {
		t.Fatalf("workspace was rewritten: %s", data)
	}
	model, _ := defaults["model"].(map[string]any)
	if model["primary"] != "oneagent/model-a" {
		t.Fatalf("default model = %#v", model)
	}
	if fallbacks, _ := model["fallbacks"].([]any); len(fallbacks) != 1 || fallbacks[0] != "other/m" {
		t.Fatalf("user fallbacks were dropped: %s", data)
	}

	// The file holds a live credential.
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("OpenClaw config mode = %v, err=%v", info.Mode().Perm(), err)
	}

	// openclaw.json is documented as JSON5, so comments are expected in the wild
	// and silently dropping them would lose real user content.
	if err := os.WriteFile(path, []byte("{\n // keep\n \"tools\": {}\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteOpenClaw(context.Background(), path, "PPIO", "https://api.ppio.com/openai", "k", "m"); err == nil || !strings.Contains(err.Error(), "JSONC comments") {
		t.Fatalf("JSONC write error = %v", err)
	}
}

func TestWriteWorkBuddyUpdatesModelArrayAndPreservesOtherModels(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".workbuddy", "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := `[{"id":"keep","name":"Keep","vendor":"Custom","url":"https://keep.example","apiKey":"keep-key","extra":{"nested":true}},{"id":"model-a","name":"Old","vendor":"Custom","url":"https://old.example","apiKey":"old-key","unknown":"kept"}]`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteWorkBuddy(context.Background(), path, "https://api.example/openai", "new-key", "model-a"); err != nil {
		t.Fatal(err)
	}
	var models []map[string]any
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &models) != nil || len(models) != 2 {
		t.Fatalf("WorkBuddy config = %s, err=%v", data, err)
	}
	if models[0]["id"] != "keep" || models[0]["extra"].(map[string]any)["nested"] != true {
		t.Fatalf("unmanaged WorkBuddy model changed: %#v", models[0])
	}
	updated := models[1]
	if updated["id"] != "model-a" || updated["name"] != "model-a" || updated["vendor"] != "Custom" ||
		updated["url"] != "https://api.example/openai" || updated["apiKey"] != "new-key" || updated["unknown"] != "kept" ||
		updated["supportsToolCall"] != true || updated["supportsImages"] != false || updated["supportsReasoning"] != false || updated["useCustomProtocol"] != false {
		t.Fatalf("updated WorkBuddy model = %#v", updated)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("WorkBuddy config mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

func TestWriteWorkBuddyRejectsInvalidFiles(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".workbuddy", "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	for _, invalid := range []string{`{"models":"not-an-array"}`, `null`} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteWorkBuddy(context.Background(), path, "https://api.example", "key", "model-c"); err == nil {
			t.Fatal("invalid WorkBuddy config unexpectedly succeeded")
		}
		if data, _ := os.ReadFile(path); string(data) != invalid {
			t.Fatalf("invalid WorkBuddy config was modified: %s", data)
		}
	}
}

func TestWriteAiderQuotesSecretsOnUnixAndWindows(t *testing.T) {
	linuxHome := t.TempDir()
	linux := testWriter(t, linuxHome, "linux")
	linuxPath := filepath.Join(linuxHome, ".oneagent", "aider.env")
	if err := linux.WriteAider(context.Background(), linuxPath, "https://api.example/openai", "key'quoted"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(linuxPath)
	if !strings.Contains(string(data), "OPENAI_API_BASE=https://api.example/openai/v1") || !strings.Contains(string(data), `'key\'quoted'`) {
		t.Fatalf("Unix Aider config = %q", data)
	}
	if detected := ReadAiderConfig(string(data)); detected.BaseURL != "https://api.example/openai/v1" {
		t.Fatalf("Unix Aider round-trip = %#v", detected)
	}

	windowsHome := t.TempDir()
	filesystem := securefs.New(securefs.Options{OS: "windows", Username: "tester", Run: func(context.Context, []string) error { return nil }})
	windows := NewWriter(windowsHome, "windows", filesystem)
	windowsPath := filepath.Join(windowsHome, ".oneagent", "aider.env")
	if err := windows.WriteAider(context.Background(), windowsPath, "https://api.example/openai", "key'quoted"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(windowsPath)
	if !strings.Contains(string(data), `'key\'quoted'`) {
		t.Fatalf("Windows Aider config = %q", data)
	}
}

func TestWriteHermesPreservesExistingConfig(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".hermes", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "tools:\n  enabled: true\nmodel:\n  default: old\n  context_length: 32000\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testWriter(t, home, "linux").WriteHermes(context.Background(), path, "https://api.example/openai", "hermes-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"enabled: true", "context_length: 32000", "default: model-a", "provider: custom", "api_key: hermes-secret", "base_url: https://api.example/openai/v1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Hermes config missing %q:\n%s", want, text)
		}
	}
	detected := ReadHermesConfig(text)
	if detected.BaseURL != "https://api.example/openai/v1" || detected.Model != "model-a" || !detected.ManagedByOneAgent {
		t.Fatalf("Hermes round-trip = %#v", detected)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Hermes config mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

// The real ~/.zcode/v2/config.json holds builtin:* entries the app owns and
// UUID-keyed custom ones. This fixture reproduces that shape, because preserving
// both is the whole risk of writing into a file OneAgent does not own.
const zcodeExisting = `{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "builtin:zai": {
      "name": "Z.ai - API Key",
      "kind": "anthropic",
      "options": {"apiKey": "", "baseURL": "https://api.z.ai/api/anthropic"},
      "source": "custom"
    },
    "ed76b5b4-63bb-4cf0-b0f2-7fb651c50cac": {
      "name": "Mine",
      "kind": "anthropic",
      "options": {"apiKey": "user-key", "baseURL": "https://user.example/v1"},
      "source": "custom",
      "models": {"user/model": {"limit": {"context": 1000000}}}
    }
  }
}`

func zcodeProviders(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Schema   string                    `json:"$schema"`
		Provider map[string]map[string]any `json:"provider"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("ZCode config is not valid JSON: %s: %v", data, err)
	}
	if document.Schema != "https://opencode.ai/config.json" {
		t.Fatalf("$schema = %q", document.Schema)
	}
	return document.Provider
}

func TestWriteZCodeAddsAProviderAndKeepsTheAppsOwnEntries(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".zcode", "v2", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(zcodeExisting), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteZCode(context.Background(), path, "PPIO", "https://api.ppio.com/openai", "sk-zcode", "deepseek-v4-pro"); err != nil {
		t.Fatal(err)
	}
	providers := zcodeProviders(t, path)
	if len(providers) != 3 {
		t.Fatalf("provider count = %d, want 3: %#v", len(providers), providers)
	}
	// Both pre-existing entries survive untouched, including the nested limit the
	// user's own entry carries.
	if providers["builtin:zai"]["name"] != "Z.ai - API Key" {
		t.Errorf("builtin entry changed: %#v", providers["builtin:zai"])
	}
	mine := providers["ed76b5b4-63bb-4cf0-b0f2-7fb651c50cac"]
	if mine["name"] != "Mine" || mine["models"].(map[string]any)["user/model"] == nil {
		t.Errorf("user entry changed: %#v", mine)
	}

	var written map[string]any
	for key, entry := range providers {
		if entry["name"] == "OneAgent - PPIO" {
			written = entry
			if strings.HasPrefix(key, "builtin:") {
				t.Errorf("OneAgent reused a builtin key: %q", key)
			}
		}
	}
	if written == nil {
		t.Fatalf("no OneAgent entry written: %#v", providers)
	}
	if written["kind"] != "openai" || written["source"] != "custom" {
		t.Errorf("entry shape = %#v", written)
	}
	options := written["options"].(map[string]any)
	// OpenAIBaseURL normalizes to a /v1 base; apiKeyRequired must be set or ZCode
	// sends the request unauthenticated.
	if options["baseURL"] != "https://api.ppio.com/openai/v1" || options["apiKey"] != "sk-zcode" || options["apiKeyRequired"] != true {
		t.Errorf("options = %#v", options)
	}
	if written["models"].(map[string]any)["deepseek-v4-pro"] == nil {
		t.Errorf("model was not registered: %#v", written["models"])
	}
	// No top-level model key: which model is active stays ZCode's decision, the
	// same division WriteWorkBuddy keeps.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	if _, present := top["model"]; present {
		t.Errorf("wrote a top-level model key: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("ZCode config mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

// ZCode keys custom providers by UUID, so a second write that generated a fresh
// key would leave the user with a duplicate provider per reconfiguration.
func TestWriteZCodeUpdatesItsOwnEntryInsteadOfAddingAnother(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".zcode", "v2", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(zcodeExisting), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	for _, step := range []struct{ key, model string }{{"first-key", "model-a"}, {"second-key", "model-b"}} {
		if err := writer.WriteZCode(context.Background(), path, "PPIO", "https://api.ppio.com/openai", step.key, step.model); err != nil {
			t.Fatal(err)
		}
	}
	providers := zcodeProviders(t, path)
	if len(providers) != 3 {
		t.Fatalf("a second write added an entry: %d providers: %#v", len(providers), providers)
	}
	for _, entry := range providers {
		if entry["name"] != "OneAgent - PPIO" {
			continue
		}
		if entry["options"].(map[string]any)["apiKey"] != "second-key" {
			t.Errorf("key was not updated: %#v", entry["options"])
		}
		if entry["models"].(map[string]any)["model-b"] == nil {
			t.Errorf("model was not updated: %#v", entry["models"])
		}
	}
}

func TestWriteZCodeCreatesTheFileWhenAbsent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".zcode", "v2", "config.json")
	writer := testWriter(t, home, "linux")
	if err := writer.WriteZCode(context.Background(), path, "Novita", "https://api.novita.ai/openai", "sk-new", "m"); err != nil {
		t.Fatal(err)
	}
	providers := zcodeProviders(t, path)
	if len(providers) != 1 {
		t.Fatalf("provider count = %d, want 1", len(providers))
	}
}
