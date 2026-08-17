package app

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
)

// noBundles describes a machine with no macOS application bundles present, so a
// case can isolate PATH detection.
func noBundles(string) bool { return false }

func hasBundles(present ...string) pathExists {
	return func(path string) bool { return slices.Contains(present, path) }
}

func onPath(commands ...string) CommandLookup {
	return func(command string) (string, bool) {
		if slices.Contains(commands, command) {
			return "/usr/bin/" + command, true
		}
		return "", false
	}
}

// The auto choice must reproduce what every build before this setting did, or
// upgrading silently changes which window opens.
func TestAutoTerminalKeepsThePreviousPerPlatformDefault(t *testing.T) {
	macArgv, macID, err := terminalArgv("macos", "codex", onPath(), hasBundles("/System/Applications/Utilities/Terminal.app"), terminalAuto)
	if err != nil {
		t.Fatal(err)
	}
	if macID != "terminal" || macArgv[0] != "osascript" {
		t.Fatalf("macos auto = %q %#v", macID, macArgv)
	}
	if !strings.Contains(strings.Join(macArgv, " "), `tell application "Terminal" to do script "codex"`) {
		t.Fatalf("macos auto argv = %#v", macArgv)
	}

	winArgv, winID, err := terminalArgv("windows", "codex", onPath(), noBundles, terminalAuto)
	if err != nil {
		t.Fatal(err)
	}
	if winID != "cmd" || !slices.Equal(winArgv, []string{"cmd", "/K", "codex"}) {
		t.Fatalf("windows auto = %q %#v", winID, winArgv)
	}

	// Linux auto still walks the list in order and takes the first installed one.
	linuxArgv, linuxID, err := terminalArgv("linux", "codex", onPath("konsole"), noBundles, terminalAuto)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"konsole", "-e", "bash", "-lc", "codex; exec bash -l"}
	if linuxID != "konsole" || !slices.Equal(linuxArgv, want) {
		t.Fatalf("linux auto = %q %#v, want %#v", linuxID, linuxArgv, want)
	}
}

func TestChosenTerminalIsUsedWhenInstalled(t *testing.T) {
	cases := []struct {
		name      string
		osID      string
		requested string
		look      CommandLookup
		exists    pathExists
		wantID    string
		wantArgv  []string
	}{
		{
			name: "iTerm on macOS is driven by AppleScript", osID: "macos", requested: "iterm",
			look: onPath(), exists: hasBundles("/Applications/iTerm.app"), wantID: "iterm",
			wantArgv: []string{
				"osascript",
				"-e", `tell application "iTerm" to do script "codex"`,
				"-e", `tell application "iTerm" to activate`,
			},
		},
		{
			name: "kitty on macOS is found on PATH, not as a bundle", osID: "macos", requested: "kitty",
			look: onPath("kitty"), exists: noBundles, wantID: "kitty",
			wantArgv: []string{"kitty", "bash", "-lc", "codex; exec bash -l"},
		},
		{
			name: "Windows Terminal wraps cmd", osID: "windows", requested: "windows-terminal",
			look: onPath("wt"), exists: noBundles, wantID: "windows-terminal",
			wantArgv: []string{"wt", "cmd", "/K", "codex"},
		},
		{
			name: "PowerShell keeps the window open", osID: "windows", requested: "pwsh",
			look: onPath("pwsh"), exists: noBundles, wantID: "pwsh",
			wantArgv: []string{"pwsh", "-NoExit", "-Command", "codex"},
		},
		{
			name: "an explicit Linux choice beats earlier list entries", osID: "linux", requested: "alacritty",
			look: onPath("xterm", "alacritty", "gnome-terminal"), exists: noBundles, wantID: "alacritty",
			wantArgv: []string{"alacritty", "-e", "bash", "-lc", "codex; exec bash -l"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			argv, id, err := terminalArgv(testCase.osID, "codex", testCase.look, testCase.exists, testCase.requested)
			if err != nil {
				t.Fatal(err)
			}
			if id != testCase.wantID || !slices.Equal(argv, testCase.wantArgv) {
				t.Fatalf("terminalArgv = %q %#v, want %q %#v", id, argv, testCase.wantID, testCase.wantArgv)
			}
		})
	}
}

// A terminal the user picked and then uninstalled must not break launching. The
// returned id is what actually ran, so the caller can say so.
func TestUninstalledChoiceFallsBackToAutoAndReportsWhatRan(t *testing.T) {
	argv, id, err := terminalArgv("macos", "codex", onPath(), hasBundles("/System/Applications/Utilities/Terminal.app"), "iterm")
	if err != nil {
		t.Fatal(err)
	}
	if id != "terminal" {
		t.Fatalf("fallback terminal = %q, want terminal", id)
	}
	if !strings.Contains(strings.Join(argv, " "), `application "Terminal"`) {
		t.Fatalf("fallback argv = %#v", argv)
	}
}

// An id from a settings file written on another platform must not win over an
// installed terminal here.
func TestUnknownTerminalIDFallsBackToAuto(t *testing.T) {
	_, id, err := terminalArgv("linux", "codex", onPath("xterm"), noBundles, "windows-terminal")
	if err != nil {
		t.Fatal(err)
	}
	if id != "xterm" {
		t.Fatalf("terminal = %q, want xterm", id)
	}
}

func TestNoTerminalAtAllStillReportsThePrerequisite(t *testing.T) {
	if _, _, err := terminalArgv("linux", "codex", onPath(), noBundles, terminalAuto); err == nil {
		t.Fatal("expected a prerequisite error when no emulator is installed")
	}
}

func TestAvailableTerminalsMarksWhatIsInstalled(t *testing.T) {
	options := availableTerminals("macos", onPath("wezterm"), hasBundles("/Applications/iTerm.app"))
	installed := map[string]bool{}
	for _, option := range options {
		installed[option.ID] = option.Installed
		if option.Name == "" {
			t.Fatalf("option %q has no display name", option.ID)
		}
	}
	if !installed["iterm"] || !installed["wezterm"] {
		t.Fatalf("installed = %#v", installed)
	}
	if installed["kitty"] || installed["ghostty"] {
		t.Fatalf("absent terminals reported as installed: %#v", installed)
	}
	// cmd on Windows has neither probe: the OS guarantees it, so it is always on.
	for _, option := range availableTerminals("windows", onPath(), noBundles) {
		if option.ID == "cmd" && !option.Installed {
			t.Fatal("cmd must always be available on Windows")
		}
	}
}

func TestNormalizeTerminalAppRejectsForeignIDs(t *testing.T) {
	if got := normalizeTerminalApp("macos", "iterm"); got != "iterm" {
		t.Fatalf("normalize(iterm) = %q", got)
	}
	// Stored on Windows, read on macOS: not offered here, so it degrades to auto
	// rather than blocking an unrelated setting from being saved.
	if got := normalizeTerminalApp("macos", "windows-terminal"); got != terminalAuto {
		t.Fatalf("normalize(windows-terminal on macos) = %q, want auto", got)
	}
	if got := normalizeTerminalApp("macos", "ITERM"); got != "iterm" {
		t.Fatalf("normalize is case sensitive: %q", got)
	}
}

// The choice has to survive a round trip, and the derived terminal list must not
// be persisted -- a stored list would outlive the machine state that produced it.
func TestSettingsRoundTripTheTerminalChoice(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"konsole": "/usr/bin/konsole"}}
	core := launchCore(t, "linux", runner)
	konsole := "konsole"
	saved, err := core.UpdateSettings(context.Background(), SettingsPatch{TerminalApp: &konsole})
	if err != nil {
		t.Fatal(err)
	}
	if saved.TerminalApp != "konsole" {
		t.Fatalf("saved terminal = %q", saved.TerminalApp)
	}
	if len(saved.Terminals) == 0 {
		t.Fatal("save did not report the available terminals")
	}
	reread, err := core.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reread.TerminalApp != "konsole" {
		t.Fatalf("reread terminal = %q", reread.TerminalApp)
	}
	if !slices.ContainsFunc(reread.Terminals, func(option TerminalOption) bool {
		return option.ID == "konsole" && option.Installed
	}) {
		t.Fatalf("reread terminals = %#v", reread.Terminals)
	}
	if data := storedSettingsBytes(t, core); strings.Contains(data, "terminals") {
		t.Fatalf("the derived terminal list was persisted: %s", data)
	}
}

// Every other settings control sends a patch naming only its own field. An
// omitted terminal_app must carry the stored choice forward, or toggling the
// mirror or launch-at-login would silently send the user back to the default
// terminal.
func TestPatchingAnotherSettingKeepsTheStoredTerminal(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"konsole": "/usr/bin/konsole"}}
	core := launchCore(t, "linux", runner)
	konsole := "konsole"
	if _, err := core.UpdateSettings(context.Background(), SettingsPatch{TerminalApp: &konsole}); err != nil {
		t.Fatal(err)
	}
	mirror := true
	saved, err := core.UpdateSettings(context.Background(), SettingsPatch{PreferMirror: &mirror})
	if err != nil {
		t.Fatal(err)
	}
	if saved.TerminalApp != "konsole" {
		t.Fatalf("terminal after an unrelated patch = %q, want konsole", saved.TerminalApp)
	}
	reread, err := core.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reread.TerminalApp != "konsole" {
		t.Fatalf("reread terminal = %q, want konsole", reread.TerminalApp)
	}
}

// Explicitly choosing auto has to be distinguishable from omitting the field.
func TestChoosingAutoExplicitlyClearsTheStoredChoice(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"konsole": "/usr/bin/konsole"}}
	core := launchCore(t, "linux", runner)
	konsole := "konsole"
	if _, err := core.UpdateSettings(context.Background(), SettingsPatch{TerminalApp: &konsole}); err != nil {
		t.Fatal(err)
	}
	auto := terminalAuto
	saved, err := core.UpdateSettings(context.Background(), SettingsPatch{TerminalApp: &auto})
	if err != nil {
		t.Fatal(err)
	}
	if saved.TerminalApp != terminalAuto {
		t.Fatalf("terminal after choosing auto = %q, want auto", saved.TerminalApp)
	}
}

// A launch must honor the stored choice end to end, not just in the pure helper.
func TestLaunchAgentUsesTheStoredTerminalChoice(t *testing.T) {
	runner := &launchRunner{paths: map[string]string{"konsole": "/usr/bin/konsole", "xterm": "/usr/bin/xterm"}}
	core := launchCore(t, "linux", runner)
	xterm := "xterm"
	if _, err := core.UpdateSettings(context.Background(), SettingsPatch{TerminalApp: &xterm}); err != nil {
		t.Fatal(err)
	}
	result, err := core.LaunchAgent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if result.Terminal != "xterm" {
		t.Fatalf("launched terminal = %q, want xterm", result.Terminal)
	}
	// xterm is last in the list, so picking it proves the stored choice beat the
	// auto order rather than coinciding with it.
	if len(runner.started) != 1 || runner.started[0][0] != "xterm" {
		t.Fatalf("started = %#v", runner.started)
	}
}

func storedSettingsBytes(t *testing.T, core *UseCases) string {
	t.Helper()
	data, err := os.ReadFile(core.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
