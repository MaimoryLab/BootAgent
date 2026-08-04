package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/platform"
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
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("status probe touched shared Codex config: %v", err)
	}
}

func TestInstallDesktopAgentDoesNotWriteSharedCodexConfig(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	_, err := core.InstallDesktopAgent(context.Background())
	if err == nil {
		t.Fatal("unsupported platform install unexpectedly succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(statErr) {
		t.Fatalf("install action touched shared Codex config: %v", statErr)
	}
}
