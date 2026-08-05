package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/desktopapp"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

func TestDesktopAgentStatusIsUnsupportedOutsideDesktopPlatforms(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	status, err := core.DesktopAgentStatus(context.Background(), desktopapp.ChatGPTDesktopID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed || status.Supported || status.ID != desktopapp.ChatGPTDesktopID {
		t.Fatalf("status = %#v", status)
	}
	if status.ConfigPath != filepath.Join(home, ".codex", "config.toml") || status.ConfigSharedWith != "Codex" {
		t.Fatalf("shared config = %q with %q", status.ConfigPath, status.ConfigSharedWith)
	}
	if status.ProfileAgentID != "codex" || status.ProfileID != nil {
		t.Fatalf("profile projection = %#v", status)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("status probe touched shared Codex config: %v", err)
	}
}

func TestConfigureDesktopAgentAcceptsAnyProfileWithAnAPIMode(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "workbuddy", Label: "WorkBuddy", Provider: "ppio", Model: "model-a", ConfigMode: "provider", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfigureDesktopAgent(context.Background(), "workbuddy", "workbuddy"); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "workbuddy-own", Label: "WorkBuddy", Provider: "ppio", Model: "model-a", ConfigMode: "provider", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := core.ConfigureDesktopAgent(context.Background(), "workbuddy", "workbuddy-own")
	if err != nil || result.ProfileAgentID != "workbuddy" || result.ProfileID != "workbuddy-own" {
		t.Fatalf("configure result = %#v, err=%v", result, err)
	}
	binding, err := core.ListAgentBindings(context.Background())
	if err != nil || binding["workbuddy"].ProfileRef != "workbuddy-own" {
		t.Fatalf("desktop profile binding = %#v, err=%v", binding, err)
	}
}

func TestConfigureWorkBuddyWritesModelsJSONFromProvider(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")})
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "workbuddy-profile", Label: "WorkBuddy", Provider: "ppio", APIKey: "provider-secret",
		Model: "model-a", ConfigMode: "provider", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := core.ConfigureDesktopAgent(context.Background(), desktopapp.WorkBuddyID, "workbuddy-profile")
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(home, ".workbuddy", "models.json")
	if result.Config != wantPath || result.ProfileAgentID != desktopapp.WorkBuddyID || result.Restart == "" {
		t.Fatalf("WorkBuddy configure result = %#v", result)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	var models []map[string]any
	if err := json.Unmarshal(data, &models); err != nil || len(models) != 1 {
		t.Fatalf("WorkBuddy models = %s, err=%v", data, err)
	}
	if models[0]["id"] != "model-a" || models[0]["url"] != "https://api.ppio.com/openai" || models[0]["apiKey"] != "provider-secret" {
		t.Fatalf("WorkBuddy model = %#v", models[0])
	}
	info, err := os.Stat(wantPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("WorkBuddy config mode = %v, err=%v", info.Mode().Perm(), err)
	}
	reapplied, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "ppio", Name: "PPIO", BaseURL: "https://relay.example/openai", APIKey: "rotated-key",
	})
	if err != nil || len(reapplied.Failures) != 0 || len(reapplied.Reapplied) != 1 || reapplied.Reapplied[0] != desktopapp.WorkBuddyID {
		t.Fatalf("WorkBuddy Provider reapply = %#v, err=%v", reapplied, err)
	}
	data, err = os.ReadFile(wantPath)
	if err != nil || json.Unmarshal(data, &models) != nil || models[0]["url"] != "https://relay.example/openai" || models[0]["apiKey"] != "rotated-key" {
		t.Fatalf("reapplied WorkBuddy models = %s, err=%v", data, err)
	}
	binding, err := core.profiles.ReadAgentBinding(desktopapp.WorkBuddyID)
	if err != nil || binding == nil || binding.BaseURL != "https://relay.example/openai" {
		t.Fatalf("reapplied WorkBuddy binding = %#v, err=%v", binding, err)
	}
}

func TestConfigureDesktopAgentDoesNotLetBindingOverrideExplicitProfileOwner(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "codex-owned", Label: "Codex", Provider: "ppio", Model: "model-a", ConfigMode: "provider", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.profiles.WriteAgentBinding(context.Background(), "workbuddy", profileStore.BindingWriteRequest{
		Provider: "ppio", BaseURL: "https://api.ppio.com/openai", Model: "model-a", ProfileRef: "codex-owned",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfigureDesktopAgent(context.Background(), "workbuddy", "codex-owned"); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopAgentStatusDoesNotClaimCodexSharingForOtherApps(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	status := core.publicDesktopAgentStatus(desktopapp.Status{ID: "workbuddy", Name: "WorkBuddy"})
	if status.ProfileAgentID != "workbuddy" || status.ConfigSharedWith != "" || status.ConfigPath != "" {
		t.Fatalf("non-shared desktop projection = %#v", status)
	}
}

func TestInstallDesktopAgentDoesNotWriteSharedCodexConfig(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	_, err := core.InstallDesktopAgent(context.Background(), desktopapp.ChatGPTDesktopID, nil)
	if err == nil {
		t.Fatal("unsupported platform install unexpectedly succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(statErr) {
		t.Fatalf("install action touched shared Codex config: %v", statErr)
	}
}
