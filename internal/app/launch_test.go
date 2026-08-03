package app

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

// launchRunner records the detached argv instead of opening a window.
type launchRunner struct {
	paths   map[string]string
	started [][]string
	envs    []map[string]string
}

func (r *launchRunner) LookPath(command string) (string, bool) {
	path, ok := r.paths[command]
	return path, ok
}

func (r *launchRunner) Run(context.Context, []string, map[string]string, time.Duration) (process.Result, error) {
	return process.Result{}, nil
}

func (r *launchRunner) Start(argv []string, env map[string]string) error {
	r.started = append(r.started, append([]string(nil), argv...))
	r.envs = append(r.envs, env)
	return nil
}

func launchCore(t *testing.T, osID string, runner process.Runner) *UseCases {
	t.Helper()
	return NewUseCases(StatusOptions{
		Home:        t.TempDir(),
		Platform:    platform.For(osID, "amd64"),
		Runner:      runner,
		Lookup:      runner.LookPath,
		Environment: map[string]string{"PATH": "/usr/bin"},
	})
}

func TestLaunchAgentRunsTheConfiguredAgentAndKeepsTheKeyOut(t *testing.T) {
	runner := &launchRunner{}
	core := launchCore(t, "darwin", runner)
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "codex", Provider: "ppio", APIKey: "launch-secret", Model: "model-a",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := core.LaunchAgent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.started) != 1 || runner.started[0][0] != "osascript" {
		t.Fatalf("started = %#v", runner.started)
	}
	argv := strings.Join(runner.started[0], " ")
	if !strings.Contains(argv, "codex") {
		t.Fatalf("launch argv = %q", argv)
	}
	if strings.Contains(argv, "launch-secret") || strings.Contains(result.Command, "launch-secret") {
		t.Fatal("launch leaked the API key into argv")
	}
	// The window inherits the managed PATH so an Agent installed into the
	// managed global prefix resolves there, not just in a fresh login shell.
	if _, ok := runner.envs[0]["PATH"]; !ok {
		t.Fatalf("launch env = %#v", runner.envs[0])
	}
}

// The Windows window used to be handed a raw -Command string. Go's argv quoting
// plus powershell.exe re-parsing the command line turned a mis-escaped path into
// a parse error, which exits before -NoExit can hold the window open: the user
// saw a flash and nothing else. Encoding the command removes both parse layers.
func TestLaunchAgentEncodesTheWindowsCommand(t *testing.T) {
	runner := &launchRunner{}
	core := launchCore(t, "windows", runner)
	result, err := core.LaunchAgent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	argv := runner.started[0]
	if argv[0] != "powershell" || !slices.Contains(argv, "-NoExit") || !slices.Contains(argv, "-EncodedCommand") {
		t.Fatalf("started = %#v", argv)
	}
	// Aider's launch line dot-sources OneAgent's .ps1 env file, and loading a
	// script file is what the Windows client default of Restricted refuses.
	// Without the process-scoped bypass the window opens only to report that.
	if index := slices.Index(argv, "-ExecutionPolicy"); index < 0 || argv[index+1] != "Bypass" {
		t.Fatalf("windows launch has no execution policy bypass: %#v", argv)
	}
	raw, err := base64.StdEncoding.DecodeString(argv[len(argv)-1])
	if err != nil {
		t.Fatalf("encoded command is not base64: %v", err)
	}
	units := make([]uint16, 0, len(raw)/2)
	for index := 0; index+1 < len(raw); index += 2 {
		units = append(units, uint16(raw[index])|uint16(raw[index+1])<<8)
	}
	decoded := string(utf16.Decode(units))
	if !strings.Contains(decoded, result.Command) {
		t.Fatalf("decoded = %q, want it to run %q", decoded, result.Command)
	}
	// A terminating error unwinds the host and closes the window before the
	// message can be read; -NoExit alone does not cover that case.
	if !strings.Contains(decoded, "catch") || !strings.Contains(decoded, "Read-Host") {
		t.Fatalf("windows script cannot report a failure: %q", decoded)
	}
	// Aider is the one Agent whose credential still lives in a sourced file. A
	// doubled backslash would make PowerShell dot-source a path that does not
	// exist, so it would start unconfigured even when the window survived.
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if line := nextStep("windows", "aider", manifest.Agents["aider"], "model-a"); !strings.Contains(line, `. "$HOME\.oneagent\aider.ps1"`) {
		t.Fatalf("Aider windows launch line = %q", line)
	}
}

func TestLaunchAgentUsesAnAvailableLinuxTerminal(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"konsole": "/usr/bin/konsole"}}
	core := launchCore(t, "linux", runner)
	if _, err := core.LaunchAgent(context.Background(), "codex"); err != nil {
		t.Fatal(err)
	}
	if runner.started[0][0] != "konsole" {
		t.Fatalf("started = %#v", runner.started)
	}

	bare := &launchRunner{}
	if _, err := launchCore(t, "linux", bare).LaunchAgent(context.Background(), "codex"); err == nil {
		t.Fatal("expected a prerequisite error when no terminal emulator exists")
	}
}

func TestLaunchAgentRejectsUnknownAndCommandlessAgents(t *testing.T) {
	core := launchCore(t, "darwin", &launchRunner{})
	if _, err := core.LaunchAgent(context.Background(), "../etc/passwd"); err == nil {
		t.Fatal("expected an invalid ID to be rejected")
	}
	if _, err := core.LaunchAgent(context.Background(), "nope"); err == nil {
		t.Fatal("expected an unknown Agent to be rejected")
	}
	// cline is IDE-only and has no command; launching it would be a no-op window.
	if _, err := core.LaunchAgent(context.Background(), "cline"); err == nil {
		t.Fatal("expected a commandless Agent to be rejected")
	}
}
