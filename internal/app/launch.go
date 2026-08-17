package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// LaunchAgentResult reports the shell line a new terminal window was given.
// The line is public on purpose: it is what the user would have typed, and it
// carries no credential — the key stays in the Agent's own configuration.
type LaunchAgentResult struct {
	Agent   string `json:"agent"`
	Command string `json:"command"`
	// Terminal is the terminal that actually opened. It can differ from the
	// stored preference when that terminal is no longer installed, so the caller
	// can say which one ran instead of leaving the substitution invisible.
	Terminal string `json:"terminal"`
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
	argv, terminal, err := terminalArgv(u.status.Platform.OS, line, u.lookPath, u.pathExists, u.terminalApp(ctx))
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
	return LaunchAgentResult{Agent: agentID, Command: line, Terminal: terminal}, nil
}

func launchInDirectory(osID, directory, line string) string {
	if osID == "windows" {
		return `cd /d "` + directory + `" && ` + line
	}
	return "cd " + shellQuote(directory) + " && " + line
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// appleScriptQuote renders a shell line as an AppleScript string literal.
func appleScriptQuote(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
