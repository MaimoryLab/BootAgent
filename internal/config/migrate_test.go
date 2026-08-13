package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

func TestMigrateLegacyAgentConfigs(t *testing.T) {
	home := t.TempDir()
	files := map[string]string{
		".codex/config.toml": `model_provider = "oneagent"
model = "oneagent/m"
[model_providers.oneagent]
base_url = "https://example.test"`,
		".config/opencode/opencode.json": `{"provider":{"oneagent":{"options":{}}},"model":"oneagent/m"}`,
		".config/kilo/kilo.jsonc":        `{"provider":{"oneagent":{"options":{}}},"model":"oneagent/m"}`,
		".openclaw/openclaw.json":        `{"models":{"providers":{"oneagent":{}}},"agents":{"defaults":{"model":{"primary":"oneagent/m"}}}}`,
		".kimi-code/config.toml": `default_model = "oneagent/m"
[providers."oneagent"]
base_url = "https://example.test"
[models."oneagent/m"]
provider = "oneagent"
model = "m"`,
		".zcode/v2/config.json": `{"provider":{"old":{"name":"OneAgent - PPIO","kind":"openai"}}}`,
	}
	for name, content := range files {
		path := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fs := securefs.New(securefs.Options{OS: "linux", BackupRoot: filepath.Join(home, ".bootagent", "backup")})
	if err := MigrateLegacyAgentConfigs(context.Background(), home, fs); err != nil {
		t.Fatal(err)
	}
	for name := range files {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "oneagent") {
			t.Errorf("%s still contains oneagent: %s", name, data)
		}
	}
	var zcode map[string]any
	data, _ := os.ReadFile(filepath.Join(home, ".zcode/v2/config.json"))
	if err := json.Unmarshal(data, &zcode); err != nil {
		t.Fatal(err)
	}
	providers := zcode["provider"].(map[string]any)
	for key, value := range providers {
		if !strings.HasPrefix(key, "bootagent-") || !strings.HasPrefix(value.(map[string]any)["name"].(string), "BootAgent - ") {
			t.Fatalf("ZCode provider was not migrated: %#v", providers)
		}
	}
}
