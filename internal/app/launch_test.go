package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
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

// webUIProbe stands in for the port and the browser. readyAfter is how many dials
// happen before the address answers, so a test can model a server that is already
// up (0) or one that binds shortly after its window opens.
type webUIProbe struct {
	readyAfter int
	dials      int
	dialed     []string
	opened     []string
	openErr    error
}

func (p *webUIProbe) dial(address string) bool {
	p.dials++
	p.dialed = append(p.dialed, address)
	return p.dials > p.readyAfter
}

func (p *webUIProbe) open(url string) error {
	p.opened = append(p.opened, url)
	return p.openErr
}

// launchWebCore is launchCore with the port and browser stubbed. Agents without a
// web_url never reach either hook, which is why the other tests need no probe.
func launchWebCore(t *testing.T, osID string, runner process.Runner, probe *webUIProbe) *UseCases {
	t.Helper()
	return NewUseCases(StatusOptions{
		Home:        t.TempDir(),
		Platform:    platform.For(osID, "amd64"),
		Runner:      runner,
		Lookup:      runner.LookPath,
		Environment: map[string]string{"PATH": "/usr/bin"},
		OpenURL:     probe.open,
		DialWebUI:   probe.dial,
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

func TestLaunchAgentUsesCmdOnWindows(t *testing.T) {
	runner := &launchRunner{}
	core := launchCore(t, "windows", runner)
	result, err := core.LaunchAgent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	argv := runner.started[0]
	if want := []string{"cmd", "/K", result.Command}; !slices.Equal(argv, want) {
		t.Fatalf("started = %#v, want %#v", argv, want)
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if line := nextStep("windows", "aider", manifest.Agents["aider"], "model-a"); line != `aider --env-file "%USERPROFILE%\.bootagent\aider.env" --model openai/model-a` {
		t.Fatalf("Aider windows launch line = %q", line)
	}
}

// dsh boots a profile and its launcher requires one: the bare command exits
// nonzero with "--profile <name> is required". Since LaunchAgent runs this line
// in a real terminal, a bare command would open a window only to print an error.
func TestLaunchAgentGivesDSHABootableProfile(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"x-terminal-emulator": "/usr/bin/x-terminal-emulator"}}
	// Not yet serving, then up on the next poll: the window it just opened is what
	// binds the port.
	core := launchWebCore(t, "linux", runner, &webUIProbe{readyAfter: 1})
	result, err := core.LaunchAgent(context.Background(), "dsh")
	if err != nil {
		t.Fatal(err)
	}
	if result.Command != "dsh web" {
		t.Fatalf("dsh launch line = %q, want %q", result.Command, "dsh web")
	}
	// The line reaches the terminal wrapped in its shell invocation, so the
	// assertion is that it carries the profile, not that it is the whole argument.
	if len(runner.started) != 1 || !slices.ContainsFunc(runner.started[0], func(argument string) bool {
		return strings.Contains(argument, "dsh web")
	}) {
		t.Fatalf("terminal argv = %#v", runner.started)
	}
}

// dsh's interface is a local web app, so the terminal is only how the server gets
// started: the launch is not finished until the page is open in the browser.
func TestLaunchAgentOpensTheWebUIAfterStartingItsServer(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"x-terminal-emulator": "/usr/bin/x-terminal-emulator"}}
	probe := &webUIProbe{readyAfter: 1}
	core := launchWebCore(t, "linux", runner, probe)
	result, err := core.LaunchAgent(context.Background(), "dsh")
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.started) != 1 {
		t.Fatalf("expected one terminal window, started = %#v", runner.started)
	}
	if want := []string{"http://127.0.0.1:3080"}; !slices.Equal(probe.opened, want) {
		t.Fatalf("opened = %#v, want %#v", probe.opened, want)
	}
	if result.WebURL != "http://127.0.0.1:3080" {
		t.Fatalf("result.WebURL = %q", result.WebURL)
	}
	// The port, not the URL, is what gets dialled.
	if len(probe.dialed) == 0 || probe.dialed[0] != "127.0.0.1:3080" {
		t.Fatalf("dialed = %#v", probe.dialed)
	}
}

// A second window would only fail to bind the port and show that error, so a
// server that is already up is joined rather than duplicated.
func TestLaunchAgentSkipsTheTerminalWhenTheWebUIIsAlreadyServing(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"x-terminal-emulator": "/usr/bin/x-terminal-emulator"}}
	probe := &webUIProbe{readyAfter: 0}
	core := launchWebCore(t, "linux", runner, probe)
	result, err := core.LaunchAgent(context.Background(), "dsh")
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.started) != 0 {
		t.Fatalf("expected no terminal window, started = %#v", runner.started)
	}
	if want := []string{"http://127.0.0.1:3080"}; !slices.Equal(probe.opened, want) {
		t.Fatalf("opened = %#v, want %#v", probe.opened, want)
	}
	// No window ran, so naming one would be a claim about something that did not
	// happen.
	if result.Terminal != "" {
		t.Fatalf("result.Terminal = %q, want empty", result.Terminal)
	}
}

// The ordinary terminal Agents have no web_url, and must not gain a browser hop
// or a port probe because one Agent does.
func TestLaunchAgentLeavesTerminalAgentsUntouched(t *testing.T) {
	runner := &launchRunner{}
	probe := &webUIProbe{}
	core := launchWebCore(t, "darwin", runner, probe)
	result, err := core.LaunchAgent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.opened) != 0 || len(probe.dialed) != 0 {
		t.Fatalf("codex touched the web path: opened = %#v, dialed = %#v", probe.opened, probe.dialed)
	}
	if result.WebURL != "" {
		t.Fatalf("result.WebURL = %q, want empty", result.WebURL)
	}
	if result.Terminal == "" {
		t.Fatal("expected a terminal for an ordinary Agent")
	}
}

// A browser that will not open is worth reporting: the page is what the user
// asked for, and the terminal window alone does not deliver it.
func TestLaunchAgentReportsABrowserThatCannotOpen(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"x-terminal-emulator": "/usr/bin/x-terminal-emulator"}}
	probe := &webUIProbe{readyAfter: 0, openErr: errors.New("no browser")}
	core := launchWebCore(t, "linux", runner, probe)
	if _, err := core.LaunchAgent(context.Background(), "dsh"); err == nil {
		t.Fatal("expected a failing browser to surface")
	}
}

// The wait must not outlive the caller: a cancelled launch stops polling instead
// of holding the request open for the full timeout.
func TestLaunchAgentStopsWaitingForTheWebUIWhenCancelled(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"x-terminal-emulator": "/usr/bin/x-terminal-emulator"}}
	// Never answers, so only cancellation can end the wait.
	probe := &webUIProbe{readyAfter: 1 << 30}
	core := launchWebCore(t, "linux", runner, probe)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if _, err := core.LaunchAgent(ctx, "dsh"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= webUIWaitTimeout {
		t.Fatalf("wait ignored cancellation, took %s", elapsed)
	}
	// The browser still opens: the user asked for the page, and a late server is
	// better answered by a reload than by BootAgent calling the launch a failure.
	if len(probe.opened) != 1 {
		t.Fatalf("opened = %#v", probe.opened)
	}
}

// Both files BootAgent writes for dsh are watched and re-read per request, so
// telling the user to quit a running session would be busywork.
func TestDSHActivationDoesNotAskForARestart(t *testing.T) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	hint := restartHint("dsh", manifest.Agents["dsh"])
	if !strings.Contains(hint, "No restart needed") {
		t.Fatalf("dsh restart hint = %q", hint)
	}
}

func TestLaunchAgentChangesWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	core := launchCore(t, "linux", &launchRunner{paths: map[string]string{"x-terminal-emulator": "/usr/bin/x-terminal-emulator"}})
	result, err := core.LaunchAgent(context.Background(), "codex", directory)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Command, "cd '") || !strings.Contains(result.Command, directory) {
		t.Fatalf("launch command = %q", result.Command)
	}
}

func TestLaunchAgentRejectsQuotedWindowsDirectory(t *testing.T) {
	runner := &launchRunner{}
	core := launchCore(t, "windows", runner)
	if _, err := core.LaunchAgent(context.Background(), "codex", `C:\bad" & whoami`); err == nil {
		t.Fatal("quoted Windows directory was accepted")
	}
}

func TestLaunchDirectoryQuotesShellMetacharacters(t *testing.T) {
	if got := shellQuote("a'b"); got != `'a'\''b'` {
		t.Fatalf("shell quote = %q", got)
	}
}

func TestOfficialInstallerUsesPowerShellOnWindows(t *testing.T) {
	runner := &launchRunner{}
	core := launchCore(t, "windows", runner)
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	run := installRun{core: core}
	if err := run.launchOfficialInstaller(context.Background(), manifest.Agents["hermes"]); err != nil {
		t.Fatal(err)
	}
	want := []string{"powershell.exe", "-NoExit", "-Command", "& ([scriptblock]::Create((irm https://hermes-agent.nousresearch.com/install.ps1))) -SkipSetup"}
	if len(runner.started) != 1 || !slices.Equal(runner.started[0], want) {
		t.Fatalf("started = %#v, want %#v", runner.started, want)
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
