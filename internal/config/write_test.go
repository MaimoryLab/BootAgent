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

func TestWriteQwenUsesItsOwnEndpointVariable(t *testing.T) {
	home := t.TempDir()
	writer := testWriter(t, home, "linux")
	path := filepath.Join(home, ".qwen", ".env")
	if err := writer.WriteQwen(context.Background(), path, "https://api.example/openai", "key'quoted", "qwen3-coder-plus"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	// Qwen Code reads OPENAI_BASE_URL. Writing Aider's OPENAI_API_BASE instead
	// would leave the CLI on its default endpoint holding a key that does not
	// belong to it, which surfaces as an auth error rather than a missing config.
	if !strings.Contains(text, "OPENAI_BASE_URL=https://api.example/openai/v1") {
		t.Errorf("endpoint variable is wrong or absent: %q", text)
	}
	if strings.Contains(text, "OPENAI_API_BASE=") {
		t.Errorf("wrote Aider's variable name: %q", text)
	}
	if !strings.Contains(text, `'key\'quoted'`) {
		t.Errorf("key was not quoted: %q", text)
	}
	// Without the model Qwen Code keeps its built-in default and ignores the one
	// the user picked.
	if !strings.Contains(text, "OPENAI_MODEL=qwen3-coder-plus") {
		t.Errorf("model is absent: %q", text)
	}

	detected := ReadQwenConfig(text)
	if detected.BaseURL != "https://api.example/openai/v1" || detected.Model != "qwen3-coder-plus" {
		t.Errorf("round-trip through the reader = %#v", detected)
	}
}

func TestWriteQwenOmitsAnEmptyModel(t *testing.T) {
	// An empty OPENAI_MODEL= line would override the CLI's own default with
	// nothing, which is worse than leaving the variable out.
	home := t.TempDir()
	path := filepath.Join(home, ".qwen", ".env")
	if err := testWriter(t, home, "linux").WriteQwen(context.Background(), path, "https://api.example/openai", "k", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "OPENAI_MODEL") {
		t.Errorf("empty model reached the file: %q", data)
	}
}
