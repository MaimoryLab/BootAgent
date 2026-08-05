package app

import (
	"context"
	"sort"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

type ProviderProbeOptions struct {
	Provider   string
	APIBaseURL string
	APIKey     string
	Model      string
	AgentIDs   []string
}

type ProviderProbeResult struct {
	Primary   provider.ProbeResult
	Protocols map[string]provider.ProbeResult
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
	target, err := u.providers.Resolve(options.Provider, options.APIBaseURL)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = target.APIKey
	}
	model, err := u.resolveProviderModel(ctx, target, apiKey, options.Model)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	protocols, err := protocolsForAgents(options.AgentIDs)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	results := make(map[string]provider.ProbeResult, len(protocols))
	for _, protocolID := range protocols {
		result, probeErr := u.provider.Probe(ctx, protocolID, "custom", apiKey, model, target.BaseFor(protocolID))
		if probeErr != nil {
			return ProviderProbeResult{}, probeErr
		}
		results[protocolID] = result
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
	return ProviderProbeResult{Primary: primary, Protocols: results}, nil
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
func (u *UseCases) SaveProvider(ctx context.Context, entry provider.Entry) (SaveProviderResult, error) {
	if u == nil {
		return SaveProviderResult{}, oneerrors.New(oneerrors.InternalError, "Provider service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Provider request was cancelled"); err != nil {
		return SaveProviderResult{}, err
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	before, _ := u.providers.Get(entry.ID)
	saved, err := u.providers.Save(ctx, entry)
	if err != nil {
		return SaveProviderResult{}, err
	}
	result := SaveProviderResult{Entry: saved}
	// Nothing an Agent config carries has changed, so there is nothing to rewrite.
	if before.BaseFor(provider.ProtocolAnthropic) == saved.BaseFor(provider.ProtocolAnthropic) &&
		before.BaseURL == saved.BaseURL && before.APIKey == saved.APIKey {
		return result, nil
	}
	result.Reapplied, result.Failures = u.reapplyProviderLocked(ctx, saved)
	return result, nil
}

// reapplyProviderLocked rewrites each configured Agent that points at this
// Provider, keeping its own model and Profile reference. Callers must hold
// writeMu.
func (u *UseCases) reapplyProviderLocked(ctx context.Context, target provider.Entry) ([]string, map[string]string) {
	var reapplied []string
	var failures map[string]string
	bindings := u.profiles.ListAgentBindings()
	agentIDs := make([]string, 0, len(bindings))
	for agentID := range bindings {
		agentIDs = append(agentIDs, agentID)
	}
	sort.Strings(agentIDs)
	for _, agentID := range agentIDs {
		binding := bindings[agentID]
		if binding.Provider != target.ID {
			continue
		}
		// The Agent's own model stays authoritative; only the Provider changed.
		var err error
		if agentID == desktopapp.WorkBuddyID {
			if u.status.Platform.OS != "macos" && u.status.Platform.OS != "windows" {
				continue
			}
			if _, err = u.writeWorkBuddyConfig(ctx, target, binding.Model); err == nil {
				_, err = u.profiles.WriteAgentBinding(ctx, agentID, profileStore.BindingWriteRequest{
					Provider: target.ID, BaseURL: target.BaseURL, Model: binding.Model, ProfileRef: binding.ProfileRef,
				})
			}
		} else {
			_, err = u.activateAgentLocked(ctx, ActivateAgentOptions{
				AgentID:   agentID,
				Provider:  target.ID,
				APIKey:    target.APIKey,
				Model:     binding.Model,
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
	return reapplied, failures
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
	return u.providers.Delete(ctx, providerID)
}

func (u *UseCases) resolveProviderModel(ctx context.Context, target provider.Entry, apiKey, model string) (string, error) {
	if model = strings.TrimSpace(model); model != "" {
		return model, nil
	}
	if apiKey == "" {
		return target.FallbackModel, nil
	}
	listing, err := u.provider.ListModels(ctx, "custom", apiKey, target.BaseURL)
	if err != nil {
		return "", err
	}
	if listing.OK && len(listing.Models) > 0 {
		return provider.PickChatModel(listing.Models), nil
	}
	return target.FallbackModel, nil
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
			if agentID == desktopapp.WorkBuddyID {
				protocols[provider.ProtocolOpenAI] = true
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
