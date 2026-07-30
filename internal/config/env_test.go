package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

func TestWriteAgentEnvUsesDeclaredAndCompatibilityVariables(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".oneagent", "agents", "claude-code.env")
	filesystem := securefs.New(securefs.Options{OS: "linux"})
	if err := WriteAgentEnv(context.Background(), filesystem, path, "linux", "claude-code", "key'quoted", "https://api.example", "model-a", "model-fast", map[string]string{
		"api_key": "ANTHROPIC_AUTH_TOKEN", "base_url": "ANTHROPIC_BASE_URL", "model": "ANTHROPIC_MODEL", "small_fast_model": "ANTHROPIC_SMALL_FAST_MODEL",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{
		"export ONEAGENT_API_KEY_CLAUDE_CODE='key'\\''quoted'",
		"export ONEAGENT_API_BASE_URL_CLAUDE_CODE=https://api.example",
		"export ONEAGENT_API_KEY='key'\\''quoted'",
		"export ANTHROPIC_MODEL=model-a",
		"export ANTHROPIC_SMALL_FAST_MODEL=model-fast",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("env missing %q: %s", expected, text)
		}
	}
	if strings.Contains(text, "api_key") {
		t.Fatal("env variable names unexpectedly contain internal field names")
	}
}

func TestWriteAgentEnvWindowsAndSharedCompatibility(t *testing.T) {
	home := t.TempDir()
	filesystem := securefs.New(securefs.Options{OS: "windows", Username: "tester", Run: func(context.Context, []string) error { return nil }})
	path := filepath.Join(home, ".oneagent", "env.ps1")
	if err := WriteSharedEnv(context.Background(), filesystem, path, "windows", "key'quoted", "https://api.example"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "$env:ONEAGENT_API_KEY = 'key''quoted'") || !strings.Contains(string(data), "$env:ONEAGENT_API_BASE_URL = 'https://api.example'") {
		t.Fatalf("PowerShell env = %q", data)
	}
}
