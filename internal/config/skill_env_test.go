package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestSkillEnvName(t *testing.T) {
	for providerID, want := range map[string]string{
		"novita":     "BOOTAGENT_NOVITA_API_KEY",
		"my-gateway": "BOOTAGENT_MY_GATEWAY_API_KEY",
		" ppio ":     "BOOTAGENT_PPIO_API_KEY",
	} {
		if got := SkillEnvName(providerID); got != want || !IsSkillEnvName(got) {
			t.Errorf("SkillEnvName(%q) = %q, want %q", providerID, got, want)
		}
	}
	for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "BOOTAGENT_KEY", "bootagent_x_api_key", "MY_BOOTAGENT_X_API_KEY"} {
		if IsSkillEnvName(name) {
			t.Errorf("%s should not be a managed name", name)
		}
	}
}

func readSkillJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func assertPrivate(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode = %v, want 0600", path, info.Mode().Perm())
	}
}

func TestWriteClaudeSkillEnvManagesOnlyItsOwnNames(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := `{"model":"keep","env":{"ANTHROPIC_AUTH_TOKEN":"sk-provider","MY_VAR":"mine","BOOTAGENT_STALE_API_KEY":"old"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	ctx := context.Background()
	if err := writer.WriteClaudeSkillEnv(ctx, path, map[string]string{"BOOTAGENT_NOVITA_API_KEY": "sk-novita"}); err != nil {
		t.Fatal(err)
	}
	document := readSkillJSON(t, path)
	env := document["env"].(map[string]any)
	if env["BOOTAGENT_NOVITA_API_KEY"] != "sk-novita" || env["MY_VAR"] != "mine" || env["ANTHROPIC_AUTH_TOKEN"] != "sk-provider" || document["model"] != "keep" {
		t.Fatalf("settings = %#v", document)
	}
	if _, stale := env["BOOTAGENT_STALE_API_KEY"]; stale {
		t.Fatal("a managed variable no Skill needs was kept")
	}
	assertPrivate(t, path)

	// An unchanged set writes nothing, so no backup piles up.
	before, _ := os.Stat(path)
	if err := writer.WriteClaudeSkillEnv(ctx, path, map[string]string{"BOOTAGENT_NOVITA_API_KEY": "sk-novita"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("an unchanged set rewrote the file")
	}

	if err := writer.WriteClaudeSkillEnv(ctx, path, nil); err != nil {
		t.Fatal(err)
	}
	env = readSkillJSON(t, path)["env"].(map[string]any)
	if len(env) != 2 || env["MY_VAR"] != "mine" {
		t.Fatalf("clearing removed the user's variables: %#v", env)
	}
}

func TestWriteClaudeSkillEnvCreatesNothingWithNothingToWrite(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	writer := testWriter(t, home, "linux")
	if err := writer.WriteClaudeSkillEnv(context.Background(), path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("an empty set created Claude's config directory")
	}
	if err := writer.WriteClaudeSkillEnv(context.Background(), path, map[string]string{"NOT_OURS": "x"}); err == nil {
		t.Fatal("a non-BootAgent name was written")
	}
	if err := writer.WriteClaudeSkillEnv(context.Background(), path, map[string]string{"BOOTAGENT_X_API_KEY": "a\nb"}); err == nil {
		t.Fatal("a value with a line break was written")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"env":"not an object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteClaudeSkillEnv(context.Background(), path, map[string]string{"BOOTAGENT_X_API_KEY": "k"}); err == nil {
		t.Fatal("a non-object env block was overwritten")
	}
}

func decodeTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := toml.Unmarshal(data, &value); err != nil {
		t.Fatalf("written TOML is invalid: %v\n%s", err, data)
	}
	return value
}

func TestWriteCodexSkillEnvMergesWithTheUsersPolicy(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := strings.Join([]string{
		`model_provider = "bootagent"`,
		`model = "m"`,
		``,
		`[model_providers.bootagent]`,
		`name = "PPIO"`,
		`base_url = "https://api.ppio.com/openai"`,
		``,
		`[shell_environment_policy]`,
		`inherit = "core"`,
		``,
		`[shell_environment_policy.set]`,
		`MY_VAR = "mine"`,
		`BOOTAGENT_STALE_API_KEY = "old"`,
		``,
		`[mcp_servers.docs]`,
		`command = "docs"`,
		``,
	}, "\n")
	// World-readable on purpose: the first write that adds a key must tighten
	// both the file and the backup of its old content.
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteCodexSkillEnv(context.Background(), path, map[string]string{"BOOTAGENT_NOVITA_API_KEY": "sk-novita"}); err != nil {
		t.Fatal(err)
	}
	parsed := decodeTOML(t, path)
	policy := parsed["shell_environment_policy"].(map[string]any)
	set := policy["set"].(map[string]any)
	if policy["inherit"] != "core" || set["MY_VAR"] != "mine" || set["BOOTAGENT_NOVITA_API_KEY"] != "sk-novita" {
		t.Fatalf("policy = %#v", policy)
	}
	if _, stale := set["BOOTAGENT_STALE_API_KEY"]; stale {
		t.Fatal("a managed variable no Skill needs was kept")
	}
	if parsed["model"] != "m" || parsed["mcp_servers"].(map[string]any)["docs"] == nil || parsed["model_providers"].(map[string]any)["bootagent"] == nil {
		t.Fatalf("unrelated configuration lost: %#v", parsed)
	}
	assertPrivate(t, path)
	backups, _ := filepath.Glob(path + ".backup-*")
	if len(backups) == 0 {
		t.Fatal("no backup was taken")
	}
	for _, backup := range backups {
		assertPrivate(t, backup)
	}
	if !CodexConfigHoldsSkillKey(readTextFile(t, path)) {
		t.Fatal("written config not recognised as holding a key")
	}

	// Removing the last managed variable keeps the user's own.
	if err := writer.WriteCodexSkillEnv(context.Background(), path, nil); err != nil {
		t.Fatal(err)
	}
	set = decodeTOML(t, path)["shell_environment_policy"].(map[string]any)["set"].(map[string]any)
	if len(set) != 1 || set["MY_VAR"] != "mine" {
		t.Fatalf("set after clearing = %#v", set)
	}
	if CodexConfigHoldsSkillKey(readTextFile(t, path)) {
		t.Fatal("cleared config still reported as holding a key")
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWriteCodexSkillEnvDropsAnEmptySetAndCreatesNothingNeedlessly(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	writer := testWriter(t, home, "linux")
	ctx := context.Background()
	if err := writer.WriteCodexSkillEnv(ctx, path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("an empty set created config.toml")
	}
	if err := writer.WriteCodexSkillEnv(ctx, path, map[string]string{"BOOTAGENT_PPIO_API_KEY": `sk-"quoted"\`}); err != nil {
		t.Fatal(err)
	}
	set := decodeTOML(t, path)["shell_environment_policy"].(map[string]any)["set"].(map[string]any)
	if set["BOOTAGENT_PPIO_API_KEY"] != `sk-"quoted"\` {
		t.Fatalf("quoted value = %#v", set)
	}
	if err := writer.WriteCodexSkillEnv(ctx, path, nil); err != nil {
		t.Fatal(err)
	}
	if text := readTextFile(t, path); strings.Contains(text, "shell_environment_policy") {
		t.Fatalf("an empty set left its table behind: %q", text)
	}
}

func TestWriteCodexSkillEnvRefusesSpellingsItCannotMerge(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	for _, invalid := range []string{
		"[shell_environment_policy]\nset = { MY_VAR = \"x\" }\n",
		"shell_environment_policy.set.MY_VAR = \"x\"\n",
		"[shell_environment_policy.set]\nNUMBER = 3\n",
		"[shell_environment_policy]\nset = \"nope\"\n",
		"model = \"unterminated\n",
	} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteCodexSkillEnv(context.Background(), path, map[string]string{"BOOTAGENT_X_API_KEY": "k"}); err == nil {
			t.Errorf("merged an unsupported config: %q", invalid)
		}
		if got := readTextFile(t, path); got != invalid {
			t.Errorf("unsupported config was modified: %q", got)
		}
	}
}

// WriteCodex rewrites config.toml for a Profile. Once a Skill key lives there,
// that write must keep it and keep the file secret.
func TestWriteCodexKeepsSkillEnvAndStaysSecret(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	writer := testWriter(t, home, "linux")
	ctx := context.Background()
	if err := writer.WriteCodexSkillEnv(ctx, path, map[string]string{"BOOTAGENT_NOVITA_API_KEY": "sk-novita"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteCodex(ctx, path, "PPIO", "https://api.ppio.com/openai", "sk-codex", "m", ""); err != nil {
		t.Fatal(err)
	}
	set := decodeTOML(t, path)["shell_environment_policy"].(map[string]any)["set"].(map[string]any)
	if set["BOOTAGENT_NOVITA_API_KEY"] != "sk-novita" {
		t.Fatalf("Profile write dropped the Skill key: %#v", set)
	}
	assertPrivate(t, path)
	backups, _ := filepath.Glob(path + ".backup-*")
	for _, backup := range backups {
		assertPrivate(t, backup)
	}
}

func TestSkillEnvFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	path := SkillEnvFilePath(home, "my-gateway")
	writer := testWriter(t, home, "linux")
	if err := writer.WriteSkillEnvFile(context.Background(), path, "BOOTAGENT_MY_GATEWAY_API_KEY", "sk-a'b"); err != nil {
		t.Fatal(err)
	}
	if got := readTextFile(t, path); got != "BOOTAGENT_MY_GATEWAY_API_KEY='sk-a\\'b'\n" {
		t.Fatalf("env file = %q", got)
	}
	assertPrivate(t, path)
	if err := RemoveSkillEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSkillEnvFile(path); err != nil {
		t.Fatal("removing a missing file failed")
	}
	if matches, _ := filepath.Glob(path + "*"); len(matches) != 0 {
		t.Fatalf("removal left copies of the key: %v", matches)
	}
}

// An unrelated problem in an Agent's config must not fail a Skill change that
// has nothing to write there.
func TestSkillEnvWritersIgnoreFilesWithNothingOfTheirs(t *testing.T) {
	home := t.TempDir()
	writer := testWriter(t, home, "linux")
	claude := filepath.Join(home, ".claude", "settings.json")
	codex := filepath.Join(home, ".codex", "config.toml")
	for _, path := range []string{claude, codex} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{ not valid"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.WriteClaudeSkillEnv(context.Background(), claude, nil); err != nil {
		t.Fatalf("Claude: %v", err)
	}
	if err := writer.WriteCodexSkillEnv(context.Background(), codex, nil); err != nil {
		t.Fatalf("Codex: %v", err)
	}
	if err := writer.WriteClaudeSkillEnv(context.Background(), claude, map[string]string{"BOOTAGENT_X_API_KEY": "k"}); err == nil {
		t.Fatal("a real write to an invalid file succeeded")
	}
}
