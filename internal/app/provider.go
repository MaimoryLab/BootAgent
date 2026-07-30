package app

import (
	"context"
	"sort"

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
	model, err := u.provider.ResolveProbeModel(ctx, options.Provider, options.APIKey, options.Model, options.APIBaseURL)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	protocols, err := protocolsForAgents(options.AgentIDs)
	if err != nil {
		return ProviderProbeResult{}, err
	}
	results := make(map[string]provider.ProbeResult, len(protocols))
	for _, protocolID := range protocols {
		result, probeErr := u.provider.Probe(ctx, protocolID, options.Provider, options.APIKey, model, options.APIBaseURL)
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
	return u.provider.ListModels(ctx, providerID, apiKey, customBase)
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
