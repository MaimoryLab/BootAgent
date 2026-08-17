package app

import (
	"strings"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

// terminalAuto is the stored value meaning "let BootAgent pick". It is the empty
// string so a settings file written before this feature reads as auto.
const terminalAuto = ""

// TerminalOption describes one terminal the user may pick for launching CLI
// Agents. Installed is resolved on this machine, so the picker can offer only
// terminals that will actually open.
type TerminalOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

// terminalDefinition is one terminal and how to hand it a shell line.
//
// Detection is deliberately split: Command is resolved on PATH, while Bundles
// are macOS application directories. A terminal shipped as a .app does not put
// its binary on PATH unless the user opted in, so checking only PATH would hide
// installed terminals, and checking only the bundle would offer terminals whose
// binary we cannot invoke.
type terminalDefinition struct {
	id      string
	name    string
	command string
	bundles []string
	argv    func(line string) []string
}

// keepOpen wraps a line so the window survives the Agent exiting. Without it a
// failure flashes past and closes, which reads as "nothing happened".
func keepOpen(line string) []string {
	return []string{"bash", "-lc", line + "; exec bash -l"}
}

// appleScriptTerminal builds the osascript invocation for a terminal that is
// driven by AppleScript rather than argv.
func appleScriptTerminal(app string) func(string) []string {
	return func(line string) []string {
		script := appleScriptQuote(line)
		return []string{
			"osascript",
			"-e", "tell application \"" + app + "\" to do script " + script,
			"-e", "tell application \"" + app + "\" to activate",
		}
	}
}

// macTerminals lists macOS terminals. Terminal.app is first because it ships
// with the OS and is therefore the auto choice, which keeps the default launch
// behavior identical to before this setting existed.
var macTerminals = []terminalDefinition{
	{
		id:      "terminal",
		name:    "Terminal",
		bundles: []string{"/System/Applications/Utilities/Terminal.app", "/Applications/Utilities/Terminal.app"},
		argv:    appleScriptTerminal("Terminal"),
	},
	{
		id:      "iterm",
		name:    "iTerm2",
		bundles: []string{"/Applications/iTerm.app"},
		argv:    appleScriptTerminal("iTerm"),
	},
	{
		id:      "ghostty",
		name:    "Ghostty",
		command: "ghostty",
		argv:    func(line string) []string { return append([]string{"ghostty", "-e"}, keepOpen(line)...) },
	},
	{
		id:      "kitty",
		name:    "kitty",
		command: "kitty",
		argv:    func(line string) []string { return append([]string{"kitty"}, keepOpen(line)...) },
	},
	{
		id:      "wezterm",
		name:    "WezTerm",
		command: "wezterm",
		argv:    func(line string) []string { return append([]string{"wezterm", "start", "--"}, keepOpen(line)...) },
	},
	{
		id:      "alacritty",
		name:    "Alacritty",
		command: "alacritty",
		argv:    func(line string) []string { return append([]string{"alacritty", "-e"}, keepOpen(line)...) },
	},
}

// windowsTerminals lists Windows shells. cmd is first so auto keeps the
// previous `cmd /K` behavior.
var windowsTerminals = []terminalDefinition{
	{
		id:   "cmd",
		name: "Command Prompt",
		argv: func(line string) []string { return []string{"cmd", "/K", line} },
	},
	{
		id:      "windows-terminal",
		name:    "Windows Terminal",
		command: "wt",
		argv:    func(line string) []string { return []string{"wt", "cmd", "/K", line} },
	},
	{
		id:      "powershell",
		name:    "Windows PowerShell",
		command: "powershell",
		argv:    func(line string) []string { return []string{"powershell", "-NoExit", "-Command", line} },
	},
	{
		id:      "pwsh",
		name:    "PowerShell",
		command: "pwsh",
		argv:    func(line string) []string { return []string{"pwsh", "-NoExit", "-Command", line} },
	},
}

// linuxTerminals lists the emulators to try and how each takes a command. The
// order prefers the desktop's own default before named emulators, so auto
// resolves the same way it did before this setting existed.
var linuxTerminals = []terminalDefinition{
	{id: "x-terminal-emulator", name: "System default", command: "x-terminal-emulator", argv: linuxArgv("x-terminal-emulator", "-e")},
	{id: "gnome-terminal", name: "GNOME Terminal", command: "gnome-terminal", argv: linuxArgv("gnome-terminal", "--")},
	{id: "konsole", name: "Konsole", command: "konsole", argv: linuxArgv("konsole", "-e")},
	{id: "xfce4-terminal", name: "Xfce Terminal", command: "xfce4-terminal", argv: linuxArgv("xfce4-terminal", "-e")},
	{id: "kitty", name: "kitty", command: "kitty", argv: linuxArgv("kitty")},
	{id: "ghostty", name: "Ghostty", command: "ghostty", argv: linuxArgv("ghostty", "-e")},
	{id: "wezterm", name: "WezTerm", command: "wezterm", argv: linuxArgv("wezterm", "start", "--")},
	{id: "alacritty", name: "Alacritty", command: "alacritty", argv: linuxArgv("alacritty", "-e")},
	{id: "xterm", name: "xterm", command: "xterm", argv: linuxArgv("xterm", "-e")},
}

func linuxArgv(command string, args ...string) func(string) []string {
	return func(line string) []string {
		return append(append([]string{command}, args...), keepOpen(line)...)
	}
}

func terminalsFor(osID string) []terminalDefinition {
	switch osID {
	case "macos":
		return macTerminals
	case "windows":
		return windowsTerminals
	default:
		return linuxTerminals
	}
}

// pathExists reports whether a path is present. It is a parameter rather than a
// direct os.Stat so tests can describe a machine without creating /Applications.
type pathExists func(string) bool

func (d terminalDefinition) installed(look CommandLookup, exists pathExists) bool {
	// A definition with neither probe is always available: it is a shell the OS
	// guarantees, such as cmd on Windows.
	if d.command == "" && len(d.bundles) == 0 {
		return true
	}
	if d.command != "" && look != nil {
		if _, ok := look(d.command); ok {
			return true
		}
	}
	for _, bundle := range d.bundles {
		if exists != nil && exists(bundle) {
			return true
		}
	}
	return false
}

// availableTerminals lists this platform's terminals for the settings picker,
// marking which are installed. Uninstalled ones are still reported so the UI can
// explain why a stored choice is no longer usable instead of silently dropping
// it from the list.
func availableTerminals(osID string, look CommandLookup, exists pathExists) []TerminalOption {
	definitions := terminalsFor(osID)
	options := make([]TerminalOption, 0, len(definitions))
	for _, definition := range definitions {
		options = append(options, TerminalOption{
			ID:        definition.id,
			Name:      definition.name,
			Installed: definition.installed(look, exists),
		})
	}
	return options
}

// resolveTerminal picks the terminal to launch with and reports which one it
// chose. A stored choice that is no longer installed falls back to auto rather
// than failing the launch, and the returned id is what actually ran -- the
// caller surfaces it so the substitution is visible instead of silent.
func resolveTerminal(osID, requested string, look CommandLookup, exists pathExists) (terminalDefinition, error) {
	definitions := terminalsFor(osID)
	if requested != terminalAuto {
		for _, definition := range definitions {
			if !strings.EqualFold(definition.id, requested) {
				continue
			}
			if definition.installed(look, exists) {
				return definition, nil
			}
			break
		}
	}
	for _, definition := range definitions {
		if definition.installed(look, exists) {
			return definition, nil
		}
	}
	return terminalDefinition{}, oneerrors.New(
		oneerrors.PrerequisiteMissing,
		"No terminal emulator was found; install one of x-terminal-emulator, gnome-terminal, konsole, xfce4-terminal or xterm",
	)
}

// terminalArgv wraps a shell line in the chosen terminal's launcher, returning
// the argv and the id of the terminal that will open.
func terminalArgv(osID, line string, look CommandLookup, exists pathExists, requested string) ([]string, string, error) {
	definition, err := resolveTerminal(osID, requested, look, exists)
	if err != nil {
		return nil, "", err
	}
	return definition.argv(line), definition.id, nil
}
