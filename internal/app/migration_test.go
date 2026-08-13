package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateLegacyHomeCopiesConfigurationOnlyAndRenamesCodexProvider(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".oneagent")
	if err := os.MkdirAll(filepath.Join(legacy, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"mcp.json":              `{"schema_version":1}`,
		"providers.json":        `{"schema_version":1}`,
		"profile.json":          `{"schema_version":2}`,
		"profiles/team.json":    `{"id":"team"}`,
		"agents/codex.json":     `{"agent_id":"codex"}`,
		"runtimes/node/bin/node": "must not copy",
	} {
		path := filepath.Join(legacy, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	codex := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codex), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("model_provider = \"oneagent\"\n[model_providers.oneagent]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if notice, err := migrateLegacyHome(home); err != nil || !strings.Contains(notice, "Node.js") {
		t.Fatalf("migration = %q, %v", notice, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bootagent", "agents", "codex.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bootagent", "runtimes")); !os.IsNotExist(err) {
		t.Fatalf("runtime was copied: %v", err)
	}
	data, err := os.ReadFile(codex)
	if err != nil || strings.Contains(string(data), "oneagent") || !strings.Contains(string(data), "bootagent") {
		t.Fatalf("Codex migration = %q, %v", data, err)
	}
}
