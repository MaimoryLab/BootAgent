package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/install"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// AgentUninstallResult reports the package removed without implying that user
// configuration, Profiles, Providers, or conversation data were deleted.
type AgentUninstallResult struct {
	Agent   string `json:"agent"`
	Package string `json:"package"`
	Command string `json:"command"`
}

// UninstallAgent removes one npm-managed Agent executable. User-owned state is
// deliberately outside this operation: only the catalog package is passed to
// npm in the managed runtime environment.
func (u *UseCases) UninstallAgent(ctx context.Context, agentID string, listeners ...process.OutputListener) (AgentUninstallResult, error) {
	if u == nil {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Agent uninstall request was cancelled"); err != nil {
		return AgentUninstallResult{}, err
	}
	agentID = strings.TrimSpace(agentID)
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return AgentUninstallResult{}, err
	}
	agent, ok := manifest.Agents[agentID]
	if !ok || agent.Package == nil || agent.Package.Manager != "npm" {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InvalidRequest, "Agent is not npm-managed: "+agentID)
	}

	unlockTask := u.lockTask("agent-task:" + agentID)
	defer unlockTask()
	var output process.OutputListener
	if len(listeners) > 0 && listeners[0] != nil {
		base := listeners[0]
		output = func(event process.Output) { event.Agent = agentID; base(event) }
	}
	runtime := u.installRuntime(output)
	npm, present := runtime.Runner.LookPath("npm")
	if !present || npm == "" {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.PrerequisiteMissing, "npm is required to uninstall "+agent.Name)
	}
	if agent.Command != "" {
		if executable, installed := runtime.Runner.LookPath(agent.Command); !installed || executable == "" {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.PrerequisiteMissing, agent.Name+" is not installed")
		}
	}

	args := []string{npm, "uninstall", "-g", agent.Package.Name}
	result, err := runtime.Run(ctx, args, install.NPMEnvironment(runtime, npm, ""), install.DefaultCommandTimeout)
	if err != nil {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InternalError, "Unable to uninstall "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if result.ExitCode != 0 {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentInstallFailed, fmt.Sprintf("Unable to uninstall %s: command exited with code %d", agent.Name, result.ExitCode), oneerrors.WithRetryable(true))
	}
	return AgentUninstallResult{Agent: agentID, Package: agent.Package.Name, Command: strings.Join(args, " ")}, nil
}
