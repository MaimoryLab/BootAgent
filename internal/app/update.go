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

type AgentUpdateResult struct {
	Agent   string `json:"agent"`
	Package string `json:"package"`
	Command string `json:"command"`
}

// UpdateAgent updates an npm-managed Agent through the same managed runtime as installs.
func (u *UseCases) UpdateAgent(ctx context.Context, agentID string, listeners ...process.OutputListener) (AgentUpdateResult, error) {
	if u == nil {
		return AgentUpdateResult{}, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Agent update request was cancelled"); err != nil {
		return AgentUpdateResult{}, err
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return AgentUpdateResult{}, err
	}
	agent, ok := manifest.Agents[strings.TrimSpace(agentID)]
	if !ok || agent.Package == nil || agent.Package.Manager != "npm" {
		return AgentUpdateResult{}, oneerrors.New(oneerrors.InvalidRequest, "Agent is not npm-managed: "+agentID)
	}
	unlockTask := u.lockTask("agent-task:" + strings.TrimSpace(agentID))
	defer unlockTask()
	var output process.OutputListener
	if len(listeners) > 0 && listeners[0] != nil {
		base := listeners[0]
		output = func(event process.Output) { event.Agent = agentID; base(event) }
	}
	if output != nil {
		output(process.Output{Kind: "phase", Agent: agentID, Phase: "preparing"})
	}
	runtime := u.installRuntime(output)
	npm, present := runtime.Runner.LookPath("npm")
	if !present || npm == "" {
		return AgentUpdateResult{}, oneerrors.New(oneerrors.PrerequisiteMissing, "npm is required to update "+agent.Name)
	}
	// `npm update -g` on a package that was never installed exits 0 and does
	// nothing, so without this the task centre reports "update complete" for an
	// Agent that is still missing -- worse than an error, because it looks like it
	// worked. The UI hides the button in this case; this covers the CLI and a
	// restored task card, which do not go through it.
	if agent.Command != "" {
		if executable, installed := runtime.Runner.LookPath(agent.Command); !installed || executable == "" {
			return AgentUpdateResult{}, oneerrors.New(oneerrors.PrerequisiteMissing, agent.Name+" is not installed yet; install it before updating")
		}
	}
	args := []string{npm, "update", "-g", agent.Package.Name}
	if output != nil {
		output(process.Output{Kind: "phase", Agent: agentID, Phase: "installing"})
	}
	registry := u.packageRegistry(ctx, "")
	if registry != "" {
		registry, err = install.ResolveRegistry(registry)
		if err != nil {
			return AgentUpdateResult{}, err
		}
		args = append(args, "--registry="+registry)
	}
	result, err := runtime.Run(ctx, args, install.NPMEnvironment(runtime, npm, registry), install.DefaultCommandTimeout)
	if err != nil {
		return AgentUpdateResult{}, oneerrors.New(oneerrors.InternalError, "Unable to update "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if result.ExitCode != 0 {
		return AgentUpdateResult{}, oneerrors.New(oneerrors.AgentInstallFailed, fmt.Sprintf("Unable to update %s: command exited with code %d", agent.Name, result.ExitCode), oneerrors.WithRetryable(true))
	}
	if output != nil {
		output(process.Output{Kind: "phase", Agent: agentID, Phase: "completed"})
	}
	return AgentUpdateResult{Agent: agentID, Package: agent.Package.Name, Command: strings.Join(args, " ")}, nil
}
