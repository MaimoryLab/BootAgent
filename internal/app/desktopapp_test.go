package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/desktopapp"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
)

func TestDesktopAgentStatusIsUnsupportedOutsideDesktopPlatforms(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	status, err := core.DesktopAgentStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed || status.Supported || status.ID != "desktop-agent" {
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
	_, err := core.InstallDesktopAgent(context.Background(), nil)
	if err == nil {
		t.Fatal("unsupported platform install unexpectedly succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(statErr) {
		t.Fatalf("install action touched shared Codex config: %v", statErr)
	}
}
