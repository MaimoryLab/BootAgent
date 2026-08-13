package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCodexFollowsSelectedProviderAndMarker(t *testing.T) {
	detected := ReadCodexConfig("model_provider = \"vendor\"\nmodel = \"gpt-5-mini\"\n[model_providers.vendor]\nbase_url = \"https://vendor.example/v1\"\n[model_providers.bootagent]\nbase_url = \"https://ours.example/v1\"\napi_key = \"sk-hidden\"\n")
	if detected.BaseURL != "https://vendor.example/v1" || detected.Model != "gpt-5-mini" || !detected.ManagedByBootAgent || detected.Unreadable != nil {
		t.Fatalf("codex detection = %#v", detected)
	}
	if strings.Contains(mustJSON(t, detected), "sk-hidden") || strings.Contains(mustJSON(t, detected), "api_key") {
		t.Fatalf("codex detection leaked secret fields: %#v", detected)
	}
}

func TestReadClaudeRequiresAllDeclaredVariablesWithoutReturningKey(t *testing.T) {
	declared := map[string]string{
		"api_key":          "ANTHROPIC_AUTH_TOKEN",
		"base_url":         "ANTHROPIC_BASE_URL",
		"model":            "ANTHROPIC_MODEL",
		"small_fast_model": "ANTHROPIC_SMALL_FAST_MODEL",
	}
	partial := ReadClaudeConfig(`{"env":{"ANTHROPIC_BASE_URL":"https://x.example"}}`, declared)
	if partial.BaseURL != "https://x.example" || partial.ManagedByBootAgent {
		t.Fatalf("partial Claude detection = %#v", partial)
	}
	full := ReadClaudeConfig(`{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-secret","ANTHROPIC_BASE_URL":"https://x.example","ANTHROPIC_MODEL":"m","ANTHROPIC_SMALL_FAST_MODEL":"fast"}}`, declared)
	if !full.ManagedByBootAgent || full.Model != "m" || strings.Contains(mustJSON(t, full), "sk-secret") {
		t.Fatalf("full Claude detection = %#v", full)
	}
}

func TestReadOpenAICompatibleAndJSONC(t *testing.T) {
	detected := ReadOpenAICompatibleConfig(`{"provider":{"mine":{"options":{"baseURL":"https://mine.example/v1","apiKey":"sk-secret"}}},"model":"mine/local-llm"}`)
	if detected.BaseURL != "https://mine.example/v1" || detected.Model != "local-llm" {
		t.Fatalf("OpenAI-compatible detection = %#v", detected)
	}
	if detected := ReadOpenAICompatibleConfig("{\n // comment\n \"model\":\"x/y\"\n}"); detected.Unreadable == nil || !strings.Contains(*detected.Unreadable, "JSONC") {
		t.Fatalf("JSONC detection = %#v", detected)
	}
	if detected := ReadOpenAICompatibleConfig(`{"model":"bare-model"}`); detected.Model != "bare-model" {
		t.Fatalf("bare model detection = %#v", detected)
	}
}

func TestReadAiderNeverExecutesOrReturnsKey(t *testing.T) {
	detected := ReadAiderConfig("export OPENAI_API_BASE='https://hand.example/v1'\nexport OPENAI_API_KEY='sk-never-return'\n")
	if detected.BaseURL != "https://hand.example/v1" || detected.ManagedByBootAgent || strings.Contains(mustJSON(t, detected), "sk-never-return") {
		t.Fatalf("Aider detection = %#v", detected)
	}
	if got := ReadAiderConfig("$env:OPENAI_API_BASE = \"https://win.example/v1\"\n"); got.BaseURL != "https://win.example/v1" {
		t.Fatalf("PowerShell detection = %#v", got)
	}
}

func TestReadersReturnLocalDiagnosticsForMalformedOrWrongTypes(t *testing.T) {
	for _, detected := range []Detected{
		ReadCodexConfig("model_provider = \nbroken ["),
		ReadClaudeConfig("[]", nil),
		ReadOpenAICompatibleConfig("[]"),
		ReadOpenAICompatibleConfig("{ not json"),
	} {
		if detected.Unreadable == nil || detected.BaseURL != "" || detected.Model != "" {
			t.Fatalf("malformed detection = %#v", detected)
		}
	}
	codex := ReadCodexConfig("model_provider = 1\nmodel = 2\n[model_providers.p]\nbase_url = 3\n")
	if codex.BaseURL != "" || codex.Model != "" {
		t.Fatalf("wrongly typed TOML values = %#v", codex)
	}
}

func TestDetectFileDistinguishesAbsentEmptyUnknownAndSecrets(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	if got := DetectFile(path, "codex", nil); got != nil {
		t.Fatalf("absent detection = %#v", got)
	}
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DetectFile(path, "codex", nil); got == nil || got.Unreadable == nil || !strings.Contains(*got.Unreadable, "空") {
		t.Fatalf("empty detection = %#v", got)
	}
	if err := os.WriteFile(path, []byte("model_provider = \"p\"\nmodel = \"m\"\n[model_providers.p]\nbase_url = \"https://example\"\napi_key = \"sk-file-secret\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := DetectFile(path, "codex", nil)
	if got == nil || got.BaseURL != "https://example" || strings.Contains(mustJSON(t, got), "sk-file-secret") {
		t.Fatalf("file detection = %#v", got)
	}
	if got := DetectFile(path, "future-adapter", nil); got == nil || got.Unreadable == nil {
		t.Fatalf("unknown adapter detection = %#v", got)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
