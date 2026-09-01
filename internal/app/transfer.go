package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/MaimoryLab/BootAgent/internal/mcp"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
	"github.com/MaimoryLab/BootAgent/internal/skill"
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
	config := map[string]json.RawMessage{"providers": providersJSON, "profiles": profilesJSON}
	if len(mcpIDs) > 0 {
		data, err := u.ExportMCP(ctx, mcp.ExportOptions{Mode: mcp.SecretOmit, ServerIDs: mcpIDs})
		if err != nil {
			return nil, err
		}
		config["mcp"] = data
	}
	skillFiles := map[string][]byte{}
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
		skillFiles[id+".skill.zip"] = data
	}
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	files["config.json"] = configJSON
	if len(skillFiles) > 0 {
		files["skills.zip"], err = buildSkillArchive(skillFiles)
		if err != nil {
			return nil, err
		}
	}
	return transfer.Build(files)
}

func buildSkillArchive(files map[string][]byte) ([]byte, error) {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ImportTransferV2 imports Skill archives into BootAgent's managed library.
// It deliberately does not publish to any Agent directory; publication remains
// an explicit action in the Skills management page. Registry and newly-created
// variants are rolled back together if any archive fails.
func (u *UseCases) ImportTransferV2(ctx context.Context, data []byte) error {
	if err := contextError(ctx, "transfer import was cancelled"); err != nil {
		return err
	}
	_, pkg, err := transfer.PreviewPackage(data)
	if err != nil {
		return err
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	store := u.skillStore()
	before, err := store.Load()
	if err != nil {
		return err
	}
	created := make([]string, 0)
	rollback := func() {
		for _, p := range created {
			_ = os.RemoveAll(p)
		}
		_ = store.Save(ctx, before)
	}
	configRaw := pkg.Files["config.json"]
	if len(configRaw) == 0 {
		legacy := map[string]json.RawMessage{}
		if raw := pkg.Files["providers.json"]; len(raw) > 0 {
			legacy["providers"] = raw
		}
		if raw := pkg.Files["profiles.json"]; len(raw) > 0 {
			legacy["profiles"] = raw
		}
		if raw := pkg.Files["mcp.json"]; len(raw) > 0 {
			legacy["mcp"] = raw
		}
		configRaw, _ = json.Marshal(legacy)
	}
	if len(configRaw) > 0 {
		var config map[string]json.RawMessage
		if e := json.Unmarshal(configRaw, &config); e != nil {
			rollback()
			return errors.New("invalid config section")
		}
		var providers struct {
			Providers []provider.Entry `json:"providers"`
		}
		if raw := config["providers"]; len(raw) > 0 {
			if e := json.Unmarshal(raw, &providers); e != nil {
				rollback()
				return errors.New("invalid providers section")
			}
			for _, entry := range providers.Providers {
				existing, _ := u.providers.Get(entry.ID)
				if entry.APIKey == "" {
					entry.APIKey = existing.APIKey
				}
				if _, e := u.providers.Save(ctx, entry); e != nil {
					rollback()
					return e
				}
			}
		}
		var profiles []ProfileSummary
		if raw := config["profiles"]; len(raw) > 0 {
			if e := json.Unmarshal(raw, &profiles); e != nil {
				rollback()
				return errors.New("invalid profiles section")
			}
			for _, p := range profiles {
				model := ""
				if p.Model != nil {
					model = *p.Model
				}
				if _, e := u.profiles.Save(ctx, profileStore.SaveRequest{ID: p.ID, Label: p.Label, Provider: p.Provider, Model: model, ConfigMode: "provider", Protocol: p.Protocol, ProviderKeyAvailable: true}); e != nil {
					rollback()
					return e
				}
			}
		}
		if raw := config["mcp"]; len(raw) > 0 {
			imported, e := mcp.Import(raw, "")
			if e != nil {
				rollback()
				return e
			}
			current, e := u.mcpStore().Load()
			if e != nil {
				rollback()
				return e
			}
			for id, fact := range imported.Servers {
				current.Servers[id] = fact
			}
			if e = u.mcpStore().Save(ctx, current); e != nil {
				rollback()
				return e
			}
		}
	}
	for name, raw := range pkg.Files {
		if filepath.Base(filepath.Dir(name)) != "skills" || filepath.Ext(name) != ".zip" {
			continue
		}
		stage, e := os.MkdirTemp("", "bootagent-skill-import-")
		if e != nil {
			rollback()
			return e
		}
		manifest, e := skill.ExtractArchive(ctx, raw, stage)
		if e != nil {
			_ = os.RemoveAll(stage)
			rollback()
			return fmt.Errorf("invalid Skill %q: %w", name, e)
		}
		stats, e := skill.HashTree(ctx, stage)
		if e != nil {
			_ = os.RemoveAll(stage)
			rollback()
			return e
		}
		if stats.Hash != manifest.Variant.Hash {
			_ = os.RemoveAll(stage)
			rollback()
			return fmt.Errorf("Skill %q hash does not match manifest", manifest.ID)
		}
		variantPath := store.VariantPath(manifest.ID, stats.Hash)
		_, existed := os.Stat(variantPath)
		if e = store.SaveVariant(ctx, manifest.ID, stage, stats); e != nil {
			_ = os.RemoveAll(stage)
			rollback()
			return e
		}
		if os.IsNotExist(existed) {
			created = append(created, variantPath)
		}
		fact := before.Skills[manifest.ID]
		fact.Name, fact.Description = manifest.Name, manifest.Description
		idx := -1
		for i := range fact.Variants {
			if fact.Variants[i].Hash == stats.Hash {
				idx = i
				break
			}
		}
		if idx < 0 {
			fact.Variants = append(fact.Variants, skill.Variant{Hash: stats.Hash})
			idx = len(fact.Variants) - 1
		}
		fact.Variants[idx].Stored = true
		fact.Variants[idx].ImportSources = appendTransferUnique(fact.Variants[idx].ImportSources, "zip")
		before.Skills[manifest.ID] = fact
		_ = os.RemoveAll(stage)
	}
	if err := store.Save(ctx, before); err != nil {
		rollback()
		return err
	}
	return nil
}

// PreviewTransferV2 enriches the package validation with conflicts against the
// current central Skill library, while remaining read-only.
func (u *UseCases) PreviewTransferV2(ctx context.Context, data []byte) (transfer.Preview, error) {
	preview, _, err := transfer.PreviewPackage(data)
	if err != nil {
		return transfer.Preview{}, err
	}
	registry, err := u.skillStore().Load()
	if err != nil {
		return transfer.Preview{}, err
	}
	for _, id := range preview.Skills {
		if _, exists := registry.Skills[id]; exists && !slices.Contains(preview.SkillConflicts, id) {
			preview.SkillConflicts = append(preview.SkillConflicts, id)
		}
	}
	slices.Sort(preview.SkillConflicts)
	return preview, nil
}

func appendTransferUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}
