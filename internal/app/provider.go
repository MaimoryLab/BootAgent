package app

import (
	"context"
	"sort"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
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

func (u *UseCases) SaveProvider(ctx context.Context, entry provider.Entry) (provider.Entry, error) {
	if u == nil {
		return provider.Entry{}, oneerrors.New(oneerrors.InternalError, "Provider service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Provider request was cancelled"); err != nil {
		return provider.Entry{}, err
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	return u.providers.Save(ctx, entry)
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
