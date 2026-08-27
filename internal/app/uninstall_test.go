package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/platform"
)

func TestUninstallAgentRemovesOnlyTheManagedNPMPackage(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("model = \"keep-me\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &installAppRunner{paths: map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"}}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Runner: runner})

	result, err := core.UninstallAgent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/fake/npm", "uninstall", "-g", "@openai/codex"}
	if len(runner.calls) != 1 || !slices.Equal(runner.calls[0], want) {
		t.Fatalf("uninstall call = %v, want %v", runner.calls, want)
	}
	if result.Agent != "codex" || result.Package != "@openai/codex" {
		t.Fatalf("uninstall result = %#v", result)
	}
	if data, readErr := os.ReadFile(configPath); readErr != nil || string(data) != "model = \"keep-me\"\n" {
		t.Fatalf("uninstall changed user config: data=%q err=%v", data, readErr)
	}
}

func TestUninstallAgentRejectsUnsupportedOrMissingAgents(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		paths   map[string]string
		code    string
	}{
		{name: "unknown", agentID: "missing", paths: map[string]string{"npm": "/fake/npm"}, code: oneerrors.InvalidRequest},
		{name: "not npm managed", agentID: "aider", paths: map[string]string{"npm": "/fake/npm", "aider": "/fake/aider"}, code: oneerrors.InvalidRequest},
		{name: "not installed", agentID: "codex", paths: map[string]string{"npm": "/fake/npm"}, code: oneerrors.PrerequisiteMissing},
		{name: "npm missing", agentID: "codex", paths: map[string]string{"codex": "/fake/codex"}, code: oneerrors.PrerequisiteMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &installAppRunner{paths: test.paths}
			core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Runner: runner})
			_, err := core.UninstallAgent(context.Background(), test.agentID)
			if err == nil || oneerrors.As(err).Code != test.code {
				t.Fatalf("UninstallAgent(%q) error = %v, want %s", test.agentID, err, test.code)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("rejected uninstall ran commands: %v", runner.calls)
			}
		})
	}
}

func TestUninstallAgentReportsNonZeroExit(t *testing.T) {
	runner := &installAppRunner{
		paths:    map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"},
		exitCode: 1,
	}
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Runner: runner})

	if _, err := core.UninstallAgent(context.Background(), "codex"); err == nil || oneerrors.As(err).Code != oneerrors.AgentInstallFailed {
		t.Fatalf("non-zero uninstall exit should fail, got %v", err)
	}
}
