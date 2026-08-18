package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

func TestWriteClaudeDesktopPreservesOtherConfiguration(t *testing.T) {
	home := t.TempDir()
	paths, err := claudeDesktopConfigPaths(home, "macos")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.meta), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.normal), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.normal, []byte(`{"keep":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.meta, []byte(`{"entries":[{"id":"other","name":"Other"}],"keep":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	profilePath, err := testWriter(t, home, "macos").WriteClaudeDesktop(context.Background(), "https://anthropic.example", "secret", "pa/claude-opus-5-ppinfra", true)
	if err != nil {
		t.Fatal(err)
	}
	if profilePath != paths.profile {
		t.Fatalf("profile path = %q", profilePath)
	}
	var profile map[string]any
	readJSONFile(t, paths.profile, &profile)
	models := profile["inferenceModels"].([]any)
	if profile["inferenceGatewayBaseUrl"] != "https://anthropic.example" || models[0].(map[string]any)["name"] != "pa/claude-opus-5-ppinfra" || models[0].(map[string]any)["supports1m"] != true {
		t.Fatalf("profile = %#v", profile)
	}
	var normal map[string]any
	readJSONFile(t, paths.normal, &normal)
	if normal["keep"] != true || normal["deploymentMode"] != "3p" {
		t.Fatalf("normal config = %#v", normal)
	}
	var meta map[string]any
	readJSONFile(t, paths.meta, &meta)
	if meta["keep"] != float64(1) || len(meta["entries"].([]any)) != 2 || meta["appliedId"] != claudeDesktopProfileID {
		t.Fatalf("meta = %#v", meta)
	}
	info, err := os.Stat(paths.profile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode = %v", info.Mode().Perm())
	}
}

func TestWriteClaudeDesktopRejectsInvalidInputsWithoutChangingFiles(t *testing.T) {
	home := t.TempDir()
	paths, _ := claudeDesktopConfigPaths(home, "windows")
	if err := os.MkdirAll(filepath.Dir(paths.normal), 0o700); err != nil {
		t.Fatal(err)
	}
	const invalid = `[]`
	if err := os.WriteFile(paths.normal, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "windows")
	if _, err := writer.WriteClaudeDesktop(context.Background(), "https://example.test", "secret", "deepseek-v4", false); err == nil {
		t.Fatal("non-Claude model unexpectedly succeeded")
	}
	if _, err := writer.WriteClaudeDesktop(context.Background(), "https://example.test", "secret", "claude-opus-5", false); err == nil {
		t.Fatal("non-object config unexpectedly succeeded")
	}
	data, err := os.ReadFile(paths.normal)
	if err != nil || string(data) != invalid {
		t.Fatalf("invalid config changed to %q, err=%v", data, err)
	}
}

func TestWriteClaudeDesktopRollsBackPartialWrite(t *testing.T) {
	home := t.TempDir()
	paths, _ := claudeDesktopConfigPaths(home, "macos")
	secureCalls := 0
	filesystem := securefs.New(securefs.Options{OS: "macos", Secure: func(string, bool) error {
		secureCalls++
		if secureCalls == 6 {
			return os.ErrPermission
		}
		return nil
	}})
	writer := NewWriter(home, "macos", filesystem)
	if _, err := writer.WriteClaudeDesktop(context.Background(), "https://example.test", "secret", "claude-haiku-4-5", false); err == nil {
		t.Fatal("partial write unexpectedly succeeded")
	}
	for _, path := range []string{paths.normal, paths.threep, paths.profile, paths.meta} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("partial write left %s: %v", path, err)
		}
	}
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
}
