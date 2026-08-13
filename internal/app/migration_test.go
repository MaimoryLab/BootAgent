package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateLegacyHomeCopiesStateAndRetainsLegacy(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".oneagent")
	if err := os.MkdirAll(filepath.Join(legacy, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"mcp.json":                   `{"schema_version":1}`,
		"providers.json":             `{"schema_version":1}`,
		"profile.json":               `{"schema_version":2}`,
		"profiles/team.json":         `{"id":"team"}`,
		"agents/codex.json":          `{"agent_id":"codex"}`,
		"runtimes/node/bin/node":     "must not copy",
		"settings.json":              `{"backup_retention":7}`,
		"env/team.env":               "export OPENAI_API_KEY=secret\n",
		"backup/keep.json":           "backup",
		"skill-backups/s1/meta.json": "backup",
		"logs/install.log":           "log",
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
	if notice, err := migrateLegacyHome(home, "linux"); err != nil || !strings.Contains(notice, "Node.js") {
		t.Fatalf("migration = %q, %v", notice, err)
	}
	entries, err := filepath.Glob(filepath.Join(home, ".oneagent-migrated-*"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("retained legacy directory = %v, %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(entries[0], "runtimes", "node", "bin", "node")); err != nil {
		t.Fatalf("retained runtime = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bootagent", "agents", "codex.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bootagent", "runtimes")); !os.IsNotExist(err) {
		t.Fatalf("runtime was copied: %v", err)
	}
	for _, name := range []string{"settings.json", "env/team.env", "backup/keep.json", "skill-backups/s1/meta.json", "logs/install.log"} {
		if _, err := os.Stat(filepath.Join(home, ".bootagent", name)); err != nil {
			t.Fatalf("migrated state %s is missing: %v", name, err)
		}
	}
	data, err := os.ReadFile(codex)
	if err != nil || strings.Contains(string(data), "oneagent") || !strings.Contains(string(data), "bootagent") {
		t.Fatalf("Codex migration = %q, %v", data, err)
	}
}

func TestMigrateLegacyHomeDoesNotOverwriteBootAgentState(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".oneagent")
	current := filepath.Join(home, ".bootagent")
	for dir, content := range map[string]string{
		filepath.Join(legacy, "settings.json"):  `{"source":"legacy"}`,
		filepath.Join(current, "settings.json"): `{"source":"current"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := migrateLegacyHome(home, "linux"); err == nil {
		t.Fatal("migration unexpectedly overwrote existing BootAgent state")
	}
	data, err := os.ReadFile(filepath.Join(current, "settings.json"))
	if err != nil || string(data) != `{"source":"current"}` {
		t.Fatalf("BootAgent state = %q, %v", data, err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy directory was not retained after conflict: %v", err)
	}
}
