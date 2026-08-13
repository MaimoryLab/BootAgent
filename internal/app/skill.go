package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/skill"
)

type SkillSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Variants    int      `json:"variants"`
	Agents      []string `json:"agents"`
	Conflict    bool     `json:"conflict"`
}
type SkillCandidate struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Hash           string   `json:"hash"`
	Source         string   `json:"source"`
	Files          int      `json:"files"`
	Bytes          int64    `json:"bytes"`
	Diagnostic     string   `json:"diagnostic,omitempty"`
	Stored         bool     `json:"stored"`
	ObservedAgents []string `json:"observed_agents,omitempty"`
}
type SkillScanResult struct {
	Skills         []SkillSummary   `json:"skills"`
	Candidates     []SkillCandidate `json:"candidates"`
	EligibleAgents []string         `json:"eligible_agents"`
	PreviewToken   string           `json:"preview_token,omitempty"`
	Diagnostics    []string         `json:"diagnostics,omitempty"`
}
type SkillImportRequest struct {
	Source string `json:"source"`
}
type SkillImportPreview struct {
	Token      string           `json:"token"`
	Candidates []SkillCandidate `json:"candidates"`
}
type SkillChange struct {
	ID           string   `json:"id"`
	VariantHash  string   `json:"variant_hash"`
	Targets      []string `json:"targets"`
	Delete       bool     `json:"delete,omitempty"`
	ImportSource string   `json:"import_source,omitempty"`
}
type SkillApplyRequest struct {
	PreviewToken string        `json:"preview_token,omitempty"`
	Changes      []SkillChange `json:"changes"`
}
type SkillRestoreRequest struct {
	BackupID    string   `json:"backup_id"`
	VariantHash string   `json:"variant_hash"`
	Targets     []string `json:"targets"`
}
type SkillAgentApplyResult struct {
	Agent           string `json:"agent"`
	TargetUpdated   bool   `json:"target_updated"`
	RegistryUpdated bool   `json:"registry_updated"`
	Error           string `json:"error,omitempty"`
}
type SkillApplyResult struct {
	Results []SkillAgentApplyResult `json:"results"`
}
type SkillUninstallResult struct {
	BackupID        string `json:"backup_id,omitempty"`
	RegistryUpdated bool   `json:"registry_updated"`
	Error           string `json:"error,omitempty"`
}
type SkillBackupSummary struct {
	ID        string `json:"id"`
	BackupID  string `json:"backup_id"`
	CreatedAt string `json:"created_at"`
	Variants  int    `json:"variants"`
}

type skillPreview struct {
	expires    time.Time
	source     string
	candidates []skill.Candidate
}

type skillDraftState struct {
	dirty  atomic.Bool
	locale atomic.Value
}

func (u *UseCases) skillStore() skill.Store { return skill.NewStore(u.status.Home, u.filesystem) }

func skillPath(home, osID string, agent catalog.Agent) string {
	osID = platform.For(osID, "").OS
	rel := agent.SkillsPath
	if osID == "windows" && agent.SkillsWindowsPath != "" {
		rel = agent.SkillsWindowsPath
	}
	if rel == "" {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(rel))
}

func (u *UseCases) eligibleSkillAgents() (map[string]catalog.Agent, error) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return nil, err
	}
	lookup := u.status.Lookup
	if lookup == nil && u.runner != nil {
		lookup = u.runner.LookPath
	}
	osID := platform.For(u.status.Platform.OS, u.status.Platform.Arch).OS
	result := map[string]catalog.Agent{}
	for id, agent := range manifest.Agents {
		if agent.SkillsPath == "" || agent.Command == "" || lookup == nil || !contains(agent.Platforms, osID) {
			continue
		}
		if _, ok := lookup(agent.Command); !ok {
			continue
		}
		root := skillPath(u.status.Home, u.status.Platform.OS, agent)
		if info, statErr := os.Lstat(root); statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || hasSymlinkComponent(u.status.Home, root) {
				continue
			}
		} else if !os.IsNotExist(statErr) {
			continue
		}
		result[id] = agent
	}
	return result, nil
}

func (u *UseCases) ScanSkills(ctx context.Context) (SkillScanResult, error) {
	if err := contextError(ctx, "Skill scan was cancelled"); err != nil {
		return SkillScanResult{}, err
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	store := u.skillStore()
	registry, err := store.Load()
	if err != nil {
		return SkillScanResult{}, err
	}
	eligible, err := u.eligibleSkillAgents()
	if err != nil {
		return SkillScanResult{}, err
	}
	diagnostics := []string{}
	candidates := []SkillCandidate{}
	rawCandidates := []skill.Candidate{}
	for _, agentID := range skillAgentIDs(eligible) {
		agent := eligible[agentID]
		root := skillPath(u.status.Home, u.status.Platform.OS, agent)
		found, scanErr := skill.ScanAgentRoot(ctx, root, "agent")
		if scanErr != nil {
			if os.IsNotExist(scanErr) {
				continue
			}
			diagnostics = append(diagnostics, fmt.Sprintf("%s: Skill directory could not be read", agentID))
			continue
		}
		oldFacts := make(map[string]skill.Fact, len(registry.Skills))
		for id, fact := range registry.Skills {
			oldFacts[id] = fact
			for i := range fact.Variants {
				fact.Variants[i].ObservedAgents = removeString(fact.Variants[i].ObservedAgents, agentID)
				fact.Variants[i].ManagedTargets = removeString(fact.Variants[i].ManagedTargets, agentID)
			}
			registry.Skills[id] = fact
		}
		for _, candidate := range found {
			if candidate.Diagnostic != "" || skill.ValidateID(candidate.ID) != nil {
				if candidate.Diagnostic == "" {
					candidate.Diagnostic = "invalid Skill ID"
				}
				candidates = append(candidates, toCandidate(candidate, false))
				diagnostics = append(diagnostics, fmt.Sprintf("%s: %s", agentID, candidate.Diagnostic))
				continue
			}
			rawCandidates = append(rawCandidates, candidate)
			fact := registry.Skills[candidate.ID]
			fact.Name, fact.Description = candidate.Name, candidate.Description
			idx := variantIndex(fact.Variants, candidate.Hash)
			if idx < 0 {
				fact.Variants = append(fact.Variants, skill.Variant{Hash: candidate.Hash})
				idx = len(fact.Variants) - 1
			} else {
				fact.Variants[idx].ObservedAgents = addSorted(fact.Variants[idx].ObservedAgents, agentID)
			}
			fact.Variants[idx].ObservedAgents = addSorted(fact.Variants[idx].ObservedAgents, agentID)
			if old := oldFacts[candidate.ID]; old.Variants != nil {
				if oldIdx := variantIndex(old.Variants, candidate.Hash); oldIdx >= 0 && old.Variants[oldIdx].Stored {
					fact.Variants[idx].ManagedTargets = addSorted(old.Variants[oldIdx].ManagedTargets, agentID)
				}
			}
			registry.Skills[candidate.ID] = fact
			stored := fact.Variants[idx].Stored
			out := toCandidate(candidate, stored)
			out.ObservedAgents = []string{agentID}
			if !stored {
				candidates = append(candidates, out)
			}
		}
	}
	previewToken := ""
	if len(rawCandidates) > 0 {
		previewToken = u.storeSkillPreview("agent", rawCandidates)
	}
	if err := store.Save(ctx, registry); err != nil {
		return SkillScanResult{Candidates: candidates, Diagnostics: diagnostics, PreviewToken: previewToken}, err
	}
	return SkillScanResult{Skills: summarizeSkills(registry), Candidates: candidates, EligibleAgents: skillAgentIDs(eligible), PreviewToken: previewToken, Diagnostics: diagnostics}, nil
}

func (u *UseCases) ListSkills(ctx context.Context) ([]SkillSummary, error) {
	if err := contextError(ctx, "Skill request was cancelled"); err != nil {
		return nil, err
	}
	r, err := u.skillStore().Load()
	if err != nil {
		return nil, err
	}
	return summarizeSkills(r), nil
}

func (u *UseCases) GetSkill(ctx context.Context, id string) (SkillSummary, error) {
	if err := contextError(ctx, "Skill request was cancelled"); err != nil {
		return SkillSummary{}, err
	}
	if err := skill.ValidateID(id); err != nil {
		return SkillSummary{}, err
	}
	r, err := u.skillStore().Load()
	if err != nil {
		return SkillSummary{}, err
	}
	fact, ok := r.Skills[id]
	if !ok {
		return SkillSummary{}, errors.New("Skill not found")
	}
	return summarizeSkill(id, fact), nil
}

func (u *UseCases) PreviewSkillImport(ctx context.Context, req SkillImportRequest, selectedPath string) (SkillImportPreview, error) {
	if err := contextError(ctx, "Skill import was cancelled"); err != nil {
		return SkillImportPreview{}, err
	}
	if selectedPath == "" {
		return SkillImportPreview{}, errors.New("Skill import source is required")
	}
	var candidates []skill.Candidate
	var err error
	switch req.Source {
	case "agent":
		candidates, err = skill.ScanAgentRoot(ctx, selectedPath, "agent")
	case "folder":
		candidates, err = skill.DiscoverFolder(ctx, selectedPath)
	case "zip":
		stagingParent, stageErr := u.skillPreviewParent()
		if stageErr != nil {
			return SkillImportPreview{}, stageErr
		}
		candidates, err = skill.DiscoverZIP(ctx, selectedPath, stagingParent)
	default:
		return SkillImportPreview{}, errors.New("invalid Skill import source")
	}
	if err != nil {
		return SkillImportPreview{}, err
	}
	token := u.storeSkillPreview(req.Source, candidates)
	if token == "" {
		_ = skill.CleanupCandidates(candidates)
		return SkillImportPreview{}, errors.New("cannot create Skill import preview")
	}
	out := make([]SkillCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, toCandidate(candidate, false))
	}
	return SkillImportPreview{Token: token, Candidates: out}, nil
}

func (u *UseCases) ApplySkills(ctx context.Context, req SkillApplyRequest) SkillApplyResult {
	result := SkillApplyResult{Results: []SkillAgentApplyResult{}}
	if err := contextError(ctx, "Skill apply was cancelled"); err != nil {
		result.Results = append(result.Results, SkillAgentApplyResult{Error: err.Error()})
		return result
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	store := u.skillStore()
	registry, err := store.Load()
	if err != nil {
		result.Results = append(result.Results, SkillAgentApplyResult{Error: "Cannot read Skill Registry"})
		return result
	}
	eligible, err := u.eligibleSkillAgents()
	if err != nil {
		result.Results = append(result.Results, SkillAgentApplyResult{Error: err.Error()})
		return result
	}
	var preview skillPreview
	if req.PreviewToken != "" {
		preview, err = u.takeSkillPreview(req.PreviewToken)
		if err != nil {
			result.Results = append(result.Results, SkillAgentApplyResult{Error: err.Error()})
			return result
		}
	}
	for _, change := range req.Changes {
		if err := skill.ValidateID(change.ID); err != nil {
			result.Results = append(result.Results, SkillAgentApplyResult{Error: err.Error()})
			continue
		}
		if change.ImportSource != "" && change.ImportSource != "agent" && change.ImportSource != "folder" && change.ImportSource != "zip" {
			result.Results = append(result.Results, SkillAgentApplyResult{Error: "Invalid Skill import source"})
			continue
		}
		fact := registry.Skills[change.ID]
		variantIdx := variantIndex(fact.Variants, change.VariantHash)
		if change.Delete && variantIdx < 0 && change.VariantHash == "" {
			for i := range fact.Variants {
				if len(fact.Variants[i].ManagedTargets) > 0 {
					variantIdx = i
					change.VariantHash = fact.Variants[i].Hash
					break
				}
			}
		}
		var sourcePath string
		if preview.source != "" {
			for _, candidate := range preview.candidates {
				if candidate.ID == change.ID && candidate.Hash == change.VariantHash {
					sourcePath = candidate.Path
					break
				}
			}
			if sourcePath == "" {
				result.Results = append(result.Results, SkillAgentApplyResult{Error: "Skill preview candidate not found"})
				continue
			}
			stats, hashErr := skill.HashTree(ctx, sourcePath)
			if hashErr != nil || stats.Hash != change.VariantHash {
				result.Results = append(result.Results, SkillAgentApplyResult{Error: "Skill source changed after preview"})
				continue
			}
		}
		if sourcePath != "" && (variantIdx < 0 || !fact.Variants[variantIdx].Stored) {
			stats, _ := skill.HashTree(ctx, sourcePath)
			if err := store.SaveVariant(ctx, change.ID, sourcePath, stats); err != nil {
				result.Results = append(result.Results, SkillAgentApplyResult{Error: "Cannot store Skill variant"})
				continue
			}
			if variantIdx < 0 {
				fact.Variants = append(fact.Variants, skill.Variant{Hash: change.VariantHash})
				variantIdx = len(fact.Variants) - 1
			}
			fact.Name, fact.Description = previewCandidateMeta(preview.candidates, change.ID, fact.Name, fact.Description)
			fact.Variants[variantIdx].Stored = true
			if change.ImportSource != "" && change.ImportSource != "agent" && change.ImportSource != "folder" && change.ImportSource != "zip" {
				result.Results = append(result.Results, SkillAgentApplyResult{Error: "Invalid Skill import source"})
				continue
			}
			fact.Variants[variantIdx].ImportSources = addSorted(fact.Variants[variantIdx].ImportSources, change.ImportSource)
			registry.Skills[change.ID] = fact
			if err := store.Save(ctx, registry); err != nil {
				result.Results = append(result.Results, SkillAgentApplyResult{Error: "Cannot update Skill Registry"})
				continue
			}
		}
		targetIDs := append([]string(nil), change.Targets...)
		if change.Delete && len(targetIDs) == 0 && variantIdx >= 0 {
			targetIDs = append(targetIDs, fact.Variants[variantIdx].ManagedTargets...)
		}
		if change.Delete && len(targetIDs) == 0 {
			result.Results = append(result.Results, SkillAgentApplyResult{Error: "Skill target is not managed"})
			continue
		}
		for _, agentID := range targetIDs {
			item := SkillAgentApplyResult{Agent: agentID}
			agent, ok := eligible[agentID]
			if !ok {
				item.Error = "Agent is not eligible"
				result.Results = append(result.Results, item)
				continue
			}
			target := filepath.Join(skillPath(u.status.Home, u.status.Platform.OS, agent), change.ID)
			if hasSymlinkComponent(u.status.Home, filepath.Dir(target)) {
				item.Error = "Skill target path is not private"
				result.Results = append(result.Results, item)
				continue
			}
			if change.Delete {
				if variantIdx < 0 || !contains(fact.Variants[variantIdx].ManagedTargets, agentID) {
					item.Error = "Skill target is not managed"
					result.Results = append(result.Results, item)
					continue
				}
				if infoErr := removeManagedSkill(ctx, target, change.VariantHash); infoErr != nil {
					item.Error = infoErr.Error()
					result.Results = append(result.Results, item)
					continue
				}
				fact.Variants[variantIdx].ManagedTargets = removeString(fact.Variants[variantIdx].ManagedTargets, agentID)
			} else {
				if variantIdx < 0 || !fact.Variants[variantIdx].Stored {
					item.Error = "Skill variant is not stored"
					result.Results = append(result.Results, item)
					continue
				}
				if err := skill.PublishTree(ctx, store.VariantPath(change.ID, change.VariantHash), target); err != nil {
					item.Error = "Cannot publish Skill to Agent"
					result.Results = append(result.Results, item)
					continue
				}
				for i := range fact.Variants {
					if i != variantIdx {
						fact.Variants[i].ManagedTargets = removeString(fact.Variants[i].ManagedTargets, agentID)
					}
				}
				fact.Variants[variantIdx].ManagedTargets = addSorted(fact.Variants[variantIdx].ManagedTargets, agentID)
			}
			registry.Skills[change.ID] = fact
			if err := store.Save(ctx, registry); err != nil {
				item.Error = "Agent updated but Skill Registry was not updated"
				result.Results = append(result.Results, item)
				continue
			}
			item.TargetUpdated, item.RegistryUpdated = true, true
			result.Results = append(result.Results, item)
		}
	}
	if req.PreviewToken != "" {
		allSucceeded := true
		for _, item := range result.Results {
			if item.Error != "" {
				allSucceeded = false
				break
			}
		}
		if allSucceeded {
			_ = u.releaseSkillPreview(req.PreviewToken, preview)
		}
	}
	return result
}

func (u *UseCases) UninstallSkill(ctx context.Context, id string) SkillUninstallResult {
	result := SkillUninstallResult{}
	if err := contextError(ctx, "Skill uninstall was cancelled"); err != nil {
		result.Error = err.Error()
		return result
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	if err := skill.ValidateID(id); err != nil {
		result.Error = err.Error()
		return result
	}
	store := u.skillStore()
	registry, err := store.Load()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	fact, ok := registry.Skills[id]
	if !ok {
		result.Error = "Skill not found"
		return result
	}
	var backup skill.BackupSummary
	if hasStoredVariants(fact) {
		backup, err = store.CreateBackup(ctx, id, fact)
		if err != nil {
			result.Error = "Cannot back up Skill before uninstall"
			return result
		}
		result.BackupID = backup.BackupID
	}
	eligible, eligibleErr := u.eligibleSkillAgents()
	if eligibleErr != nil {
		result.Error = "Cannot inspect eligible Skill Agents"
		return result
	}
	for _, variant := range fact.Variants {
		for _, agentID := range variant.ManagedTargets {
			agent, ok := eligible[agentID]
			if !ok {
				result.Error = "Agent is not eligible"
				return result
			}
			if err := removeManagedSkill(ctx, filepath.Join(skillPath(u.status.Home, u.status.Platform.OS, agent), id), variant.Hash); err != nil {
				result.Error = err.Error()
				return result
			}
		}
	}
	if err := store.RemoveSkill(ctx, id); err != nil {
		result.Error = err.Error()
		return result
	}
	delete(registry.Skills, id)
	if err := store.Save(ctx, registry); err != nil {
		result.Error = err.Error()
		return result
	}
	result.RegistryUpdated = true
	return result
}

func (u *UseCases) ListSkillBackups(ctx context.Context) ([]SkillBackupSummary, error) {
	if err := contextError(ctx, "Skill backup request was cancelled"); err != nil {
		return nil, err
	}
	backups, err := u.skillStore().ListBackups()
	if err != nil {
		return nil, err
	}
	out := make([]SkillBackupSummary, len(backups))
	for i, backup := range backups {
		out[i] = SkillBackupSummary{ID: backup.ID, BackupID: backup.BackupID, CreatedAt: backup.CreatedAt, Variants: backup.Variants}
	}
	return out, nil
}

func (u *UseCases) RestoreSkillBackup(ctx context.Context, backupID string, targets []string) SkillApplyResult {
	result := SkillApplyResult{}
	if err := contextError(ctx, "Skill restore was cancelled"); err != nil {
		result.Results = []SkillAgentApplyResult{{Error: err.Error()}}
		return result
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	store := u.skillStore()
	backups, err := store.ListBackups()
	if err != nil {
		result.Results = []SkillAgentApplyResult{{Error: err.Error()}}
		return result
	}
	backupIDFound := false
	id := ""
	for _, backup := range backups {
		if backup.BackupID == backupID {
			backupIDFound = true
			id = backup.ID
			break
		}
	}
	if !backupIDFound {
		result.Results = []SkillAgentApplyResult{{Error: "Skill backup not found"}}
		return result
	}
	registry, err := store.Load()
	if err != nil {
		result.Results = []SkillAgentApplyResult{{Error: err.Error()}}
		return result
	}
	eligible, eligibleErr := u.eligibleSkillAgents()
	if eligibleErr != nil {
		result.Results = []SkillAgentApplyResult{{Error: "Cannot inspect eligible Skill Agents"}}
		return result
	}
	for _, agentID := range targets {
		if _, ok := eligible[agentID]; !ok {
			result.Results = append(result.Results, SkillAgentApplyResult{Agent: agentID, Error: "Agent is not eligible"})
		}
	}
	if len(result.Results) > 0 {
		return result
	}
	backupSkillID, backupFact, err := store.InspectBackup(ctx, backupID)
	if err != nil {
		result.Results = []SkillAgentApplyResult{{Error: "Cannot inspect Skill backup"}}
		return result
	}
	if backupSkillID != id {
		result.Results = []SkillAgentApplyResult{{Error: "Skill backup identity mismatch"}}
		return result
	}
	fact := backupFact
	variant := firstStored(fact)
	storedCount := 0
	for _, candidate := range fact.Variants {
		if candidate.Stored {
			storedCount++
		}
	}
	if storedCount != 1 {
		result.Results = []SkillAgentApplyResult{{Error: "Select one Skill variant before restore"}}
		return result
	}
	fact, err = store.RestoreBackup(ctx, backupID)
	if err != nil {
		result.Results = []SkillAgentApplyResult{{Error: "Cannot restore Skill backup"}}
		return result
	}
	registry.Skills[id] = fact
	if err := store.Save(ctx, registry); err != nil {
		result.Results = []SkillAgentApplyResult{{Error: "Cannot update Skill Registry"}}
		return result
	}
	for _, agentID := range targets {
		agent, ok := eligible[agentID]
		item := SkillAgentApplyResult{Agent: agentID}
		if !ok {
			item.Error = "Agent is not eligible"
		} else {
			if variant.Hash == "" {
				item.Error = "Skill backup is empty"
			} else if err := skill.PublishTree(ctx, store.VariantPath(id, variant.Hash), filepath.Join(skillPath(u.status.Home, u.status.Platform.OS, agent), id)); err != nil {
				item.Error = err.Error()
			} else {
				for i := range fact.Variants {
					fact.Variants[i].ManagedTargets = removeString(fact.Variants[i].ManagedTargets, agentID)
				}
				fact.Variants[variantIndex(fact.Variants, variant.Hash)].ManagedTargets = addSorted(fact.Variants[variantIndex(fact.Variants, variant.Hash)].ManagedTargets, agentID)
				registry.Skills[id] = fact
				if saveErr := store.Save(ctx, registry); saveErr != nil {
					item.Error = "Agent updated but Skill Registry was not updated"
				} else {
					item.TargetUpdated, item.RegistryUpdated = true, true
				}
			}
		}
		result.Results = append(result.Results, item)
		_ = agent
	}
	return result
}

func (u *UseCases) SetSkillDraftState(dirty bool, locale string) {
	if u == nil {
		return
	}
	if locale == "" {
		locale = "zh"
	}
	u.skillDraft.dirty.Store(dirty)
	u.skillDraft.locale.Store(locale)
}
func (u *UseCases) SkillDraftState() (bool, string) {
	if u == nil {
		return false, "zh"
	}
	locale, _ := u.skillDraft.locale.Load().(string)
	if locale == "" {
		locale = "zh"
	}
	return u.skillDraft.dirty.Load(), locale
}

func (u *UseCases) takeSkillPreview(token string) (skillPreview, error) {
	u.skillPreviewMu.Lock()
	defer u.skillPreviewMu.Unlock()
	u.cleanupExpiredSkillPreviewsLocked()
	preview, ok := u.skillPreviews[token]
	if !ok {
		return skillPreview{}, errors.New("Skill import preview expired")
	}
	return preview, nil
}

func (u *UseCases) releaseSkillPreview(token string, preview skillPreview) error {
	u.skillPreviewMu.Lock()
	defer u.skillPreviewMu.Unlock()
	if current, ok := u.skillPreviews[token]; ok && current.expires.Equal(preview.expires) {
		delete(u.skillPreviews, token)
		return skill.CleanupCandidates(current.candidates)
	}
	return nil
}

func (u *UseCases) storeSkillPreview(source string, candidates []skill.Candidate) string {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return ""
	}
	token := hex.EncodeToString(tokenBytes)
	u.skillPreviewMu.Lock()
	defer u.skillPreviewMu.Unlock()
	u.cleanupExpiredSkillPreviewsLocked()
	if u.skillPreviews == nil {
		u.skillPreviews = map[string]skillPreview{}
	}
	u.skillPreviews[token] = skillPreview{expires: time.Now().Add(5 * time.Minute), source: source, candidates: candidates}
	return token
}

func (u *UseCases) cleanupExpiredSkillPreviewsLocked() {
	now := time.Now()
	for token, preview := range u.skillPreviews {
		if now.After(preview.expires) {
			_ = skill.CleanupCandidates(preview.candidates)
			delete(u.skillPreviews, token)
		}
	}
}

func (u *UseCases) skillPreviewParent() (string, error) {
	root := filepath.Join(u.status.Home, ".oneagent", "skill-staging")
	if hasSymlinkComponent(u.status.Home, root) {
		return "", errors.New("Skill staging path is not private")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func hasSymlinkComponent(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return true
	}
	current := base
	for _, part := range splitPath(rel) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func splitPath(path string) []string {
	parts := []string{}
	for path != "." && path != "" {
		dir, base := filepath.Split(path)
		if base != "" {
			parts = append([]string{base}, parts...)
		}
		trimmed := filepath.Clean(dir)
		if trimmed == path {
			break
		}
		path = trimmed
	}
	return parts
}
func toCandidate(candidate skill.Candidate, stored bool) SkillCandidate {
	return SkillCandidate{ID: candidate.ID, Name: candidate.Name, Description: candidate.Description, Hash: candidate.Hash, Source: candidate.Source, Files: candidate.Files, Bytes: candidate.Bytes, Diagnostic: candidate.Diagnostic, Stored: stored}
}
func previewCandidateMeta(candidates []skill.Candidate, id, name, description string) (string, string) {
	for _, candidate := range candidates {
		if candidate.ID == id {
			return candidate.Name, candidate.Description
		}
	}
	return name, description
}
func summarizeSkills(registry skill.Registry) []SkillSummary {
	ids := make([]string, 0, len(registry.Skills))
	for id := range registry.Skills {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]SkillSummary, 0, len(ids))
	for _, id := range ids {
		result = append(result, summarizeSkill(id, registry.Skills[id]))
	}
	return result
}
func summarizeSkill(id string, fact skill.Fact) SkillSummary {
	agents := map[string]bool{}
	for _, variant := range fact.Variants {
		for _, agent := range variant.ObservedAgents {
			agents[agent] = true
		}
		for _, agent := range variant.ManagedTargets {
			agents[agent] = true
		}
	}
	list := make([]string, 0, len(agents))
	for agent := range agents {
		list = append(list, agent)
	}
	sort.Strings(list)
	return SkillSummary{ID: id, Name: fact.Name, Description: fact.Description, Variants: len(fact.Variants), Agents: list, Conflict: len(fact.Variants) > 1}
}
func skillAgentIDs(agents map[string]catalog.Agent) []string {
	ids := make([]string, 0, len(agents))
	for id := range agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func variantIndex(variants []skill.Variant, hash string) int {
	for i, variant := range variants {
		if variant.Hash == hash {
			return i
		}
	}
	return -1
}
func addSorted(values []string, value string) []string {
	if value == "" || contains(values, value) {
		return values
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}
func removeString(values []string, value string) []string {
	out := values[:0]
	for _, existing := range values {
		if existing != value {
			out = append(out, existing)
		}
	}
	return out
}
func hasStoredVariants(fact skill.Fact) bool {
	for _, variant := range fact.Variants {
		if variant.Stored {
			return true
		}
	}
	return false
}
func firstStored(fact skill.Fact) skill.Variant {
	for _, variant := range fact.Variants {
		if variant.Stored {
			return variant
		}
	}
	return skill.Variant{}
}
func removeManagedSkill(ctx context.Context, target, expectedHash string) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Skill target is not a regular directory")
	}
	stats, err := skill.HashTree(ctx, target)
	if err != nil {
		return err
	}
	if stats.Hash != expectedHash {
		return errors.New("Skill target changed and was not removed")
	}
	return os.RemoveAll(target)
}
