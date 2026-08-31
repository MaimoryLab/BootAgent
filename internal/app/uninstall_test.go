package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

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
	wantCheck := []string{"/fake/npm", "list", "-g", "--depth=0", "--json", "@openai/codex"}
	wantUninstall := []string{"/fake/npm", "uninstall", "-g", "--ignore-scripts", "@openai/codex"}
	if len(runner.calls) != 2 || !slices.Equal(runner.calls[0], wantCheck) || !slices.Equal(runner.calls[1], wantUninstall) {
		t.Fatalf("uninstall calls = %v, want %v then %v", runner.calls, wantCheck, wantUninstall)
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
		{name: "not installed", agentID: "codex", paths: map[string]string{"npm": "/fake/npm"}, code: oneerrors.AgentPackageMissing},
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

func TestUninstallAgentRejectsPackageOwnedByAnotherNPM(t *testing.T) {
	runner := &installAppRunner{
		paths:     map[string]string{"npm": "/homebrew/bin/npm", "codex": "/mise/shims/codex"},
		exitCodes: map[string]int{"list -g --depth=0 --json @openai/codex": 1},
		stderrs:   map[string]string{"list -g --depth=0 --json @openai/codex": "missing: @openai/codex"},
	}
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Runner: runner})

	_, err := core.UninstallAgent(context.Background(), "codex")
	if err == nil || oneerrors.As(err).Code != oneerrors.AgentNPMMismatch {
		t.Fatalf("mismatched npm owner error = %v, want %s", err, oneerrors.PrerequisiteMissing)
	}
	if len(runner.calls) != 1 || runner.calls[0][1] != "list" {
		t.Fatalf("ownership mismatch ran a mutating command: %v", runner.calls)
	}
}

func TestUninstallAgentCanUseRecordedNPMAfterExplicitCrossEnvironmentApproval(t *testing.T) {
	home := t.TempDir()
	runner := &installAppRunner{
		paths: map[string]string{"npm": "/current/npm", "codex": "/current/bin/codex"},
		exitCodes: map[string]int{
			"list -g --depth=0 --json @openai/codex": 1,
		},
		exitArgs: map[string]int{
			"/original/npm list -g --depth=0 --json @openai/codex":      0,
			"/original/npm uninstall -g --ignore-scripts @openai/codex": 0,
		},
		stderrs: map[string]string{"list -g --depth=0 --json @openai/codex": "missing: @openai/codex"},
	}
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Runner: runner})
	if err := core.saveAgentInstallRecord(context.Background(), agentInstallRecord{Agent: "codex", Package: "@openai/codex", Manager: "npm", NPMPath: "/original/npm", Prefix: "/original/prefix"}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.UninstallAgentWithOptions(context.Background(), "codex", AgentUninstallOptions{AllowCrossEnvironment: true}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 || runner.calls[1][0] != "/original/npm" || runner.calls[2][0] != "/original/npm" {
		t.Fatalf("cross-environment calls = %v", runner.calls)
	}
}

func TestUninstallAgentReportsNonZeroExit(t *testing.T) {
	runner := &installAppRunner{
		paths:     map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"},
		exitCodes: map[string]int{"uninstall -g --ignore-scripts @openai/codex": 1},
	}
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Runner: runner})

	if _, err := core.UninstallAgent(context.Background(), "codex"); err == nil || oneerrors.As(err).Code != oneerrors.AgentNPMFailed {
		t.Fatalf("non-zero uninstall exit should fail, got %v", err)
	}
}

func TestUninstallAgentDistinguishesNPMPermissionFailure(t *testing.T) {
	runner := &installAppRunner{
		paths:     map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"},
		exitCodes: map[string]int{"list -g --depth=0 --json @openai/codex": 1},
		stderrs:   map[string]string{"list -g --depth=0 --json @openai/codex": "EACCES: permission denied"},
	}
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Runner: runner})

	if _, err := core.UninstallAgent(context.Background(), "codex"); err == nil || oneerrors.As(err).Code != oneerrors.AgentNPMPermission {
		t.Fatalf("permission failure should be classified, got %v", err)
	}
}

func TestUninstallAgentDoesNotResumeAfterCancellationWhileWaitingForAgentLock(t *testing.T) {
	runner := &installAppRunner{paths: map[string]string{"npm": "/fake/npm", "codex": "/fake/codex"}}
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Runner: runner})
	unlock := core.lockTask("agent-task:codex")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := core.UninstallAgent(ctx, "codex")
		done <- err
	}()

	select {
	case err := <-done:
		unlock()
		t.Fatalf("uninstall returned before the Agent lock was released: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled uninstall resumed after acquiring the Agent lock")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled uninstall did not return after the Agent lock was released")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("cancelled queued uninstall ran commands: %v", runner.calls)
	}
}
