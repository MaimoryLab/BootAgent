package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
)

var (
	ErrAgentNotFound             = errors.New("agent not found")
	ErrAgentInstallationNotFound = errors.New("agent installation not found")
)

// AgentUninstallPreview describes the concrete installation selected for
// removal. User configuration and credential paths are intentionally excluded.
type AgentUninstallPreview struct {
	Agent         string            `json:"agent"`
	Installation  AgentInstallation `json:"installation"`
	Files         []string          `json:"files"`
	PreservedData []string          `json:"preservedData"`
}

func (u *UseCases) PreviewAgentUninstall(ctx context.Context, agentID, installationID string) (AgentUninstallPreview, error) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return AgentUninstallPreview{}, err
	}
	agent, ok := manifest.Agents[agentID]
	if !ok {
		return AgentUninstallPreview{}, ErrAgentNotFound
	}
	for _, installation := range u.discoverAgentInstallations(ctx, agentID, agent) {
		if installation.ID != installationID {
			continue
		}
		files := []string{installation.Executable}
		if installation.Manager == "official-script" {
			files = append(files, filepath.Join(u.status.Home, ".kimi-code", "updates"))
		}
		return AgentUninstallPreview{Agent: agentID, Installation: installation, Files: files, PreservedData: []string{agent.ConfigPath, "credentials", "sessions"}}, nil
	}
	return AgentUninstallPreview{}, ErrAgentInstallationNotFound
}
