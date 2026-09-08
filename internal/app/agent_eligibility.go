package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/platform"
)

// agentCommandAvailable accepts either the normal command lookup or an
// installation discovered through a version manager/package prefix. The latter
// is important for agents installed outside the environment inherited by the
// desktop process.
func (u *UseCases) agentCommandAvailable(ctx context.Context, id string, agent catalog.Agent) bool {
	lookup := u.status.Lookup
	if lookup == nil && u.runner != nil {
		lookup = u.runner.LookPath
	}
	if lookup != nil {
		if _, ok := lookup(agent.Command); ok {
			return true
		}
	}
	return len(u.discoverAgentInstallations(ctx, id, agent)) > 0
}

func safeUserPath(home, target string) bool {
	if home == "" || target == "" {
		return false
	}
	homeAbs, err := filepath.Abs(filepath.Clean(home))
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(homeAbs, targetAbs)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validAgentPlatform(agent catalog.Agent, osID string) bool {
	osID = platform.For(osID, "").OS
	return contains(agent.Platforms, osID)
}

func (u *UseCases) eligibleAgentBase(ctx context.Context, id string, agent catalog.Agent) bool {
	return agent.Command != "" && agent.Package != nil && validAgentPlatform(agent, u.status.Platform.OS) && u.agentCommandAvailable(ctx, id, agent)
}

// eligibleSkillAgent applies the manifest-backed Skills capability contract.
// A missing root is valid: it will be created only after the write path has
// performed its normal securefs checks.
func (u *UseCases) eligibleSkillAgent(ctx context.Context, id string, agent catalog.Agent) bool {
	if !u.eligibleAgentBase(ctx, id, agent) {
		return false
	}
	root := skillPath(u.status.Home, u.status.Platform.OS, agent)
	if root == "" || !safeUserPath(u.status.Home, root) || hasSymlinkComponent(u.status.Home, root) {
		return false
	}
	if info, err := os.Lstat(root); err == nil {
		return info.IsDir()
	} else if !os.IsNotExist(err) {
		return false
	}
	return true
}

// eligibleMCPAgent applies the manifest plus registered-adapter contract. MCP
// config parents may be absent when they are safely creatable below the user
// home; this allows first-time application on a newly installed Agent.
func (u *UseCases) eligibleMCPAgent(ctx context.Context, id string, agent catalog.Agent) bool {
	if !u.eligibleAgentBase(ctx, id, agent) || agent.MCPAdapter == "" || agent.MCPSection == "" || mcpAdapter(agent) == nil {
		return false
	}
	path := mcpPath(u.status.Home, u.status.Platform.OS, agent)
	if path == "" || !safeUserPath(u.status.Home, path) || hasSymlinkComponent(u.status.Home, path) {
		return false
	}
	parent := filepath.Dir(path)
	if info, err := os.Stat(parent); err == nil {
		return info.IsDir()
	} else if os.IsNotExist(err) {
		return true
	}
	return false
}
