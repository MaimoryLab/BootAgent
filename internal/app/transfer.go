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
	"sort"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/mcp"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
	"github.com/MaimoryLab/BootAgent/internal/skill"
	"github.com/MaimoryLab/BootAgent/internal/transfer"
)

type transferFileSnapshot struct {
	root  string
	files map[string]struct {
		data []byte
		mode os.FileMode
	}
	present bool
}

func snapshotTree(root string) (transferFileSnapshot, error) {
	s := transferFileSnapshot{root: root, files: map[string]struct {
		data []byte
		mode os.FileMode
	}{}}
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.present = true
	if !info.IsDir() {
		data, err := os.ReadFile(root)
		if err != nil {
			return s, err
		}
		s.files["."] = struct {
			data []byte
			mode os.FileMode
		}{data: data, mode: info.Mode()}
		return s, nil
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot snapshot non-regular file %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		s.files[rel] = struct {
			data []byte
			mode os.FileMode
		}{data: data, mode: info.Mode()}
		return nil
	})
	return s, err
}

func (s transferFileSnapshot) restore() error {
	if s.root == "" {
		return nil
	}
	if err := os.RemoveAll(s.root); err != nil {
		return err
	}
	if !s.present {
		return nil
	}
	if len(s.files) == 1 {
		if file, ok := s.files["."]; ok {
			if err := os.MkdirAll(filepath.Dir(s.root), 0o700); err != nil {
				return err
			}
			return os.WriteFile(s.root, file.data, file.mode.Perm())
		}
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	paths := make([]string, 0, len(s.files))
	for rel := range s.files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		file := s.files[rel]
		path := filepath.Join(s.root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, file.data, file.mode.Perm()); err != nil {
			return err
		}
	}
	return nil
}

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
	return u.ImportTransferV2WithOptions(ctx, data, transfer.ApplyOptions{ConflictPolicy: "overwrite"})
}

// ImportTransferV2WithOptions applies a validated transfer with an explicit
// Skill conflict policy. Keeping the old method preserves older bindings.
func (u *UseCases) ImportTransferV2WithOptions(ctx context.Context, data []byte, options transfer.ApplyOptions) error {
	if options.ConflictPolicy == "" {
		options.ConflictPolicy = "overwrite"
	}
	if options.ConflictPolicy != "overwrite" && options.ConflictPolicy != "skip" {
		return fmt.Errorf("unsupported Skill conflict policy %q", options.ConflictPolicy)
	}
	if err := contextError(ctx, "transfer import was cancelled"); err != nil {
		return err
	}
	preview, pkg, err := transfer.PreviewPackage(data)
	if err != nil {
		return err
	}
	if len(preview.SkillConflicts) > 0 {
		return fmt.Errorf("transfer contains duplicate Skill IDs: %s", strings.Join(preview.SkillConflicts, ", "))
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	store := u.skillStore()
	before, err := store.Load()
	if err != nil {
		return err
	}
	// Keep the rollback registry immutable; the working registry below is updated
	// as each Skill is imported.
	beforeBytes, err := json.Marshal(before)
	if err != nil {
		return err
	}
	var rollbackRegistry skill.Registry
	if err := json.Unmarshal(beforeBytes, &rollbackRegistry); err != nil {
		return err
	}
	providerSnapshot, err := snapshotTree(u.providers.Path())
	if err != nil {
		return err
	}
	mcpSnapshot, err := snapshotTree(u.mcpStore().Path())
	if err != nil {
		return err
	}
	profileSnapshot, err := snapshotTree(u.profiles.ProfilesPath())
	if err != nil {
		return err
	}
	created := make([]string, 0)
	rollback := func() error {
		for _, p := range created {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
		rollbackCtx := context.Background()
		if err := store.Save(rollbackCtx, rollbackRegistry); err != nil {
			return err
		}
		if err := providerSnapshot.restore(); err != nil {
			return err
		}
		if err := mcpSnapshot.restore(); err != nil {
			return err
		}
		if err := profileSnapshot.restore(); err != nil {
			return err
		}
		return nil
	}
	fail := func(cause error) error {
		if rollbackErr := rollback(); rollbackErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", cause, rollbackErr)
		}
		return cause
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
			return fail(errors.New("invalid config section"))
		}
		var providers struct {
			Providers []provider.Entry `json:"providers"`
		}
		if raw := config["providers"]; len(raw) > 0 {
			if e := json.Unmarshal(raw, &providers); e != nil {
				return fail(errors.New("invalid providers section"))
			}
			for _, entry := range providers.Providers {
				existing, _ := u.providers.Get(entry.ID)
				if entry.APIKey == "" {
					entry.APIKey = existing.APIKey
				}
				if _, e := u.providers.Save(ctx, entry); e != nil {
					return fail(e)
				}
			}
		}
		var profiles []ProfileSummary
		if raw := config["profiles"]; len(raw) > 0 {
			if e := json.Unmarshal(raw, &profiles); e != nil {
				return fail(errors.New("invalid profiles section"))
			}
			for _, p := range profiles {
				model := ""
				if p.Model != nil {
					model = *p.Model
				}
				if _, e := u.profiles.Save(ctx, profileStore.SaveRequest{ID: p.ID, Label: p.Label, Provider: p.Provider, Model: model, ConfigMode: "provider", Protocol: p.Protocol, ProviderKeyAvailable: true}); e != nil {
					return fail(e)
				}
			}
		}
		if raw := config["mcp"]; len(raw) > 0 {
			imported, e := mcp.Import(raw, "")
			if e != nil {
				return fail(e)
			}
			current, e := u.mcpStore().Load()
			if e != nil {
				return fail(e)
			}
			for id, fact := range imported.Servers {
				current.Servers[id] = fact
			}
			if e = u.mcpStore().Save(ctx, current); e != nil {
				return fail(e)
			}
		}
	}
	for name, raw := range pkg.Files {
		if filepath.Base(filepath.Dir(name)) != "skills" || filepath.Ext(name) != ".zip" {
			continue
		}
		stage, e := os.MkdirTemp("", "bootagent-skill-import-")
		if e != nil {
			return fail(e)
		}
		manifest, e := skill.ExtractArchive(ctx, raw, stage)
		if e != nil {
			_ = os.RemoveAll(stage)
			return fail(fmt.Errorf("invalid Skill %q: %w", name, e))
		}
		stats, e := skill.HashTree(ctx, stage)
		if e != nil {
			_ = os.RemoveAll(stage)
			return fail(e)
		}
		if stats.Hash != manifest.Variant.Hash {
			_ = os.RemoveAll(stage)
			return fail(fmt.Errorf("Skill %q hash does not match manifest", manifest.ID))
		}
		if options.ConflictPolicy == "skip" {
			if _, exists := before.Skills[manifest.ID]; exists {
				_ = os.RemoveAll(stage)
				continue
			}
		}
		variantPath := store.VariantPath(manifest.ID, stats.Hash)
		_, existed := os.Stat(variantPath)
		if e = store.SaveVariant(ctx, manifest.ID, stage, stats); e != nil {
			_ = os.RemoveAll(stage)
			return fail(e)
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
		return fail(err)
	}
	return nil
}

// PreviewTransferV2 enriches the package validation with conflicts against the
// current central Skill library, while remaining read-only.
func (u *UseCases) PreviewTransferV2(ctx context.Context, data []byte) (transfer.Preview, error) {
	preview, pkg, err := transfer.PreviewPackage(data)
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
		if _, exists := registry.Skills[id]; !exists {
			preview.SkillNew = appendTransferConflictUnique(preview.SkillNew, id)
		}
	}
	configRaw := pkg.Files["config.json"]
	if len(configRaw) == 0 {
		legacy := map[string]json.RawMessage{}
		for _, name := range []string{"providers.json", "profiles.json", "mcp.json"} {
			if raw := pkg.Files[name]; len(raw) > 0 {
				legacy[strings.TrimSuffix(name, ".json")] = raw
			}
		}
		configRaw, _ = json.Marshal(legacy)
	}
	if len(configRaw) > 0 {
		var config map[string]json.RawMessage
		if err := json.Unmarshal(configRaw, &config); err != nil {
			return transfer.Preview{}, errors.New("invalid config section")
		}
		if raw := config["providers"]; len(raw) > 0 {
			var incoming struct {
				Providers []provider.Entry `json:"providers"`
			}
			if err := json.Unmarshal(raw, &incoming); err != nil {
				return transfer.Preview{}, errors.New("invalid providers section")
			}
			for _, entry := range incoming.Providers {
				if _, e := u.providers.Get(entry.ID); e == nil {
					preview.ProviderConflicts = appendTransferConflictUnique(preview.ProviderConflicts, entry.ID)
				} else {
					preview.ProviderNew = appendTransferConflictUnique(preview.ProviderNew, entry.ID)
				}
			}
		}
		if raw := config["profiles"]; len(raw) > 0 {
			var incoming []ProfileSummary
			if err := json.Unmarshal(raw, &incoming); err != nil {
				return transfer.Preview{}, errors.New("invalid profiles section")
			}
			existing, err := u.profiles.List()
			if err != nil {
				return transfer.Preview{}, err
			}
			seen := make(map[string]bool, len(existing))
			for _, p := range existing {
				seen[p.ID] = true
			}
			for _, p := range incoming {
				if seen[p.ID] {
					preview.ProfileConflicts = appendTransferConflictUnique(preview.ProfileConflicts, p.ID)
				} else {
					preview.ProfileNew = appendTransferConflictUnique(preview.ProfileNew, p.ID)
				}
			}
		}
		if raw := config["mcp"]; len(raw) > 0 {
			incoming, err := mcp.Import(raw, "")
			if err == nil {
				current, loadErr := u.mcpStore().Load()
				if loadErr != nil {
					return transfer.Preview{}, loadErr
				}
				for id := range incoming.Servers {
					if _, exists := current.Servers[id]; exists {
						preview.MCPConflicts = appendTransferConflictUnique(preview.MCPConflicts, id)
					} else {
						preview.MCPNew = appendTransferConflictUnique(preview.MCPNew, id)
					}
				}
			}
		}
	}
	slices.Sort(preview.ProviderConflicts)
	slices.Sort(preview.ProfileConflicts)
	slices.Sort(preview.MCPConflicts)
	slices.Sort(preview.ProviderNew)
	slices.Sort(preview.ProfileNew)
	slices.Sort(preview.MCPNew)
	slices.Sort(preview.SkillNew)
	slices.Sort(preview.SkillConflicts)
	return preview, nil
}

func appendTransferConflictUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendTransferUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}
