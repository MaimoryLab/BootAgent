package app

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

type ProviderProbeOptions struct {
	Provider   string
	APIBaseURL string
	APIKey     string
	Model      string
	AgentIDs   []string
	// AnthropicBaseURL and Draft serve the Provider editor's "test this before I
	// save it" button. Draft says the endpoints above are the ones to probe even
	// when the Provider is not on disk yet; without it an unsaved Provider cannot
	// be tested at all, and an Anthropic base cannot be tested separately from the
	// OpenAI one. The wizard leaves both unset and keeps resolving from storage.
	AnthropicBaseURL string
	Draft            bool
}

type ProviderProbeResult struct {
	Primary   provider.ProbeResult
	Protocols map[string]provider.ProbeResult
	// Model is the ID actually probed, and AutoSelectedModel says we chose it
	// rather than the user. Together they let a failure distinguish "your key is
	// wrong" from "we picked a model this endpoint does not serve for chat" --
	// indistinguishable before, which is what made a bad auto-pick read as a
	// credential problem.
	Model             string
	AutoSelectedModel bool
}

// SaveProviderResult reports which Agents were rewritten after the edit so the
// UI can say so, and which ones could not be, keyed by Agent ID.
type SaveProviderResult struct {
	Entry     provider.Entry    `json:"entry"`
	Reapplied []string          `json:"reapplied"`
	Failures  map[string]string `json:"failures"`
}

func (u *UseCases) ProbeProvider(ctx context.Context, options ProviderProbeOptions) (ProviderProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return ProviderProbeResult{}, oneerrors.New(oneerrors.Timeout, "Request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if u == nil || u.provider == nil {
		return ProviderProbeResult{}, oneerrors.New(oneerrors.InternalError, "Provider probing is not configured", oneerrors.WithStatus(501))
	}
	resolve := u.providers.Resolve
	if options.Draft {
		resolve = func(id, openAIBase string) (provider.Entry, error) {
			return u.providers.ResolveDraft(id, openAIBase, options.AnthropicBaseURL)
		}
	}
	target, err := resolve(options.Provider, options.APIBaseURL)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = target.APIKey
	}
	model, autoSelected, err := u.resolveProviderModel(ctx, target, apiKey, options.Model)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	protocols, err := protocolsForAgents(options.AgentIDs)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	results, err := u.probeProtocols(ctx, protocols, apiKey, model, target.BaseFor)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	primary := results[protocols[0]]
	allOK := true
	for _, protocolID := range protocols {
		result := results[protocolID]
		if !result.OK {
			allOK = false
			primary = result
			break
		}
	}
	primary.OK = allOK
	return ProviderProbeResult{Primary: primary, Protocols: results, Model: model, AutoSelectedModel: autoSelected}, nil
}

func (u *UseCases) probeProtocols(ctx context.Context, protocols []string, apiKey, model string, baseFor func(string) string) (map[string]provider.ProbeResult, error) {
	results := make(map[string]provider.ProbeResult, len(protocols))
	errorsByProtocol := make(map[string]error)
	var mu sync.Mutex
	var group sync.WaitGroup
	for _, protocolID := range protocols {
		group.Add(1)
		go func(protocolID string) {
			defer group.Done()
			result, err := u.provider.Probe(ctx, protocolID, "custom", apiKey, model, baseFor(protocolID))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errorsByProtocol[protocolID] = err
				return
			}
			results[protocolID] = result
		}(protocolID)
	}
	group.Wait()
	for _, protocolID := range protocols {
		if err := errorsByProtocol[protocolID]; err != nil {
			return results, err
		}
	}
	return results, nil
}

func (u *UseCases) ListProviderModels(ctx context.Context, providerID, apiKey, customBase string) (provider.ModelsResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.ModelsResult{}, oneerrors.New(oneerrors.Timeout, "Request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if u == nil || u.provider == nil {
		return provider.ModelsResult{}, oneerrors.New(oneerrors.InternalError, "Model discovery is not configured", oneerrors.WithStatus(501))
	}
	target, err := u.providers.Resolve(providerID, customBase)
	if err != nil {
		return provider.ModelsResult{}, err
	}
	if apiKey == "" {
		apiKey = target.APIKey
	}
	return u.provider.ListModels(ctx, "custom", apiKey, target.BaseURL)
}

func (u *UseCases) GetProvider(ctx context.Context, providerID string) (provider.Entry, error) {
	if u == nil {
		return provider.Entry{}, oneerrors.New(oneerrors.InternalError, "Provider service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Provider request was cancelled"); err != nil {
		return provider.Entry{}, err
	}
	return u.providers.Get(providerID)
}

// SaveProvider persists the Provider and then rewrites the configuration of
// every Agent already pointed at it, so an edited endpoint or key takes effect
// without the user re-applying each Profile by hand. Reapply failures are
// returned per Agent instead of failing the save: the Provider record is already
// correct on disk, and reverting it would lose the edit.
// create distinguishes the "add Provider" form from editing an existing entry.
// The distinction has to come from the caller: both arrive here as a complete
// Entry, and an ID that is not on disk is equally consistent with creating a new
// Provider and with renaming one, so the store cannot infer the intent.
//
// keepExistingKey does the same for an absent key: an import restoring a file
// that deliberately carries no keys must not wipe the ones already on disk, while
// the Provider editor's empty field does mean "clear it".
func (u *UseCases) SaveProvider(ctx context.Context, entry provider.Entry, create, keepExistingKey bool) (SaveProviderResult, error) {
	if u == nil {
		return SaveProviderResult{}, oneerrors.New(oneerrors.InternalError, "Provider service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Provider request was cancelled"); err != nil {
		return SaveProviderResult{}, err
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	before, _ := u.providers.Get(entry.ID)
	// Store.Save writes APIKey unconditionally, so an entry carrying no key erases
	// whatever was on disk. That is right for the Provider editor, where an empty
	// field means the user cleared it, and wrong for a settings import, where the
	// file simply does not contain keys -- restoring one would otherwise destroy
	// the recipient's credentials. Only the caller knows which case it is.
	if keepExistingKey && entry.APIKey == "" {
		entry.APIKey = before.APIKey
	}
	write := u.providers.Save
	if create {
		write = u.providers.Create
	}
	saved, err := write(ctx, entry)
	if err != nil {
		return SaveProviderResult{}, err
	}
	result := SaveProviderResult{Entry: saved}
	// Nothing an Agent config carries has changed, so there is nothing to rewrite.
	if before.BaseFor(provider.ProtocolAnthropic) == saved.BaseFor(provider.ProtocolAnthropic) &&
		before.BaseURL == saved.BaseURL && before.APIKey == saved.APIKey {
		return result, nil
	}
	result.Reapplied, result.Failures, err = u.reapplyProviderLocked(ctx, saved)
	if err != nil {
		return result, err
	}
	return result, nil
}

// reapplyProviderLocked rewrites each configured Agent that points at this
// Provider, keeping its own model and Profile reference. Callers must hold
// writeMu.
func (u *UseCases) reapplyProviderLocked(ctx context.Context, target provider.Entry) ([]string, map[string]string, error) {
	// The Agent's own model stays authoritative; only the Provider changed.
	return u.reapplyBindingsLocked(ctx, func(binding profileStore.AgentBinding) bool {
		return binding.Provider == target.ID
	}, func(binding profileStore.AgentBinding) (provider.Entry, string) {
		return target, binding.Model
	})
}

// reapplyBindingsLocked rewrites every Agent whose binding `selects`, using the
// Provider and model that `rewrite` returns for it. Both a Provider edit and a
// Profile edit need this same per-Agent dispatch -- managed CLI Agents go
// through activation, desktop Agents split three ways by definition -- and they
// differ only in which bindings to touch and what to write. Callers must hold
// writeMu.
func (u *UseCases) reapplyBindingsLocked(
	ctx context.Context,
	selects func(profileStore.AgentBinding) bool,
	rewrite func(profileStore.AgentBinding) (provider.Entry, string),
) ([]string, map[string]string, error) {
	var reapplied []string
	var failures map[string]string
	bindings, err := u.profiles.ListAgentBindings()
	if err != nil {
		return nil, nil, err
	}
	agentIDs := make([]string, 0, len(bindings))
	for agentID := range bindings {
		agentIDs = append(agentIDs, agentID)
	}
	sort.Strings(agentIDs)
	for _, agentID := range agentIDs {
		binding := bindings[agentID]
		if !selects(binding) {
			continue
		}
		target, model := rewrite(binding)
		var err error
		if definition, isDesktop := desktopapp.DefinitionFor(agentID); isDesktop {
			if definition.SharedConfigAgentID != "" {
				_, err = u.activateAgentLocked(ctx, ActivateAgentOptions{
					AgentID:   definition.ProfileAgentID,
					Provider:  target.ID,
					APIKey:    target.APIKey,
					Model:     model,
					ProfileID: binding.ProfileRef,
				})
			} else if definition.ConfigAdapter != "" {
				_, managed, configErr := u.writeDesktopAgentConfig(ctx, definition, target, model)
				err = configErr
				if err == nil && !managed {
					continue
				}
				if err == nil && managed {
					_, err = u.profiles.WriteAgentBinding(ctx, agentID, profileStore.BindingWriteRequest{
						Provider: target.ID, BaseURL: target.BaseURL, Model: model, ProfileRef: binding.ProfileRef,
					})
				}
			} else {
				_, err = u.profiles.WriteAgentBinding(ctx, agentID, profileStore.BindingWriteRequest{
					Provider: target.ID, BaseURL: target.BaseURL, Model: model, ProfileRef: binding.ProfileRef,
				})
			}
		} else {
			_, err = u.activateAgentLocked(ctx, ActivateAgentOptions{
				AgentID:   agentID,
				Provider:  target.ID,
				APIKey:    target.APIKey,
				Model:     model,
				ProfileID: binding.ProfileRef,
			})
		}
		if err != nil {
			if failures == nil {
				failures = map[string]string{}
			}
			failures[agentID] = oneerrors.As(err).Message
			continue
		}
		reapplied = append(reapplied, agentID)
	}
	return reapplied, failures, nil
}

func (u *UseCases) DeleteProvider(ctx context.Context, providerID string) error {
	if u == nil {
		return oneerrors.New(oneerrors.InternalError, "Provider service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Provider request was cancelled"); err != nil {
		return err
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	var users []string
	bindings, err := u.catalogAgentBindings(false)
	if err != nil {
		return err
	}
	for agentID, binding := range bindings {
		if binding.Provider == strings.TrimSpace(providerID) {
			users = append(users, agentID)
		}
	}
	if len(users) > 0 {
		sort.Strings(users)
		return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Provider %s is used by Agent(s): %s", strings.TrimSpace(providerID), strings.Join(users, ", ")))
	}
	return u.providers.Delete(ctx, providerID)
}

// resolveProviderModel decides which model the connection probe sends a chat
// payload to. The second return value reports whether the choice was ours rather
// than the user's, so a failure can say which of the two it is describing.
//
// Preference order, and why: a model the user typed wins outright, because the
// probe model is explicitly their override and a failure on it is the answer they
// asked for. Otherwise the Provider's manifest model wins over anything picked out
// of the live catalogue — it is a reviewed, known-chat ID for that Provider, while
// the catalogue of an aggregator is mostly video, image and audio generators whose
// names no denylist will ever fully enumerate. PickChatModel is the last resort,
// for a custom endpoint or a Provider whose manifest model it no longer serves.
func (u *UseCases) resolveProviderModel(ctx context.Context, target provider.Entry, apiKey, model string) (string, bool, error) {
	if model = strings.TrimSpace(model); model != "" {
		return model, false, nil
	}
	if apiKey == "" {
		return target.FallbackModel, true, nil
	}
	listing, err := u.provider.ListModels(ctx, "custom", apiKey, target.BaseURL)
	if err != nil {
		return "", true, err
	}
	if listing.OK && len(listing.Models) > 0 {
		// Only when the Provider actually serves it: probing a manifest model the
		// endpoint has dropped would fail for a reason the user cannot act on.
		if fallback := strings.TrimSpace(target.FallbackModel); fallback != "" && slices.Contains(listing.Models, fallback) {
			return fallback, true, nil
		}
		return provider.PickChatModel(listing.Models), true, nil
	}
	return target.FallbackModel, true, nil
}

func protocolsForAgents(agentIDs []string) ([]string, error) {
	protocols := map[string]bool{}
	if len(agentIDs) == 0 {
		protocols[provider.ProtocolOpenAI] = true
	} else {
		manifest, err := catalog.LoadEmbedded()
		if err != nil {
			return nil, err
		}
		for _, agentID := range agentIDs {
			if agentID == "" {
				return nil, oneerrors.New(oneerrors.InvalidRequest, "agents must be a non-empty array of Agent IDs")
			}
			if definition, ok := desktopapp.DefinitionFor(agentID); ok {
				if definition.Protocol != "" {
					protocols[definition.Protocol] = true
				} else if definition.SharedConfigAgentID != "" {
					manifestAgent, exists := manifest.Agents[definition.SharedConfigAgentID]
					if !exists {
						return nil, oneerrors.New(oneerrors.InvalidRequest, "Unknown shared Agent: "+definition.SharedConfigAgentID)
					}
					protocols[provider.ProtocolForAdapter(manifestAgent.ConfigAdapter)] = true
				}
				continue
			}
			agent, ok := manifest.Agents[agentID]
			if !ok {
				return nil, oneerrors.New(oneerrors.InvalidRequest, "Unknown Agent: "+agentID)
			}
			if agent.ConfigMode == "auto" {
				protocols[provider.ProtocolForAdapter(agent.ConfigAdapter)] = true
			}
		}
		if len(protocols) == 0 {
			protocols[provider.ProtocolOpenAI] = true
		}
	}
	result := make([]string, 0, len(protocols))
	for protocolID := range protocols {
		result = append(result, protocolID)
	}
	sort.Strings(result)
	return result, nil
}
