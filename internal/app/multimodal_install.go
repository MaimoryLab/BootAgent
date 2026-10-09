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
}

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
}

// nativeSkillEnvAdapters are the config adapters with a writer for Skill
// variables (ADR-011). Other Agents get the private fallback file.
var nativeSkillEnvAdapters = map[string]bool{"claude-code": true, "codex": true}

// multimodalInstallTargets are the Agents a multimodal Skill can be installed
// into from the multimodal section: those with a Skills directory BootAgent
// can see, and a native place for the key variable. Phase 4 widens this.
func (u *UseCases) multimodalInstallTargets() ([]string, map[string]catalog.Agent, error) {
	eligible, err := u.eligibleSkillAgents()
	if err != nil {
		return nil, nil, err
	}
	var targets []string
	for id, agent := range eligible {
		if nativeSkillEnvAdapters[agent.ConfigAdapter] {
			targets = append(targets, id)
		}
	}
	sort.Strings(targets)
	return targets, eligible, nil
}

// InstallMultimodal generates the Skill, stores it as a variant with source
// "generated", publishes it to each Agent and writes the key variable there.
// It sends no billed request: the evidence it installs on is the free model
// listing, or a probe the user already ran. A capability found only by an ID
// rule, or on an unverified adapter without a passing probe, is refused.
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
	if len(agents) == 0 {
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
	found, err := u.resolveForInstall(ctx, rules, target, model, capability)
	if err != nil {
		return result, err
	}
	if !found.Installable {
		return result, oneerrors.New(oneerrors.InvalidRequest, "This model's "+capability+" is not confirmed yet; run the check first")
	}
	if !skillgen.Supports(capability, found.Adapter) {
		return result, oneerrors.New(oneerrors.InvalidRequest, "A Skill for this route cannot be generated yet")
	}
	spec := skillgen.Spec{
		Capability: capability, Adapter: found.Adapter, ProviderID: target.ID, ProviderName: target.Name,
		Model: model, BaseURL: target.BaseURL, NativeBaseURL: rules.NativeBaseURL(target.ID),
	}
	if generation, ok := rules.Generation(target.ID, model); ok && generation.Adapter == found.Adapter {
		spec.Options = generation.Options
	}

	u.writeMu.Lock()
	defer u.writeMu.Unlock()
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
	if len(install) == 0 {
		return result, nil
	}
	published, err := u.publishGeneratedLocked(ctx, spec, install)
	if err != nil {
		return result, err
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
// publishes it to each Agent. It returns a per-Agent error message ("" on
// success); an error return means nothing was published. Callers hold writeMu.
func (u *UseCases) publishGeneratedLocked(ctx context.Context, spec skillgen.Spec, agentIDs []string) (map[string]string, error) {
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
	return results, nil
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
}

// generatedVariants lists the variants BootAgent rendered here. Only these
// earn a key variable: a tree imported from a ZIP could name any base URL, and
// writing a real key for it would hand that key to whoever wrote the ZIP.
func (u *UseCases) generatedVariants(registry skill.Registry) []generatedVariant {
	store := u.skillStore()
	var result []generatedVariant
	for _, id := range slices.Sorted(maps.Keys(registry.Skills)) {
		if skillgen.CapabilityFromSkillID(id) == "" {
			continue
		}
		for _, variant := range registry.Skills[id].Variants {
			if !variant.Stored || !slices.Contains(variant.ImportSources, skill.ImportSourceGenerated) {
				continue
			}
			config, err := skillgen.ReadConfig(store.VariantPath(id, variant.Hash))
			if err != nil {
				continue
			}
			result = append(result, generatedVariant{id: id, hash: variant.Hash, config: config, targets: variant.ManagedTargets})
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
func (u *UseCases) reconcileSkillEnvLocked(ctx context.Context) map[string]string {
	failures := map[string]string{}
	registry, err := u.skillStore().Load()
	if err != nil {
		return failures
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return failures
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
	for _, variant := range u.generatedVariants(registry) {
		for _, agentID := range variant.targets {
			agent, ok := manifest.Agents[agentID]
			if !ok {
				continue
			}
			key := keyFor(variant.config.ProviderID)
			if key == "" {
				failures[agentID] = "Provider " + variant.config.ProviderID + " has no saved API key"
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
	writer := configWriter.NewWriter(u.status.Home, u.status.Platform.OS, u.filesystem)
	for _, agentID := range slices.Sorted(maps.Keys(manifest.Agents)) {
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
			if _, needed := fallback[match[1]]; !needed {
				_ = configWriter.RemoveSkillEnvFile(filepath.Join(directory, entry.Name()))
			}
		}
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
		if len(variant.targets) == 0 {
			continue
		}
		result = append(result, InstalledMultimodal{
			SkillID: variant.id, Capability: variant.config.Capability, ProviderID: variant.config.ProviderID,
			Model: variant.config.Model, Adapter: variant.config.Adapter, Agents: slices.Clone(variant.targets),
		})
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
		if variant.config.ProviderID == providerID && len(variant.targets) > 0 {
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
		if provider.OpenAIBaseURL(target.BaseURL) == variant.config.BaseURL && target.Name == variant.config.ProviderName {
			continue
		}
		spec := skillgen.Spec{
			Capability: variant.config.Capability, Adapter: variant.config.Adapter,
			ProviderID: target.ID, ProviderName: target.Name, Model: variant.config.Model,
			BaseURL: target.BaseURL, NativeBaseURL: variant.config.NativeBaseURL, Options: variant.config.Options,
		}
		published, err := u.publishGeneratedLocked(ctx, spec, variant.targets)
		if err != nil {
			for _, agentID := range variant.targets {
				failures[agentID] = oneerrors.As(err).Message
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
