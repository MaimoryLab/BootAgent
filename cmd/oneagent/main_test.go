package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/platform"
)

func TestAgentSetAndListCLIUseGoBindingsWithoutLeakingKey(t *testing.T) {
	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"agent", "set", "codex", "--provider", "ppio", "--model", "model-a", "--api-key", "cli-secret", "--json", "--home", home}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("set exit=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "cli-secret") {
		t.Fatal("API key appeared in agent set output")
	}
	var setPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &setPayload); err != nil || setPayload["provider"] != "ppio" {
		t.Fatalf("set payload=%s err=%v", stdout.String(), err)
	}
	if _, err := os.Stat(filepath.Join(home, ".oneagent", "agents", "codex.json")); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"agent", "list", "--json", "--home", home}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list exit=%d stderr=%s", code, stderr.String())
	}
	var listPayload struct {
		OK     bool                      `json:"ok"`
		Agents map[string]map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &listPayload); err != nil || !listPayload.OK {
		t.Fatalf("list payload=%s err=%v", stdout.String(), err)
	}
	if listPayload.Agents["codex"]["model"] != "model-a" {
		t.Fatalf("agents=%v", listPayload.Agents)
	}
}

// The compatibility wrappers and tests/install_test.sh grep this help text and
// treat a non-zero exit as a failure, so help keeps the wrapper's contract:
// exit 0, double-dash flag names, one write.
func TestHelpMatchesTheCLIContract(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"--agent", "codex", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("%v exit=%d stderr=%s", args, code, stderr.String())
		}
		help := stdout.String()
		for _, flagName := range []string{
			"--register-url URL", "--agent AGENT", "--check-agent-only",
			"--agent-version VERSION", "--registry REGISTRY", "--skip-test",
		} {
			if !strings.Contains(help, flagName) {
				t.Fatalf("%v help is missing %q", args, flagName)
			}
		}
		if stderr.Len() != 0 {
			t.Fatalf("%v wrote help diagnostics to stderr: %s", args, stderr.String())
		}
	}
}

// An interrupt is not an operation failure: it exits with the shell convention
// and writes no error payload, matching the CLI interrupt path.
func TestInterruptExitCodeUsesShellConventionWithoutErrorPayload(t *testing.T) {
	if code, interrupted := interruptExitCode(context.Background()); interrupted || code != 0 {
		t.Fatalf("uncancelled context reported code=%d interrupted=%v", code, interrupted)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	code, interrupted := interruptExitCode(cancelled)
	if !interrupted || code != 130 {
		t.Fatalf("cancelled context reported code=%d interrupted=%v", code, interrupted)
	}
}

// The install path must observe cancellation rather than run to completion, so
// a signal stops provider requests and package-manager subprocesses with it.
func TestInstallHonoursACancelledContext(t *testing.T) {
	home := t.TempDir()
	core := app.NewUseCases(app.StatusOptions{Home: home, Platform: platform.Current()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := core.InstallAgents(ctx, app.InstallAgentsOptions{
		Agents: []string{"codex"}, Provider: "ppio", APIKey: "cancel-secret",
		Model: "model-a", Configure: true, SkipTest: true, Timeout: 30 * time.Second,
	})
	if err == nil {
		t.Fatal("a cancelled install returned no error")
	}
	if strings.Contains(err.Error(), "cancel-secret") {
		t.Fatalf("cancellation error leaked the API key: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex", "config.toml")); statErr == nil {
		t.Fatal("a cancelled install still wrote Agent configuration")
	}
}

func TestFlatCLIRejectsEmptyAgentListAndInvalidVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--agent", ",", "--check-agent-only"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "At least one Agent") {
		t.Fatalf("empty agents exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--check-agent-only", "--agent-version", "1.2.3@other", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("invalid version exit=%d output=%q", code, stdout.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil || payload["error_code"] != "INVALID_REQUEST" {
		t.Fatalf("error payload=%q err=%v", stdout.String(), err)
	}
}
