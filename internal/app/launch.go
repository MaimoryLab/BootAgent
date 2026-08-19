package app

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

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
	// Empty when the Agent's server was already serving and no window was needed.
	Terminal string `json:"terminal"`
	// WebURL is the address opened in the browser, set only for Agents whose
	// interface is a local web app.
	WebURL string `json:"web_url,omitempty"`
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
	// A web-app Agent that is already serving needs no second server: starting one
	// would only fail to bind the port and leave a terminal window whose whole
	// content is that error. The page is the destination either way.
	address, servingAlready := u.webUIAddress(agent)
	if servingAlready {
		if err := u.openWebUI(agent); err != nil {
			return LaunchAgentResult{}, err
		}
		return LaunchAgentResult{Agent: agentID, Command: line, WebURL: agent.WebURL}, nil
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
	if address != "" {
		// The server was just asked to start, so the port is not up yet. Waiting
		// avoids handing the browser an address that would answer "connection
		// refused"; the wait is bounded so a server that never binds delays the
		// launch rather than hanging it.
		u.waitForWebUI(ctx, address)
		if err := u.openWebUI(agent); err != nil {
			return LaunchAgentResult{}, err
		}
	}
	return LaunchAgentResult{Agent: agentID, Command: line, Terminal: terminal, WebURL: agent.WebURL}, nil
}

// webUIDialTimeout bounds one connection attempt, and webUIWaitTimeout the whole
// wait for a server that was just started. The total is short enough that a
// launch still feels like a launch if the server never binds.
const (
	webUIDialTimeout = 300 * time.Millisecond
	webUIWaitTimeout = 15 * time.Second
	webUIPollEvery   = 150 * time.Millisecond
)

// webUIAddress returns the host:port to probe for a web-app Agent, and whether
// something is already serving there. Both are empty/false for the ordinary
// terminal Agents, which is what keeps their launch path unchanged.
func (u *UseCases) webUIAddress(agent catalog.Agent) (string, bool) {
	if u == nil || agent.WebURL == "" {
		return "", false
	}
	parsed, err := url.Parse(agent.WebURL)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	address := parsed.Host
	if parsed.Port() == "" {
		// Only the two schemes a local web app would use; anything else is a
		// manifest mistake rather than something to guess a port for.
		switch parsed.Scheme {
		case "http":
			address = net.JoinHostPort(parsed.Hostname(), "80")
		case "https":
			address = net.JoinHostPort(parsed.Hostname(), "443")
		default:
			return "", false
		}
	}
	return address, u.dialWebUI(address)
}

func (u *UseCases) dialWebUI(address string) bool {
	if u.status.DialWebUI != nil {
		return u.status.DialWebUI(address)
	}
	connection, err := net.DialTimeout("tcp", address, webUIDialTimeout)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

// waitForWebUI blocks until the address answers, the deadline passes, or the
// caller gives up. It reports nothing: a timeout still opens the browser, since
// the user asked for the page and a late server is better served by a reload
// than by BootAgent deciding the launch failed.
func (u *UseCases) waitForWebUI(ctx context.Context, address string) {
	deadline := time.Now().Add(webUIWaitTimeout)
	for {
		if u.dialWebUI(address) {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(webUIPollEvery):
		}
	}
}

func (u *UseCases) openWebUI(agent catalog.Agent) error {
	if u.status.OpenURL == nil {
		return oneerrors.New(oneerrors.InternalError, "Desktop browser is not configured", oneerrors.WithStatus(501))
	}
	if err := u.status.OpenURL(agent.WebURL); err != nil {
		return oneerrors.New(
			oneerrors.InternalError,
			"Cannot open "+agent.Name+" at "+agent.WebURL,
			oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err),
		)
	}
	return nil
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
