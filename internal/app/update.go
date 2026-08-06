package app

import (
	"context"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

type AgentUpdateResult struct {
	Agent   string `json:"agent"`
	Package string `json:"package"`
	Command string `json:"command"`
}

// UpdateAgent updates an npm-managed Agent through the same managed runtime as installs.
func (u *UseCases) UpdateAgent(ctx context.Context, agentID string) (AgentUpdateResult, error) {
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
	runtime := u.installRuntime(nil)
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
	if _, err := runtime.Runner.Run(ctx, args, runtime.Env, 180*time.Second); err != nil {
		return AgentUpdateResult{}, oneerrors.New(oneerrors.InternalError, "Unable to update "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return AgentUpdateResult{Agent: agentID, Package: agent.Package.Name, Command: strings.Join(args, " ")}, nil
}
