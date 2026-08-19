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

// A UTF-8 BOM is what Windows Notepad writes by default, and it is invisible in
// every editor that does. Both parsers reject it outright -- hujson with
// `invalid character '\ufeff' at start of value`, go-toml with `invalid
// character at start of key` -- so a user whose only mistake was opening a
// config in Notepad saw the whole migration fail.
func TestMigrateAcceptsConfigsSavedWithABOM(t *testing.T) {
	home := t.TempDir()
	files := map[string]string{
		".codex/config.toml":            "\ufeff[model_providers.oneagent]\nbase_url = \"https://x\"\n",
		".config/opencode/opencode.json": "\ufeff" + `{"provider":{"oneagent":{"x":1}},"model":"oneagent/m"}`,
		".openclaw/openclaw.json":        "\ufeff" + `{"models":{"providers":{"oneagent":{"y":2}}}}`,
		".kimi-code/config.toml":         "\ufeff[providers.oneagent]\nbase_url = \"https://x\"\n",
		".zcode/v2/config.json":          "\ufeff" + `{"provider":{"old":{"name":"OneAgent - PPIO"}}}`,
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
		t.Fatalf("migration of BOM-prefixed configs failed: %v", err)
	}
	for name := range files {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(data)), "oneagent") {
			t.Errorf("%s still contains oneagent: %s", name, data)
		}
		// Dropped rather than preserved: JSON and TOML both define the mark as
		// invalid content, so writing it back would produce a file neither parser
		// will read on the next run.
		if strings.HasPrefix(string(data), "\ufeff") {
			t.Errorf("%s was rewritten with the BOM still on it", name)
		}
	}
}

// The six configs belong to different Agents and nothing links them. One that
// cannot be parsed used to abandon every migration listed after it, and the
// reported error named only the first file -- so a single hand-edited config
// silently left later ones on the old provider name.
func TestMigrateContinuesPastAFileItCannotParse(t *testing.T) {
	home := t.TempDir()
	// openclaw is listed before zcode, so a broken openclaw is what used to stop
	// zcode from being migrated at all.
	broken := filepath.Join(home, ".openclaw", "openclaw.json")
	zcode := filepath.Join(home, ".zcode", "v2", "config.json")
	for path, content := range map[string]string{
		broken: `{"models":{"providers":{"oneagent":`,
		zcode:  `{"provider":{"old":{"name":"OneAgent - PPIO"}}}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fs := securefs.New(securefs.Options{OS: "linux", BackupRoot: filepath.Join(home, ".bootagent", "backup")})
	err := MigrateLegacyAgentConfigs(context.Background(), home, fs)
	if err == nil {
		t.Fatal("expected the unparsable config to be reported")
	}
	if !strings.Contains(err.Error(), ".openclaw") {
		t.Errorf("error does not name the file that failed: %v", err)
	}
	data, readErr := os.ReadFile(zcode)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(strings.ToLower(string(data)), "oneagent") {
		t.Errorf("zcode was skipped because an earlier file failed: %s", data)
	}
}
