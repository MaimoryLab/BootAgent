package app

import (
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/provider"
)

// ActivateOptions points one Agent at a provider and model.
type ActivateOptions struct {
	AgentID    string
	Provider   string
	APIBaseURL string
	APIKey     string
	Model      string
	// SmallFastModel is Claude Code's cheaper background model.
	SmallFastModel string
	Timeout        time.Duration
}

// ActivateResult is what repointing one Agent produced.
type ActivateResult struct {
	OK       bool   `json:"ok"`
	Agent    string `json:"agent"`
	Config   string `json:"config"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Binding is the record written for this Agent, which carries no credential.
	Binding *jsonorder.Object `json:"binding"`
	// Restart is how the user makes the rewritten config take effect. Reported
	// because an Agent reads its config at startup, so a rewrite is invisible to a
	// process that is already running.
	Restart string `json:"restart"`
	Next    string `json:"next"`
}

// Activate points one Agent at a provider and model, leaving every other Agent
// alone.
//
// Per-Agent credentials are what make this genuinely local: only this Agent's
// config and env file change, so a failure cannot leave two Agents disagreeing and
// there is no cross-file rollback to get right.
func (s *Service) Activate(options ActivateOptions) (ActivateResult, error) {
	manifest, err := catalog.Load()
	if err != nil {
		return ActivateResult{}, err
	}
	agentID, err := config.ValidateAgentID(options.AgentID)
	if err != nil {
		return ActivateResult{}, err
	}
	agent, present := manifest.Agent(agentID)
	if !present {
		return ActivateResult{}, oerr.Newf("INVALID_REQUEST", "Unknown Agent: %s", agentID)
	}
	if agent.GuideOnly() {
		return ActivateResult{}, oerr.Newf(
			"INVALID_REQUEST", "%s is guide-only and has no managed configuration", agentID,
		)
	}
	if options.APIKey == "" {
		return ActivateResult{}, oerr.New("INVALID_REQUEST", "API key is required")
	}
	if options.Timeout <= 0 {
		options.Timeout = 180 * time.Second
	}

	model := s.Probes.ResolveModel(options.Provider, options.APIKey, options.Model, options.APIBaseURL)
	providerName := "Custom"
	if known, found := catalog.Providers[options.Provider]; found {
		providerName = known.Name
	}
	protocol := catalog.AgentProtocol(agent.ConfigAdapter)
	configBase, err := provider.ConfigBase(options.Provider, options.APIBaseURL, protocol)
	if err != nil {
		return ActivateResult{}, err
	}

	settings := config.Settings{
		AgentID: agentID, ProviderName: providerName, BaseURL: configBase,
		APIKey: options.APIKey, Model: model, SmallFastModel: options.SmallFastModel,
	}

	// The env file first, so the config that references it never points at a file
	// that is not there yet.
	if config.NeedsEnvFile(agent) {
		baseURL, err := provider.Base(options.Provider, options.APIBaseURL)
		if err != nil {
			return ActivateResult{}, err
		}
		envSettings := settings
		envSettings.BaseURL = baseURL
		if _, err := s.Writer.WriteAgentEnv(agentID, agent, envSettings); err != nil {
			return ActivateResult{}, err
		}
	}

	configPath, err := s.Writer.Write(agent, settings)
	if err != nil {
		return ActivateResult{}, err
	}
	// Recorded only after the write succeeded, so a failed write never leaves a
	// binding claiming a state the Agent's own config does not have.
	binding, err := s.Store.WriteBinding(agentID, options.Provider, configBase, model, "")
	if err != nil {
		return ActivateResult{}, err
	}

	return ActivateResult{
		OK: true, Agent: agentID, Config: configPath, Provider: options.Provider,
		Model: model, Binding: binding,
		Restart: restartHint(agent, agentID),
		Next:    nextStep(s.Runtime, agent, agentID, model),
	}, nil
}
