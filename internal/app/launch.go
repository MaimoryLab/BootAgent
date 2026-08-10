package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

// LaunchAgentResult reports the shell line a new terminal window was given.
// The line is public on purpose: it is what the user would have typed, and it
// carries no credential — the key stays in the Agent's own configuration.
type LaunchAgentResult struct {
	Agent   string `json:"agent"`
	Command string `json:"command"`
}

// LaunchAgent opens a terminal window running one configured Agent. It reuses
// nextStep, so the window gets the same command the activation screen tells the
// user to run, including Aider's env file when that is what the Agent needs.
func (u *UseCases) LaunchAgent(ctx context.Context, agentID string, directories ...string) (LaunchAgentResult, error) {
	if u == nil {
		return LaunchAgentResult{}, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Agent launch request was cancelled"); err != nil {
		return LaunchAgentResult{}, err
	}
	if !managedAgentIDPattern.MatchString(agentID) {
		return LaunchAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "Invalid Agent ID: "+agentID)
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return LaunchAgentResult{}, err
	}
	agent, ok := manifest.Agents[agentID]
	if !ok {
		return LaunchAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "Unknown Agent: "+agentID)
	}
	if agent.Command == "" {
		return LaunchAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, agent.Name+" has no command to launch")
	}
	model := ""
	if binding, err := u.profiles.ReadAgentBinding(agentID); err == nil && binding != nil {
		model = binding.Model
	}
	line := nextStep(u.status.Platform.OS, agentID, agent, model)
	if line == "" {
		// Guide-only Agents have no managed configuration; the bare command is
		// still the right thing to run.
		line = agent.Command
	}
	workingDirectory := ""
	if len(directories) > 0 {
		workingDirectory = directories[0]
	}
	if workingDirectory != "" {
		if u.status.Platform.OS == "windows" && strings.ContainsAny(workingDirectory, "\"\r\n") {
			return LaunchAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "Invalid launch directory")
		}
		info, err := os.Stat(workingDirectory)
		if err != nil || !info.IsDir() {
			return LaunchAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Invalid launch directory: %s", workingDirectory))
		}
		line = launchInDirectory(u.status.Platform.OS, workingDirectory, line)
	}
	launcher, ok := process.AsLauncher(u.runner)
	if !ok {
		return LaunchAgentResult{}, oneerrors.New(oneerrors.InternalError, "This build cannot open a terminal window", oneerrors.WithStatus(501))
	}
	argv, err := terminalArgv(u.status.Platform.OS, line, u.runner.LookPath)
	if err != nil {
		return LaunchAgentResult{}, err
	}
	// The window resolves the Agent the same way status said it could: an Agent
	// CLI under the managed global prefix is only on PATH once the managed
	// directories are prepended, and a shell started before that install would
	// not see them.
	if err := launcher.Start(argv, u.installRuntime(nil).Env); err != nil {
		return LaunchAgentResult{}, oneerrors.New(
			oneerrors.InternalError,
			"Cannot open a terminal window for "+agent.Name,
			oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err),
		)
	}
	return LaunchAgentResult{Agent: agentID, Command: line}, nil
}

func launchInDirectory(osID, directory, line string) string {
	if osID == "windows" {
		return `cd /d "` + directory + `" && ` + line
	}
	return "cd " + shellQuote(directory) + " && " + line
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\\"'\\\"'") + "'"
}

// linuxTerminals lists the emulators to try and how each takes a command. The
// order prefers the desktop's own default before named emulators.
var linuxTerminals = []struct {
	command string
	args    []string
}{
	{"x-terminal-emulator", []string{"-e"}},
	{"gnome-terminal", []string{"--"}},
	{"konsole", []string{"-e"}},
	{"xfce4-terminal", []string{"-e"}},
	{"kitty", nil},
	{"alacritty", []string{"-e"}},
	{"xterm", []string{"-e"}},
}

// terminalArgv wraps a shell line in the platform's terminal launcher.
func terminalArgv(osID, line string, look CommandLookup) ([]string, error) {
	switch osID {
	case "macos":
		script := appleScriptQuote(line)
		return []string{
			"osascript",
			"-e", "tell application \"Terminal\" to do script " + script,
			"-e", "tell application \"Terminal\" to activate",
		}, nil
	case "windows":
		return []string{"cmd", "/K", line}, nil
	default:
		// The trailing shell keeps the window open after the Agent exits, so a
		// failure is readable instead of flashing past.
		shell := []string{"bash", "-lc", line + "; exec bash -l"}
		for _, terminal := range linuxTerminals {
			if _, present := look(terminal.command); !present {
				continue
			}
			return append(append([]string{terminal.command}, terminal.args...), shell...), nil
		}
		return nil, oneerrors.New(oneerrors.PrerequisiteMissing, "No terminal emulator was found; install one of x-terminal-emulator, gnome-terminal, konsole, xfce4-terminal or xterm")
	}
}

// appleScriptQuote renders a shell line as an AppleScript string literal.
func appleScriptQuote(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
