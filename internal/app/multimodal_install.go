package app

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	configWriter "github.com/MaimoryLab/BootAgent/internal/config"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/provider"
	"github.com/MaimoryLab/BootAgent/internal/skill"
	"github.com/MaimoryLab/BootAgent/internal/skillgen"
)

// MultimodalInstallRequest installs the Skill for one capability, bound to one
// Provider and model, into the named Agents.
type MultimodalInstallRequest struct {
	Provider   string   `json:"provider"`
	Model      string   `json:"model"`
	Capability string   `json:"capability"`
	Agents     []string `json:"agents"`
	// Guide also writes the Skill to ~/.bootagent/multimodal/<capability>/
	// for Agents without a Skills directory, which are pointed at it by a
	// prompt. Their key goes to the fallback file.
	Guide bool `json:"guide"`
	// Explore installs without knowing how the model is called: the Skill tells
	// the Agent to work the request out with the user or from the Provider's
	// documentation, and sends only to the Provider's own origin. The user
	// opts into it, so it needs no evidence for the capability.
	Explore bool `json:"explore,omitempty"`
}

// GuideTarget names the guide in per-target results. It is not an Agent ID
// and never enters the Skill registry: the guide directory is found again by
// its hash.
const GuideTarget = "guide"

type MultimodalAgentResult struct {
	Agent string `json:"agent"`
	// Installed: the Skill tree is in the Agent's Skills directory.
	Installed bool `json:"installed"`
	// EnvWritten: the key variable is in the Agent's own configuration.
	EnvWritten bool   `json:"envWritten"`
	Error      string `json:"error,omitempty"`
}

type MultimodalInstallResult struct {
	SkillID string                  `json:"skillId"`
	Results []MultimodalAgentResult `json:"results"`
	// GuidePath is the guide directory when one was written.
	GuidePath string `json:"guidePath,omitempty"`
}

// InstalledMultimodal is one generated Skill variant that is published to at
// least one Agent.
type InstalledMultimodal struct {
	SkillID    string   `json:"skillId"`
	Capability string   `json:"capability"`
	ProviderID string   `json:"providerId"`
	Model      string   `json:"model"`
	Adapter    string   `json:"adapter"`
	Agents     []string `json:"agents"`
	// GuidePath is set when this variant is also the guide for Agents
	// without Skills.
	GuidePath string `json:"guidePath,omitempty"`
}

// nativeSkillEnvAdapters are the config adapters with a writer for Skill
// variables (ADR-011). Other Agents get the private fallback file.
var nativeSkillEnvAdapters = map[string]bool{"claude-code": true, "codex": true}

// pendingSkillTargets are Agents with a Skills directory that are not offered.
// Hermes is installed by its official script, so Node may be absent where it
// runs, and BootAgent's managed Node sits at a versioned path that moves on
// every upgrade and does not exist on Hermes's remote backends. Decided not to
// offer it (design doc, Hermes); Hermes users can use the guide.
var pendingSkillTargets = map[string]bool{"hermes": true}

// multimodalInstallTargets are the Agents a multimodal Skill can be installed
// into from the multimodal section: those with a Skills directory BootAgent
// can see. Claude Code and Codex get the key in their own configuration; the
// others read the private fallback file, since neither OpenCode nor Hermes
// documents a way to set the environment of its shell tool.
func (u *UseCases) multimodalInstallTargets() ([]string, map[string]catalog.Agent, error) {
	eligible, err := u.eligibleSkillAgents()
	if err != nil {
		return nil, nil, err
	}
	var targets []string
	for id := range eligible {
		if !pendingSkillTargets[id] {
			targets = append(targets, id)
		}
	}
	sort.Strings(targets)
	return targets, eligible, nil
}

// guideDir is where the Skill for a capability is written for Agents without
// a Skills directory.
func (u *UseCases) guideDir(capability string) string {
	return filepath.Join(u.status.Home, ".bootagent", "multimodal", capability)
}

// InstallMultimodal generates the Skill, stores it as a variant with source
// "generated", publishes it to each Agent and writes the key variable there.
// It sends no billed request: the evidence it installs on is the free model
// listing, or a probe the user already ran. A capability found only by an ID
// rule, or on an unverified adapter without a passing probe, is refused unless
// the user chose explore mode, which claims nothing about the model.
func (u *UseCases) InstallMultimodal(ctx context.Context, request MultimodalInstallRequest) (MultimodalInstallResult, error) {
	if u == nil || u.provider == nil {
		return MultimodalInstallResult{}, oneerrors.New(oneerrors.InternalError, "Multimodal install is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Multimodal install was cancelled"); err != nil {
		return MultimodalInstallResult{}, err
	}
	model := strings.TrimSpace(request.Model)
	capability := strings.TrimSpace(request.Capability)
	result := MultimodalInstallResult{SkillID: skillgen.SkillID(capability), Results: []MultimodalAgentResult{}}
	if model == "" || !slices.Contains(modality.Capabilities, capability) {
		return result, oneerrors.New(oneerrors.InvalidRequest, "A model and a known capability are required")
	}
	agents := slices.Compact(slices.Sorted(slices.Values(request.Agents)))
	if len(agents) == 0 && !request.Guide {
		return result, oneerrors.New(oneerrors.InvalidRequest, "Choose at least one Agent")
	}
	target, err := u.providers.Get(request.Provider)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(target.APIKey) == "" {
		return result, oneerrors.New(oneerrors.InvalidRequest, "Save an API key for this Provider first")
	}
	rules, err := modality.Load()
	if err != nil {
		return result, err
	}
	var spec skillgen.Spec
	if request.Explore {
		spec = skillgen.Spec{
			Capability: capability, Adapter: modality.AdapterExplore, ProviderID: target.ID, ProviderName: target.Name,
			Model: model, BaseURL: target.BaseURL, ProviderHome: target.Home,
		}
	} else {
		found, err := u.resolveForInstall(ctx, rules, target, model, capability)
		if err != nil {
			return result, err
		}
		if !found.Installable {
			return result, oneerrors.New(oneerrors.InvalidRequest, "This model's "+capability+" is not confirmed yet; run the check first, or install it in explore mode")
		}
		if !skillgen.Supports(capability, found.Adapter) {
			return result, oneerrors.New(oneerrors.InvalidRequest, "A Skill for this route cannot be generated yet")
		}
		spec = skillgen.Spec{
			Capability: capability, Adapter: found.Adapter, ProviderID: target.ID, ProviderName: target.Name,
			Model: model, BaseURL: target.BaseURL, NativeBaseURL: rules.NativeBaseURL(target.ID),
		}
		if generation, ok := rules.Generation(target.ID, model); ok && generation.Adapter == found.Adapter {
			spec.Options = generation.Options
		}
	}

	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	current, err := u.providers.Get(target.ID)
	if err != nil {
		return result, err
	}
	if provider.Fingerprint(current) != provider.Fingerprint(target) || current.Name != target.Name {
		return result, oneerrors.New(oneerrors.InvalidRequest, "The Provider changed while its model was being checked; retry the installation", oneerrors.WithRetryable(true))
	}
	targets, _, err := u.multimodalInstallTargets()
	if err != nil {
		return result, err
	}
	var install []string
	for _, agentID := range agents {
		if slices.Contains(targets, agentID) {
			install = append(install, agentID)
		} else {
			result.Results = append(result.Results, MultimodalAgentResult{Agent: agentID, Error: "This Agent cannot take a multimodal Skill yet"})
		}
	}
	if len(install) == 0 && !request.Guide {
		return result, nil
	}
	published, err := u.publishGeneratedLocked(ctx, spec, install, request.Guide)
	if err != nil {
		return result, err
	}
	if request.Guide {
		install = append(install, GuideTarget)
		if published[GuideTarget] == "" {
			result.GuidePath = u.guideDir(capability)
		}
	}
	envFailures := u.reconcileSkillEnvLocked(ctx)
	for _, agentID := range install {
		item := MultimodalAgentResult{Agent: agentID}
		if failure, ok := published[agentID]; ok && failure != "" {
			item.Error = failure
		} else {
			item.Installed = true
			if failure, ok := envFailures[agentID]; ok {
				item.Error = "Skill installed, but its key variable was not written: " + failure
			} else {
				item.EnvWritten = true
			}
		}
		result.Results = append(result.Results, item)
	}
	sort.Slice(result.Results, func(i, j int) bool { return result.Results[i].Agent < result.Results[j].Agent })
	return result, nil
}

// resolveForInstall finds the evidence for one model's capability. A probe
// verdict needs no network; otherwise the free listing is read again rather
// than trusting what the page sent.
func (u *UseCases) resolveForInstall(ctx context.Context, rules modality.Rules, target provider.Entry, model, capability string) (modality.Capability, error) {
	verdicts := u.capabilities.Modalities(target)
	if found, ok := modality.Find(rules.Resolve(target.ID, nil, verdicts), model, capability); ok {
		return found, nil
	}
	listing, err := u.provider.ListModels(ctx, "custom", target.APIKey, target.BaseURL)
	if err != nil {
		return modality.Capability{}, err
	}
	if !listing.OK {
		return modality.Capability{}, oneerrors.New(oneerrors.ProviderUnreachable, "Cannot confirm the model: "+listing.Message, oneerrors.WithRetryable(true))
	}
	found, ok := modality.Find(rules.Resolve(target.ID, listing.Entries, verdicts), model, capability)
	if !ok {
		return modality.Capability{}, oneerrors.New(oneerrors.InvalidRequest, "This Provider does not offer "+capability+" on "+model)
	}
	return found, nil
}

// publishGeneratedLocked renders the Skill, stores it as a variant and
// publishes it to each Agent, and to the guide directory when guide is set.
// It returns a per-target error message ("" on success), the guide under
// GuideTarget; an error return means nothing was published. Callers hold
// writeMu.
func (u *UseCases) publishGeneratedLocked(ctx context.Context, spec skillgen.Spec, agentIDs []string, guide bool) (map[string]string, error) {
	id := skillgen.SkillID(spec.Capability)
	store := u.skillStore()
	registry, err := store.Load()
	if err != nil {
		return nil, oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot read the Skill Registry", oneerrors.WithCause(err))
	}
	fact, exists := registry.Skills[id]
	if exists && !u.isGeneratedFact(id, fact) {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "A Skill named "+id+" that BootAgent did not generate is already managed; remove it first")
	}
	eligible, err := u.eligibleSkillAgents()
	if err != nil {
		return nil, err
	}
	if err := store.EnsureSkillsRoot(ctx); err != nil {
		return nil, oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot prepare Skill storage", oneerrors.WithCause(err))
	}
	stage, err := os.MkdirTemp(store.SkillsRoot(), ".bootagent-generate-")
	if err != nil {
		return nil, oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot prepare the Skill", oneerrors.WithCause(err))
	}
	defer os.RemoveAll(stage)
	tree := filepath.Join(stage, id)
	if err := os.Mkdir(tree, 0o700); err != nil {
		return nil, oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot prepare the Skill", oneerrors.WithCause(err))
	}
	if err := skillgen.Render(spec, tree); err != nil {
		return nil, oneerrors.New(oneerrors.InvalidRequest, err.Error())
	}
	stats, err := skill.HashTree(ctx, tree)
	if err != nil {
		return nil, oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot hash the Skill", oneerrors.WithCause(err))
	}
	index := variantIndex(fact.Variants, stats.Hash)
	if index < 0 || !fact.Variants[index].Stored {
		if err := store.SaveVariant(ctx, id, tree, stats); err != nil {
			return nil, oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot store the Skill", oneerrors.WithCause(err))
		}
	}
	if index < 0 {
		fact.Variants = append(fact.Variants, skill.Variant{Hash: stats.Hash})
		index = len(fact.Variants) - 1
	}
	fact.Name, fact.Description, _ = skill.ReadMetadata(ctx, tree, id)
	fact.Variants[index].Stored = true
	fact.Variants[index].ImportSources = addSorted(fact.Variants[index].ImportSources, skill.ImportSourceGenerated)
	registry.Skills[id] = fact
	if err := store.Save(ctx, registry); err != nil {
		return nil, oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot update the Skill Registry", oneerrors.WithCause(err))
	}

	results := map[string]string{}
	for _, agentID := range agentIDs {
		agent, ok := eligible[agentID]
		if !ok {
			results[agentID] = "Agent is not eligible"
			continue
		}
		destination := filepath.Join(skillPath(u.status.Home, u.status.Platform.OS, agent), id)
		if hasSymlinkComponent(u.status.Home, filepath.Dir(destination)) {
			results[agentID] = "Skill target path is not private"
			continue
		}
		if err := verifyGeneratedTarget(ctx, destination, fact); err != nil {
			results[agentID] = err.Error()
			continue
		}
		if err := skill.PublishTree(ctx, store.VariantPath(id, stats.Hash), destination); err != nil {
			results[agentID] = "Cannot publish Skill to Agent"
			continue
		}
		for i := range fact.Variants {
			if i != index {
				fact.Variants[i].ManagedTargets = removeString(fact.Variants[i].ManagedTargets, agentID)
			}
		}
		fact.Variants[index].ManagedTargets = addSorted(fact.Variants[index].ManagedTargets, agentID)
		registry.Skills[id] = fact
		if err := store.Save(ctx, registry); err != nil {
			results[agentID] = "Agent updated but Skill Registry was not updated"
			continue
		}
		results[agentID] = ""
	}
	if guide {
		results[GuideTarget] = u.publishGuideLocked(ctx, store, id, stats.Hash, fact)
	}
	return results, nil
}

// publishGuideLocked writes a stored variant to the guide directory, with
// the same rule as an Agent target: only a variant of this Skill BootAgent
// stored may be replaced. It returns an error message, "" on success.
func (u *UseCases) publishGuideLocked(ctx context.Context, store skill.Store, id, hash string, fact skill.Fact) string {
	destination := u.guideDir(skillgen.CapabilityFromSkillID(id))
	parent := filepath.Dir(destination)
	if hasSymlinkComponent(u.status.Home, parent) {
		return "Guide path is not private"
	}
	if err := u.filesystem.EnsurePrivateDir(ctx, parent); err != nil {
		return "Cannot prepare the guide directory"
	}
	if err := verifyGeneratedTarget(ctx, destination, fact); err != nil {
		return err.Error()
	}
	if err := skill.PublishTree(ctx, store.VariantPath(id, hash), destination); err != nil {
		return "Cannot write the guide"
	}
	return ""
}

// guideHash is the hash of a capability's guide directory, empty when there
// is none or it is not a plain directory.
func (u *UseCases) guideHash(ctx context.Context, capability string) string {
	destination := u.guideDir(capability)
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	stats, err := skill.HashTree(ctx, destination)
	if err != nil {
		return ""
	}
	return stats.Hash
}

// removeGeneratedGuide deletes a Skill's guide directory when it holds one of
// the Skill's stored variants; anything else there is left alone.
func (u *UseCases) removeGeneratedGuide(ctx context.Context, id string, fact skill.Fact) error {
	capability := skillgen.CapabilityFromSkillID(id)
	if capability == "" {
		return nil
	}
	hash := u.guideHash(ctx, capability)
	if hash == "" || variantIndex(fact.Variants, hash) < 0 {
		return nil
	}
	return removeManagedSkill(ctx, u.guideDir(capability), hash)
}

// verifyGeneratedTarget allows replacing what is at the destination only when
// it is a variant of this Skill BootAgent stored. Anything else -- a Skill the
// user wrote under the same name, or an edited copy -- is left alone.
func verifyGeneratedTarget(ctx context.Context, destination string, fact skill.Fact) error {
	info, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("A file BootAgent did not write is in the way; it was left alone")
	}
	stats, err := skill.HashTree(ctx, destination)
	if err != nil || variantIndex(fact.Variants, stats.Hash) < 0 {
		return errors.New("A Skill with this name that BootAgent did not write is installed here; it was left alone")
	}
	return nil
}

// isGeneratedFact reports whether a registry entry is a generated Skill. A
// variant rendered here carries the generated source; one copied from another
// machine arrives as a ZIP, but its stored config.json still reads as ours.
func (u *UseCases) isGeneratedFact(id string, fact skill.Fact) bool {
	if skillgen.CapabilityFromSkillID(id) == "" {
		return false
	}
	store := u.skillStore()
	for _, variant := range fact.Variants {
		if slices.Contains(variant.ImportSources, skill.ImportSourceGenerated) {
			continue
		}
		if !variant.Stored {
			return false
		}
		if _, err := skillgen.ReadConfig(store.VariantPath(id, variant.Hash)); err != nil {
			return false
		}
	}
	return true
}

// generatedVariant is one rendered variant and the config it was made from.
type generatedVariant struct {
	id      string
	hash    string
	config  skillgen.Config
	targets []string
	// guide: this variant is what the capability's guide directory holds.
	guide bool
}

// generatedVariants lists the variants BootAgent rendered here. Only these
// earn a key variable: a tree imported from a ZIP could name any base URL, and
// writing a real key for it would hand that key to whoever wrote the ZIP.
func (u *UseCases) generatedVariants(registry skill.Registry) []generatedVariant {
	store := u.skillStore()
	var result []generatedVariant
	for _, id := range slices.Sorted(maps.Keys(registry.Skills)) {
		capability := skillgen.CapabilityFromSkillID(id)
		if capability == "" {
			continue
		}
		guide := u.guideHash(context.Background(), capability)
		for _, variant := range registry.Skills[id].Variants {
			if !variant.Stored || !slices.Contains(variant.ImportSources, skill.ImportSourceGenerated) {
				continue
			}
			config, err := skillgen.ReadConfig(store.VariantPath(id, variant.Hash))
			if err != nil {
				continue
			}
			result = append(result, generatedVariant{id: id, hash: variant.Hash, config: config, targets: variant.ManagedTargets, guide: guide == variant.Hash})
		}
	}
	return result
}

var fallbackEnvFile = regexp.MustCompile(`^([a-z0-9][a-z0-9-]{0,63})\.env$`)

// reconcileSkillEnvLocked makes every Agent's Skill variables match what its
// generated Skills need: each Agent with a generated Skill for Provider P gets
// P's key; variables no Skill needs any more are removed. Claude Code and
// Codex get them in their own configuration; any other Agent a generated Skill
// is published to reads the private fallback file. It returns failures by
// Agent and never fails the caller's own change. Callers hold writeMu.
func (u *UseCases) reconcileSkillEnvLocked(ctx context.Context, providerIDs ...string) map[string]string {
	failures := map[string]string{}
	selectedProvider := func(providerID string) bool {
		return len(providerIDs) == 0 || slices.Contains(providerIDs, providerID)
	}
	loadFailure := func(message string) map[string]string {
		if len(providerIDs) == 0 {
			failures[GuideTarget] = message
			return failures
		}
		for _, providerID := range providerIDs {
			failures[providerID] = message
		}
		return failures
	}
	registry, err := u.skillStore().Load()
	if err != nil {
		return loadFailure("Cannot read the Skill Registry")
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return loadFailure("Cannot read the Agent catalog")
	}
	keys := map[string]string{}
	keyFor := func(providerID string) string {
		if key, ok := keys[providerID]; ok {
			return key
		}
		entry, err := u.providers.Get(providerID)
		key := ""
		if err == nil {
			key = strings.TrimSpace(entry.APIKey)
		}
		keys[providerID] = key
		return key
	}
	// desired[agent][variable] = key; fallback[provider] = agents that need it.
	desired := map[string]map[string]string{}
	fallback := map[string][]string{}
	blockedNative := map[string]map[string]bool{}
	blockedFallback := map[string]bool{}
	selectedAgents := map[string]bool{}
	variants := u.generatedVariants(registry)
	for _, variant := range variants {
		if slices.Contains(providerIDs, variant.config.ProviderID) {
			for _, agentID := range variant.targets {
				selectedAgents[agentID] = true
			}
		}
	}
	rules, err := modality.Load()
	if err != nil {
		return loadFailure("Cannot read modality routes")
	}
	for _, variant := range variants {
		entry, entryErr := u.providers.Get(variant.config.ProviderID)
		// An endpoint refresh may fail for a user-edited tree. Never hand the
		// new endpoint's key to a variant that still sends to the old one. Native
		// adapters send to native_base_url, regardless of the OpenAI base snapshot.
		matches := entryErr == nil && provider.OpenAIBaseURL(entry.BaseURL) == variant.config.BaseURL
		if modality.IsNative(variant.config.Adapter) {
			matches = entryErr == nil && rules.NativeBaseURL(entry.ID) == variant.config.NativeBaseURL
		}
		if !matches {
			if variant.guide {
				blockedFallback[variant.config.ProviderID] = true
				if selectedProvider(variant.config.ProviderID) {
					failures[GuideTarget] = "Skill still uses a previous Provider endpoint; reinstall it before updating its key"
				}
			}
			for _, agentID := range variant.targets {
				if selectedProvider(variant.config.ProviderID) {
					failures[agentID] = "Skill still uses a previous Provider endpoint; reinstall it before updating its key"
				}
				if nativeSkillEnvAdapters[manifest.Agents[agentID].ConfigAdapter] {
					if blockedNative[agentID] == nil {
						blockedNative[agentID] = map[string]bool{}
					}
					blockedNative[agentID][variant.config.KeyEnv] = true
				} else {
					blockedFallback[variant.config.ProviderID] = true
				}
			}
			continue
		}
		if variant.guide {
			if keyFor(variant.config.ProviderID) == "" {
				if selectedProvider(variant.config.ProviderID) {
					failures[GuideTarget] = "Provider " + variant.config.ProviderID + " has no saved API key"
				}
			} else {
				fallback[variant.config.ProviderID] = append(fallback[variant.config.ProviderID], GuideTarget)
			}
		}
		for _, agentID := range variant.targets {
			agent, ok := manifest.Agents[agentID]
			if !ok {
				continue
			}
			key := keyFor(variant.config.ProviderID)
			if key == "" {
				if selectedProvider(variant.config.ProviderID) {
					failures[agentID] = "Provider " + variant.config.ProviderID + " has no saved API key"
				}
				continue
			}
			if nativeSkillEnvAdapters[agent.ConfigAdapter] {
				if desired[agentID] == nil {
					desired[agentID] = map[string]string{}
				}
				desired[agentID][variant.config.KeyEnv] = key
			} else {
				fallback[variant.config.ProviderID] = append(fallback[variant.config.ProviderID], agentID)
			}
		}
	}
	for agentID, variables := range blockedNative {
		for variable := range variables {
			delete(desired[agentID], variable)
		}
	}
	for providerID := range blockedFallback {
		if selectedProvider(providerID) {
			for _, agentID := range fallback[providerID] {
				failures[agentID] = "Another Skill using this key still points at a previous Provider endpoint"
			}
		}
		delete(fallback, providerID)
	}
	writer := configWriter.NewWriter(u.status.Home, u.status.Platform.OS, u.filesystem)
	for _, agentID := range slices.Sorted(maps.Keys(manifest.Agents)) {
		if len(providerIDs) > 0 && !selectedAgents[agentID] {
			continue
		}
		agent := manifest.Agents[agentID]
		if !nativeSkillEnvAdapters[agent.ConfigAdapter] {
			continue
		}
		path := configPath(u.status.Home, u.status.Platform.OS, agent)
		if path == "" {
			continue
		}
		var err error
		switch agent.ConfigAdapter {
		case "claude-code":
			err = writer.WriteClaudeSkillEnv(ctx, path, desired[agentID])
		case "codex":
			err = writer.WriteCodexSkillEnv(ctx, path, desired[agentID])
		}
		if err != nil {
			failures[agentID] = oneerrors.As(err).Message
		}
	}
	for providerID, agents := range fallback {
		if len(providerIDs) > 0 && !slices.Contains(providerIDs, providerID) {
			continue
		}
		path := configWriter.SkillEnvFilePath(u.status.Home, providerID)
		if err := writer.WriteSkillEnvFile(ctx, path, configWriter.SkillEnvName(providerID), keyFor(providerID)); err != nil {
			for _, agentID := range agents {
				failures[agentID] = oneerrors.As(err).Message
			}
		}
	}
	directory := filepath.Dir(configWriter.SkillEnvFilePath(u.status.Home, "x"))
	if entries, err := os.ReadDir(directory); err == nil {
		for _, entry := range entries {
			match := fallbackEnvFile.FindStringSubmatch(entry.Name())
			if match == nil || !entry.Type().IsRegular() {
				continue
			}
			if len(providerIDs) > 0 && !slices.Contains(providerIDs, match[1]) {
				continue
			}
			if _, needed := fallback[match[1]]; !needed {
				if err := configWriter.RemoveSkillEnvFile(filepath.Join(directory, entry.Name())); err != nil {
					failures[match[1]] = oneerrors.As(err).Message
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return loadFailure("Cannot inspect the Skill environment directory")
	}
	return failures
}

// installedMultimodal lists the generated variants published somewhere.
func (u *UseCases) installedMultimodal() []InstalledMultimodal {
	registry, err := u.skillStore().Load()
	if err != nil {
		return []InstalledMultimodal{}
	}
	result := []InstalledMultimodal{}
	for _, variant := range u.generatedVariants(registry) {
		if len(variant.targets) == 0 && !variant.guide {
			continue
		}
		installed := InstalledMultimodal{
			SkillID: variant.id, Capability: variant.config.Capability, ProviderID: variant.config.ProviderID,
			Model: variant.config.Model, Adapter: variant.config.Adapter, Agents: slices.Clone(variant.targets),
		}
		if installed.Agents == nil {
			installed.Agents = []string{}
		}
		if variant.guide {
			installed.GuidePath = u.guideDir(variant.config.Capability)
		}
		result = append(result, installed)
	}
	return result
}

// generatedSkillsUsing lists the published generated Skills bound to a
// Provider, for the Provider delete guard and the base URL refresh.
func (u *UseCases) generatedSkillsUsing(providerID string) []generatedVariant {
	registry, err := u.skillStore().Load()
	if err != nil {
		return nil
	}
	var result []generatedVariant
	for _, variant := range u.generatedVariants(registry) {
		if variant.config.ProviderID == providerID && (len(variant.targets) > 0 || variant.guide) {
			result = append(result, variant)
		}
	}
	return result
}

// refreshGeneratedSkillsLocked re-renders a Provider's generated Skills after
// its endpoint changed, so config.json does not point at the old base. The
// key needs no re-render: it lives in the Agents' configuration, which the
// env reconcile rewrites. Failures are returned by Agent. Callers hold writeMu.
func (u *UseCases) refreshGeneratedSkillsLocked(ctx context.Context, target provider.Entry) map[string]string {
	failures := map[string]string{}
	for _, variant := range u.generatedSkillsUsing(target.ID) {
		spec := skillgen.Spec{
			Capability: variant.config.Capability, Adapter: variant.config.Adapter,
			ProviderID: target.ID, ProviderName: target.Name, Model: variant.config.Model,
			BaseURL: target.BaseURL, NativeBaseURL: variant.config.NativeBaseURL, Options: variant.config.Options,
		}
		if variant.config.Adapter == modality.AdapterExplore {
			// The website is where the Agent is told to look for documentation.
			spec.ProviderHome = target.Home
		}
		if fresh, err := skillgen.NewConfig(spec); err == nil && fresh.BaseURL == variant.config.BaseURL &&
			fresh.ProviderName == variant.config.ProviderName && fresh.ProviderHome == variant.config.ProviderHome {
			continue
		}
		published, err := u.publishGeneratedLocked(ctx, spec, variant.targets, variant.guide)
		if err != nil {
			for _, agentID := range variant.targets {
				failures[agentID] = oneerrors.As(err).Message
			}
			if variant.guide {
				failures[GuideTarget] = oneerrors.As(err).Message
			}
			continue
		}
		for agentID, failure := range published {
			if failure != "" {
				failures[agentID] = failure
			}
		}
	}
	return failures
}
