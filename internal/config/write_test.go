package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/securefs"
	"gopkg.in/yaml.v3"
)

func testWriter(t *testing.T, home, osID string) Writer {
	t.Helper()
	filesystem := securefs.New(securefs.Options{OS: osID})
	return NewWriter(home, osID, filesystem)
}

func TestWriteCodexMapsReasoningEffortToCodexEnum(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	// Profile "max" → Codex "xhigh"
	if err := writer.WriteCodex(context.Background(), path, "P", "https://example.test", "k", "m", "max"); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(path)
	if !strings.Contains(string(text), `model_reasoning_effort = "xhigh"`) {
		t.Fatalf(`Codex "max" should map to "xhigh": %s`, text)
	}
	// Profile "off" → Codex "none"
	if err := writer.WriteCodex(context.Background(), path, "P", "https://example.test", "k", "m", "off"); err != nil {
		t.Fatal(err)
	}
	text, _ = os.ReadFile(path)
	if !strings.Contains(string(text), `model_reasoning_effort = "none"`) {
		t.Fatalf(`Codex "off" should map to "none": %s`, text)
	}
	// mid-range passes through
	for _, level := range []string{"low", "medium", "high"} {
		if err := writer.WriteCodex(context.Background(), path, "P", "https://example.test", "k", "m", level); err != nil {
			t.Fatal(err)
		}
		text, _ = os.ReadFile(path)
		if !strings.Contains(string(text), `model_reasoning_effort = "`+level+`"`) {
			t.Fatalf("Codex %q should pass through: %s", level, text)
		}
	}
	// empty clears the key
	if err := writer.WriteCodex(context.Background(), path, "P", "https://example.test", "k", "m", ""); err != nil {
		t.Fatal(err)
	}
	text, _ = os.ReadFile(path)
	if strings.Contains(string(text), "model_reasoning_effort") {
		t.Fatalf("Codex empty effort should clear the key: %s", text)
	}
}

func TestWriteCodexPreservesUnmanagedTablesAndRoundTrips(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("approval_policy = \"on-request\"\nmodel_provider = \"old\"\nmodel = \"old-model\"\n\n[model_providers.old]\nname = \"Keep Me\"\n\n[model_providers.bootagent]\nname = \"Old\"\nbase_url = \"https://old.example\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteCodex(context.Background(), path, "PPIO", "https://api.ppio.com/openai", "sk-codex-secret", "model-a", ""); err != nil {
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
	if !strings.Contains(text, "approval_policy = \"on-request\"") || !strings.Contains(text, "name = \"Keep Me\"") || !strings.Contains(text, "model_provider = \"bootagent\"") {
		t.Fatalf("merged Codex config = %s", text)
	}
	detected := ReadCodexConfig(text)
	if detected.BaseURL != "https://api.ppio.com/openai" || detected.Model != "model-a" || !detected.ManagedByBootAgent || detected.Unreadable != nil {
		t.Fatalf("round-trip Codex detection = %#v", detected)
	}
	for _, invalid := range []string{"model = \"unterminated\n", "[\"model_providers\".\"bootagent\"]\nname = \"quoted\"\n", "\"model\" = \"quoted\"\n"} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteCodex(context.Background(), path, "PPIO", "https://example.com", "sk-codex-secret", "m", ""); err == nil {
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
	// The parts BootAgent must not own: a top-level preference, the user's own
	// provider and model, and a stale BootAgent provider plus alias from an
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
		`[providers.bootagent]`,
		`type = "openai"`,
		`base_url = "https://old.example/v1"`,
		`api_key = "old-key"`,
		``,
		`[models."bootagent/old-model"]`,
		`provider = "bootagent"`,
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
	// Without a positive max_context_size Kimi Code 0.27.0 discards the whole
	// models entry and then refuses to start, so the alias must carry one.
	if !strings.Contains(text, "max_context_size = 262144") {
		t.Fatalf("Kimi Code models entry is missing max_context_size: %s", text)
	}
	// The previous alias has to be gone, or Kimi Code keeps a models entry whose
	// provider no longer describes it.
	if strings.Contains(text, "bootagent/old-model") || strings.Contains(text, "old-key") || strings.Contains(text, "old.example") {
		t.Fatalf("Kimi Code config kept the stale BootAgent entry: %s", text)
	}
	detected := ReadKimiCodeConfig(text)
	// Normalised to the /v1 form Kimi Code's `openai` provider type expects, the
	// same as the OpenCode and Kilo adapters.
	if detected.BaseURL != "https://api.ppio.com/openai/v1" || detected.Model != "model-a" || !detected.ManagedByBootAgent || detected.Unreadable != nil {
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
		"[\"providers\".\"bootagent\"]\ntype = \"openai\"\n",
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

func TestWriteOpenAICompatibleReasoningEffortLandsInModelOptions(t *testing.T) {
	home := t.TempDir()
	writer := testWriter(t, home, "linux")
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteOpenAICompatible(context.Background(), path, "schema", "PPIO", "https://api.example/openai", "k", "model-a", "high"); err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("OpenCode config = %s, %v", data, err)
	}
	providers := config["provider"].(map[string]any)
	bootagent := providers["bootagent"].(map[string]any)
	models := bootagent["models"].(map[string]any)
	model := models["model-a"].(map[string]any)
	options, _ := model["options"].(map[string]any)
	if options == nil || options["reasoningEffort"] != "high" {
		t.Fatalf("model options should carry the effort: %s", data)
	}
	// An empty effort rebuilds the entry without options.
	if err := writer.WriteOpenAICompatible(context.Background(), path, "schema", "PPIO", "https://api.example/openai", "k", "model-a", ""); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "reasoningEffort") {
		t.Fatalf("empty effort should clear the model options: %s", data)
	}
	// off and max are not on the scale these Agents forward.
	for _, effort := range []string{"off", "max"} {
		if err := writer.WriteOpenAICompatible(context.Background(), path, "schema", "PPIO", "https://api.example/openai", "k", "model-a", effort); err == nil || !strings.Contains(err.Error(), "reasoning effort") {
			t.Fatalf("OpenCode %q expected a scale error, got %v", effort, err)
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
	if err := writer.WriteOpenAICompatible(context.Background(), openPath, "https://opencode.ai/config.json", "PPIO", "https://api.ppio.com/openai", "sk-opencode-secret", "model-a", ""); err != nil {
		t.Fatal(err)
	}
	var open map[string]any
	data, _ = os.ReadFile(openPath)
	if err := json.Unmarshal(data, &open); err != nil || open["keep"] != true || open["model"] != "bootagent/model-a" {
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
	if detected.BaseURL != "https://api.ppio.com/openai/v1" || detected.Model != "model-a" || !detected.ManagedByBootAgent {
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
	if err := writer.WriteOpenAICompatible(context.Background(), openPath, "schema", "PPIO", "https://api.ppio.com/openai", "k", "m", ""); err == nil || !strings.Contains(err.Error(), "JSONC comments") {
		t.Fatalf("JSONC write error = %v", err)
	}
}

// OpenClaw is a gateway: BootAgent installs it and points its model provider at
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

	// Nothing about pairing or tool permissions is BootAgent's to decide.
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
	entry, _ := providers["bootagent"].(map[string]any)
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
	if model["primary"] != "bootagent/model-a" {
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
	linuxPath := filepath.Join(linuxHome, ".bootagent", "aider.env")
	if err := linux.WriteAider(context.Background(), linuxPath, "https://api.example/openai", "key'quoted", ""); err != nil {
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
	windowsPath := filepath.Join(windowsHome, ".bootagent", "aider.env")
	if err := windows.WriteAider(context.Background(), windowsPath, "https://api.example/openai", "key'quoted", ""); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(windowsPath)
	if !strings.Contains(string(data), `'key\'quoted'`) {
		t.Fatalf("Windows Aider config = %q", data)
	}
}

func TestWriteAiderReasoningEffortWritesEnvAndRejectsForeignScale(t *testing.T) {
	home := t.TempDir()
	writer := testWriter(t, home, "linux")
	envPath := filepath.Join(home, ".bootagent", "aider.env")
	// The OpenAI scale passes through as AIDER_REASONING_EFFORT.
	for _, level := range []string{"low", "medium", "high"} {
		if err := writer.WriteAider(context.Background(), envPath, "https://example.test", "k", level); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(envPath)
		if !strings.Contains(string(data), "AIDER_REASONING_EFFORT="+level) {
			t.Fatalf("aider %q should land in the env file: %s", level, data)
		}
	}
	// An empty effort rebuilds the file without the line.
	if err := writer.WriteAider(context.Background(), envPath, "https://example.test", "k", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(envPath)
	if strings.Contains(string(data), "AIDER_REASONING_EFFORT") {
		t.Fatalf("aider empty effort should clear the line: %s", data)
	}
	// off and max are not values the OpenAI scale has; aider forwards the string
	// verbatim, so both must be refused before they can break every request.
	for _, effort := range []string{"off", "max"} {
		err := writer.WriteAider(context.Background(), envPath, "https://example.test", "k", effort)
		if err == nil || !strings.Contains(err.Error(), "reasoning effort") {
			t.Fatalf("aider %q expected a scale error, got %v", effort, err)
		}
		after, _ := os.ReadFile(envPath)
		if string(after) != string(data) {
			t.Fatalf("a refused effort must not modify the file: %s", after)
		}
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
	if detected.BaseURL != "https://api.example/openai/v1" || detected.Model != "model-a" || !detected.ManagedByBootAgent {
		t.Fatalf("Hermes round-trip = %#v", detected)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Hermes config mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

// Reproduces the shape of a real ~/.zcode/v2/config.json: builtin:* entries the
// app owns beside UUID-keyed custom ones. Preserving both is the whole risk of
// writing into a file BootAgent does not own.
//
// Taken from ZCode 3.1.3, which carries no "$schema" key. 3.1.1 did, so this
// fixture doubles as a record of the version the assumptions were checked against.
const zcodeExisting = `{
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
	// 3.1.3 removed this key, so writing it back would reintroduce a field the app
	// deliberately dropped.
	if document.Schema != "" {
		t.Fatalf("wrote a $schema key the current ZCode does not use: %q", document.Schema)
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
		if entry["name"] == "BootAgent - PPIO" {
			written = entry
			if strings.HasPrefix(key, "builtin:") {
				t.Errorf("BootAgent reused a builtin key: %q", key)
			}
		}
	}
	if written == nil {
		t.Fatalf("no BootAgent entry written: %#v", providers)
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
		if entry["name"] != "BootAgent - PPIO" {
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

// dshSettings returns the pi-ai routes in a settings document.
func dshSettings(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		PiAI struct {
			Providers map[string]map[string]any `yaml:"providers"`
		} `yaml:"llm-pi-ai"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("settings are not valid YAML: %v\n%s", err, data)
	}
	return parsed.PiAI.Providers
}

// dshCredentials returns the credential document as the strict mapping dsh
// requires it to be: any other shape fails on dsh's side rather than being
// skipped, so the test asserts the shape too.
func dshCredentials(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed := map[string]string{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("credentials are not a string mapping: %v\n%s", err, data)
	}
	return parsed
}

// Both files BootAgent writes for dsh are the user's, shared with dsh's own
// Models page: settings.yaml holds unrelated sections and the user's own provider
// routes, and .credentials.yaml holds the keys that page manages. Neither may be
// rewritten wholesale the way .bootagent/aider.env is.
func TestWriteDSHRegistersARouteBesideTheUsersOwn(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	credentials := filepath.Join(home, ".dsh", ".credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "ui-onboarding:\n" +
		"  welcomeNoticeVersion: 2026-08-13.1\n" +
		"llm-pi-ai:\n" +
		"  providers:\n" +
		"    paigod:\n" +
		"      displayName: paigod\n" +
		"      apiKeyEnv: PAIGOD_API_KEY\n" +
		"      api: openai-completions\n" +
		"      baseURL: https://apiproxy.paigod.work/v1\n" +
		"      models:\n" +
		"        - id: gpt-5.4\n" +
		// What dsh's own Models page leaves behind once the user has picked a
		// model: a complete selection, effort included, against a shipped route.
		"agent-default-model:\n" +
		"  provider: deepseek-official\n" +
		"  model: deepseek-v4-flash\n" +
		"  reasoningEffort: high\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	// The credential dsh's Models page manages for its shipped DeepSeek route.
	if err := os.WriteFile(credentials, []byte("DEEPSEEK_API_KEY: sk-users-own\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example/openai", "sk-new", "deepseek-v4-pro"); err != nil {
		t.Fatal(err)
	}

	routes := dshSettings(t, path)
	if _, ok := routes["paigod"]; !ok {
		t.Errorf("the user's own route was lost: %v", routes)
	}
	route := routes["bootagent"]
	if route == nil {
		t.Fatalf("no bootagent route was written: %v", routes)
	}
	for key, want := range map[string]any{
		"displayName": "PPIO",
		"apiKeyEnv":   "BOOTAGENT_API_KEY",
		"api":         "openai-completions",
		// The /v1 the adapter needs, since it appends only the operation path.
		"baseURL": "https://api.example/openai/v1",
	} {
		if route[key] != want {
			t.Errorf("route %s = %v, want %v", key, route[key], want)
		}
	}
	// A hand-declared route inherits no catalog, so an absent models list would
	// leave dsh's picker empty for this provider.
	models, _ := route["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("route models = %v, want exactly the resolved model", route["models"])
	}
	if entry, _ := models[0].(map[string]any); entry["id"] != "deepseek-v4-pro" {
		t.Errorf("seeded model = %v, want deepseek-v4-pro", models[0])
	}
	// An unrelated section keeps its place.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ui-onboarding") {
		t.Errorf("unrelated settings section was lost:\n%s", data)
	}

	// Registering the route only makes it available; dsh resolves every new Agent
	// from the default selection, so activation has to move that too.
	var selection struct {
		Default struct {
			Provider        string `yaml:"provider"`
			Model           string `yaml:"model"`
			ReasoningEffort string `yaml:"reasoningEffort"`
		} `yaml:"agent-default-model"`
	}
	if err := yaml.Unmarshal(data, &selection); err != nil {
		t.Fatal(err)
	}
	if selection.Default.Provider != "bootagent" || selection.Default.Model != "deepseek-v4-pro" {
		t.Errorf("default selection = %+v, want the bootagent route", selection.Default)
	}
	// dsh treats a saved selection as complete, so an effort belonging to the
	// model this one replaced must not ride along.
	if selection.Default.ReasoningEffort != "" {
		t.Errorf("stale reasoningEffort survived: %q", selection.Default.ReasoningEffort)
	}

	stored := dshCredentials(t, credentials)
	if stored["BOOTAGENT_API_KEY"] != "sk-new" {
		t.Errorf("credential = %q, want sk-new", stored["BOOTAGENT_API_KEY"])
	}
	// BootAgent uses its own reference precisely so storing it cannot disturb the
	// one dsh manages for the shipped DeepSeek route.
	if stored["DEEPSEEK_API_KEY"] != "sk-users-own" {
		t.Errorf("the user's own credential was disturbed: %q", stored["DEEPSEEK_API_KEY"])
	}

	detected := ReadDSHConfig(string(data))
	if detected.BaseURL != "https://api.example/openai/v1" || detected.Model != "deepseek-v4-pro" || !detected.ManagedByBootAgent {
		t.Fatalf("dsh round-trip = %#v", detected)
	}
	// dsh refuses to parse a credential document carrying any group or other
	// permission bit, and fails before reading it.
	for _, target := range []string{path, credentials} {
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, err=%v", target, info.Mode().Perm(), err)
		}
	}
}

// Activation reruns on every Provider edit, so a rewrite must not accumulate
// duplicate routes, models, or credential entries.
func TestWriteDSHIsIdempotent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	for range 3 {
		if err := writer.WriteDSH(context.Background(), path, "PPIO", "https://api.example/openai", "sk-x", "m1"); err != nil {
			t.Fatal(err)
		}
	}
	routes := dshSettings(t, path)
	if len(routes) != 1 {
		t.Fatalf("route count = %d, want 1: %v", len(routes), routes)
	}
	if models, _ := routes["bootagent"]["models"].([]any); len(models) != 1 {
		t.Errorf("model count = %d, want 1", len(models))
	}
	if stored := dshCredentials(t, filepath.Join(home, ".dsh", ".credentials.yaml")); len(stored) != 1 {
		t.Errorf("credential count = %d, want 1: %v", len(stored), stored)
	}
}

// The route is BootAgent's own, so redefining it must not leave a field from the
// activation before it behind -- a stale compat block or a narrower model list
// would keep serving under a route the user believes was just repointed.
func TestWriteDSHReplacesItsOwnRouteWholesale(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "llm-pi-ai:\n" +
		"  providers:\n" +
		"    bootagent:\n" +
		"      displayName: Stale\n" +
		"      apiKeyEnv: BOOTAGENT_API_KEY\n" +
		"      api: openai-completions\n" +
		"      baseURL: https://stale.example/v1\n" +
		"      compat:\n" +
		"        thinkingFormat: deepseek\n" +
		"      models:\n" +
		"        - id: stale-model\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example/openai", "sk-x", "fresh-model"); err != nil {
		t.Fatal(err)
	}
	route := dshSettings(t, path)["bootagent"]
	if _, ok := route["compat"]; ok {
		t.Errorf("a stale field survived the rewrite: %v", route)
	}
	if route["baseURL"] != "https://api.example/openai/v1" || route["displayName"] != "PPIO" {
		t.Errorf("route was not repointed: %v", route)
	}
	models, _ := route["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("model count = %d, want 1: %v", len(models), route["models"])
	}
	if entry, _ := models[0].(map[string]any); entry["id"] != "fresh-model" {
		t.Errorf("stale model survived: %v", models[0])
	}
}

// A malformed settings document must fail loudly rather than being replaced: it
// is the user's file, and dsh would have kept serving from its last good copy.
func TestWriteDSHRefusesAnInvalidSettingsDocument(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("llm-pi-ai: [not, a, mapping]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example/openai", "sk-x", "m1"); err == nil {
		t.Fatal("writing over a non-mapping llm-pi-ai section succeeded")
	}
}

// WriteDSHOfficial activates DeepSeek's own service through the deepseek-official
// route dsh ships, storing the key in DEEPSEEK_API_KEY instead of declaring a
// custom bootagent route the way WriteDSH does. ReadDSHConfig must recognize
// the selection and report it without a baseURL -- the shipped route's endpoint
// is dsh's own fact, not something the settings document records.
func TestWriteDSHOfficialUsesTheShippedRouteInsteadOfDeclaringOne(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	credentials := filepath.Join(home, ".dsh", ".credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "ui-onboarding:\n" +
		"  welcomeNoticeVersion: 2026-08-13.1\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testWriter(t, home, "linux").WriteDSHOfficial(context.Background(), path, "sk-deepseek-key", "deepseek-v4-pro", ""); err != nil {
		t.Fatal(err)
	}

	// No bootagent route declared: the shipped route already exists and carries
	// DeepSeek's endpoint and model catalog.
	routes := dshSettings(t, path)
	if _, exists := routes["bootagent"]; exists {
		t.Errorf("a bootagent route was declared even though the shipped route was used: %v", routes)
	}

	// The default selection points at the shipped route instead.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var selection struct {
		Default struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
		} `yaml:"agent-default-model"`
	}
	if err := yaml.Unmarshal(data, &selection); err != nil {
		t.Fatal(err)
	}
	if selection.Default.Provider != "deepseek-official" || selection.Default.Model != "deepseek-v4-pro" {
		t.Errorf("default selection = %+v, want deepseek-official route", selection.Default)
	}

	// The key lands in DEEPSEEK_API_KEY, the credential the shipped route reads,
	// not BOOTAGENT_API_KEY.
	stored := dshCredentials(t, credentials)
	if stored["DEEPSEEK_API_KEY"] != "sk-deepseek-key" {
		t.Errorf("DEEPSEEK_API_KEY = %q, want sk-deepseek-key", stored["DEEPSEEK_API_KEY"])
	}
	if _, present := stored["BOOTAGENT_API_KEY"]; present {
		t.Errorf("BOOTAGENT_API_KEY was written even though the shipped route was used")
	}

	// Unrelated settings survive.
	if !strings.Contains(string(data), "ui-onboarding") {
		t.Errorf("unrelated settings section was lost:\n%s", data)
	}

	detected := ReadDSHConfig(string(data))
	if detected.BaseURL != "" || detected.Model != "deepseek-v4-pro" || detected.ManagedByBootAgent {
		t.Errorf("dsh round-trip = %#v, want empty baseURL for shipped route", detected)
	}

	for _, target := range []string{path, credentials} {
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, err=%v", target, info.Mode().Perm(), err)
		}
	}
}

// An activation against a different Provider after one against DeepSeek's own
// service must clean up: the bootagent route has to be declared again, and any
// leftover deepseek-official selection would keep dsh serving the wrong endpoint.
func TestWriteDSHCleansUpAfterWriteDSHOfficial(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	credentials := filepath.Join(home, ".dsh", ".credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// First activation: DeepSeek official.
	if err := testWriter(t, home, "linux").WriteDSHOfficial(context.Background(), path, "sk-deepseek", "deepseek-v4-pro", ""); err != nil {
		t.Fatal(err)
	}
	// Second activation: a gateway.
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example/openai", "sk-gateway", "deepseek-v4-pro"); err != nil {
		t.Fatal(err)
	}

	routes := dshSettings(t, path)
	if _, exists := routes["bootagent"]; !exists {
		t.Errorf("no bootagent route was declared for the gateway: %v", routes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var selection struct {
		Default struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
		} `yaml:"agent-default-model"`
	}
	if err := yaml.Unmarshal(data, &selection); err != nil {
		t.Fatal(err)
	}
	if selection.Default.Provider != "bootagent" {
		t.Errorf("default selection = %+v, want bootagent", selection.Default)
	}

	stored := dshCredentials(t, credentials)
	if stored["BOOTAGENT_API_KEY"] != "sk-gateway" {
		t.Errorf("BOOTAGENT_API_KEY = %q, want sk-gateway", stored["BOOTAGENT_API_KEY"])
	}
	// The DEEPSEEK_API_KEY from the first activation stays: it is unreferenced
	// and harmless once the selection points elsewhere.
	if stored["DEEPSEEK_API_KEY"] != "sk-deepseek" {
		t.Errorf("DEEPSEEK_API_KEY was disturbed: %q", stored["DEEPSEEK_API_KEY"])
	}
}

// A second WriteDSHOfficial call after a WriteDSH must remove the bootagent
// route so ReadDSHConfig reports the official selection, not a stale custom one.
func TestWriteDSHOfficialCleansUpAStaleBootagentRoute(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// First: gateway route.
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example/openai", "sk-gateway", "deepseek-v4-pro"); err != nil {
		t.Fatal(err)
	}
	// Second: DeepSeek official.
	if err := testWriter(t, home, "linux").WriteDSHOfficial(context.Background(), path, "sk-deepseek", "deepseek-v4-flash", ""); err != nil {
		t.Fatal(err)
	}

	routes := dshSettings(t, path)
	if _, exists := routes["bootagent"]; exists {
		t.Errorf("stale bootagent route survived: %v", routes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var selection struct {
		Default struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
		} `yaml:"agent-default-model"`
	}
	if err := yaml.Unmarshal(data, &selection); err != nil {
		t.Fatal(err)
	}
	if selection.Default.Provider != "deepseek-official" || selection.Default.Model != "deepseek-v4-flash" {
		t.Errorf("default selection = %+v, want deepseek-official", selection.Default)
	}

	detected := ReadDSHConfig(string(data))
	if detected.BaseURL != "" || detected.Model != "deepseek-v4-flash" {
		t.Errorf("ReadDSHConfig = %#v, want model without baseURL", detected)
	}
}

// The selection carries the Profile's thinking depth when one is set, and a
// rewrite without one must drop a previously-written depth -- dsh treats a
// saved selection as complete, so a leftover effort would keep applying to a
// model the user may have changed it away from.
func TestWriteDSHOfficialCarriesReasoningEffort(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	readSelection := func() map[string]string {
		t.Helper()
		data, err := os.ReadFile(path)
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

	if err := writer.WriteDSHOfficial(context.Background(), path, "sk-x", "deepseek-v4-pro", "max"); err != nil {
		t.Fatal(err)
	}
	selection := readSelection()
	if selection["reasoningEffort"] != "max" || selection["provider"] != "deepseek-official" {
		t.Errorf("selection = %v, want reasoningEffort max on the official route", selection)
	}

	// A rewrite without an effort removes the one before it.
	if err := writer.WriteDSHOfficial(context.Background(), path, "sk-x", "deepseek-v4-pro", ""); err != nil {
		t.Fatal(err)
	}
	if selection := readSelection(); selection["reasoningEffort"] != "" {
		t.Errorf("stale reasoningEffort survived: %v", selection)
	}
}

// llm-deepseek dispatches only off, high, and max; anything else -- including
// levels llm-pi-ai would accept, like "medium" -- fails on the user's first
// request, so the write refuses it up front and leaves the settings untouched.
func TestWriteDSHOfficialRejectsAnUnsupportedReasoningEffort(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".dsh", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteDSHOfficial(context.Background(), path, "sk-x", "deepseek-v4-pro", "medium"); err == nil {
		t.Fatal("an effort the shipped route cannot dispatch was accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a rejected effort still wrote settings: err=%v", err)
	}
	for _, valid := range []string{"off", "high", "max"} {
		if err := ValidateDSHOfficialReasoningEffort(valid); err != nil {
			t.Errorf("valid effort %q rejected: %v", valid, err)
		}
	}
}
