package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/platform"
)

func TestStatusUsesInjectedHomeAndCommandLookup(t *testing.T) {
	home := t.TempDir()
	lookup := func(command string) (string, bool) {
		if command == "npm" || command == "codex" {
			return "/fake/" + command, true
		}
		return "", false
	}
	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   lookup,
	})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.APIVersion != 1 || status.Platform.OS != "linux" {
		t.Fatalf("unexpected status header: %#v", status)
	}
	if !status.Agents["codex"].Installed || !status.Capabilities.CanInstall["codex"] {
		t.Fatalf("injected command lookup was not used: %#v", status.Agents["codex"])
	}
	if status.Paths["profile"] != filepath.Join(home, ".oneagent", "profile.json") {
		t.Fatalf("profile path escaped injected home: %q", status.Paths["profile"])
	}
	wire, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) == "" || hasSubstring(string(wire), "api_key") || hasSubstring(string(wire), "fallback") {
		t.Fatalf("status contains a secret/internal field: %s", wire)
	}
}

func TestStatusReportsExistingConfigWithoutWriting(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("model = 'local'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Lookup: func(string) (string, bool) { return "", false }})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Agents["codex"].Configured {
		t.Fatal("existing config was not observed")
	}
}

func TestStatusHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewUseCases(StatusOptions{Platform: platform.For("linux", "amd64")}).GetStatus(ctx)
	if err == nil || !hasSubstring(err.Error(), "cancelled") {
		t.Fatalf("cancellation was not mapped: %v", err)
	}
}

func hasSubstring(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
