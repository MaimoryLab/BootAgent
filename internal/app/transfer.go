package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MaimoryLab/BootAgent/internal/mcp"
	"github.com/MaimoryLab/BootAgent/internal/transfer"
)

// ExportTransferV2 creates a portable, secret-free bundle. Skill archives are
// kept as nested ZIPs so the common container remains deterministic and the
// existing Skill ZIP importer can consume them without a second content model.
func (u *UseCases) ExportTransferV2(ctx context.Context, providerIDs, profileIDs, mcpIDs, skillIDs []string) ([]byte, error) {
	if err := contextError(ctx, "transfer export was cancelled"); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	providers := make([]any, 0, len(providerIDs))
	for _, id := range providerIDs {
		entry, err := u.GetProvider(ctx, id)
		if err != nil {
			return nil, err
		}
		entry.APIKey = ""
		providers = append(providers, entry)
	}
	profiles, err := u.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	selectedProfiles := make([]ProfileSummary, 0, len(profileIDs))
	allowed := make(map[string]bool, len(profileIDs))
	for _, id := range profileIDs {
		allowed[id] = true
	}
	for _, profile := range profiles {
		if allowed[profile.ID] {
			selectedProfiles = append(selectedProfiles, profile)
		}
	}
	providersJSON, err := json.MarshalIndent(map[string]any{"providers": providers}, "", "  ")
	if err != nil {
		return nil, err
	}
	profilesJSON, err := json.MarshalIndent(selectedProfiles, "", "  ")
	if err != nil {
		return nil, err
	}
	files["providers.json"], files["profiles.json"] = providersJSON, profilesJSON
	if len(mcpIDs) > 0 {
		data, err := u.ExportMCP(ctx, mcp.ExportOptions{Mode: mcp.SecretOmit, ServerIDs: mcpIDs})
		if err != nil {
			return nil, err
		}
		files["mcp.json"] = data
	}
	for _, id := range skillIDs {
		summary, err := u.GetSkill(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(summary.VariantHashes) == 0 {
			return nil, fmt.Errorf("Skill %q has no stored variant", id)
		}
		data, err := u.ExportSkill(ctx, id, summary.VariantHashes[0])
		if err != nil {
			return nil, err
		}
		files["skills/"+id+".skill.zip"] = data
	}
	return transfer.Build(files)
}
