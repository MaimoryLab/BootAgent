package app

import (
	"context"
	"slices"
	"testing"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/platform"
)

func TestUpdateAgentResolvesMirrorRegistry(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"}}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Runner: runner})
	if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := core.UpdateAgent(context.Background(), "codex"); err != nil {
		t.Fatal(err)
	}
	const registry = "https://registry.npmmirror.com/"
	if !slices.Contains(runner.calls[0], "--registry="+registry) || runner.envs[0]["npm_config_registry"] != registry {
		t.Fatalf("update call = %v env = %v, want resolved mirror registry", runner.calls[0], runner.envs[0])
	}
}

func TestUpdateAgentFailsOnNonZeroExitCode(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{paths: map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"}, exitCode: 1}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Runner: runner})

	if _, err := core.UpdateAgent(context.Background(), "codex"); err == nil || oneerrors.As(err).Code != oneerrors.AgentInstallFailed {
		t.Fatalf("non-zero update exit should fail, got %v", err)
	}
}
