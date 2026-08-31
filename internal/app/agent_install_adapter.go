package app

import (
	"context"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
)

// AgentInstallAdapter is the common contract for discovering and safely
// removing installations created outside BootAgent.
type AgentInstallAdapter interface {
	Discover(context.Context, catalog.Agent) []AgentInstallation
	VerifyOwnership(context.Context, AgentInstallation) (bool, string)
	Uninstall(context.Context, catalog.Agent, AgentInstallation) (AgentUninstallResult, error)
	VerifyRemoved(context.Context, AgentInstallation) (bool, error)
}

// pathInstallAdapter is the initial cross-platform adapter. The existing
// source-specific uninstall implementations remain the compatibility layer;
// this adapter provides one stable boundary for future managers.
type pathInstallAdapter struct {
	useCases *UseCases
	agentID  string
}

func (a pathInstallAdapter) Discover(ctx context.Context, agent catalog.Agent) []AgentInstallation {
	if a.useCases == nil {
		return nil
	}
	return a.useCases.discoverAgentInstallations(ctx, a.agentID, agent)
}

func (a pathInstallAdapter) VerifyOwnership(_ context.Context, installation AgentInstallation) (bool, string) {
	if installation.CanUninstall {
		return true, ""
	}
	return false, installation.Reason
}

func (a pathInstallAdapter) Uninstall(ctx context.Context, agent catalog.Agent, installation AgentInstallation) (AgentUninstallResult, error) {
	if a.useCases == nil {
		return AgentUninstallResult{}, nil
	}
	return a.useCases.UninstallAgentWithOptions(ctx, a.agentID, AgentUninstallOptions{InstallationID: installation.ID})
}

func (a pathInstallAdapter) VerifyRemoved(_ context.Context, installation AgentInstallation) (bool, error) {
	return !fileExists(installation.Executable), nil
}

var _ AgentInstallAdapter = pathInstallAdapter{}
