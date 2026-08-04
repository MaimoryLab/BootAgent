package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/platform"
)

func TestChatGPTAppStatusIsUnsupportedOutsideDesktopPlatforms(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	status, err := core.ChatGPTAppStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed || status.Supported || status.ID != "chatgpt-desktop" {
		t.Fatalf("status = %#v", status)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("status probe touched shared Codex config: %v", err)
	}
}

func TestInstallChatGPTAppDoesNotWriteSharedCodexConfig(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	_, err := core.InstallChatGPTApp(context.Background())
	if err == nil {
		t.Fatal("unsupported platform install unexpectedly succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(statErr) {
		t.Fatalf("install action touched shared Codex config: %v", statErr)
	}
}
