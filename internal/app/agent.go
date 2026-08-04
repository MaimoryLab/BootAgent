package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	configWriter "github.com/MaimoryLab/OneAgent/internal/config"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

// ActivateAgentOptions is the transport-independent input for pointing one
// managed Agent at a Provider. APIKey is deliberately kept inside the use
// case and never appears in ActivateAgentResult.
type ActivateAgentOptions struct {
	AgentID        string
	Provider       string
	APIBaseURL     string
	APIKey         string
	Model          string
	ProfileID      string
	SmallFastModel string
}

// ActivateAgentResult contains only the public outcome needed by the UI and
// CLI. The binding itself is persisted separately and is not repeated here.
type ActivateAgentResult struct {
	AgentID  string                    `json:"agent"`
	Config   string                    `json:"config"`
	Provider string                    `json:"provider"`
	Model    string                    `json:"model"`
	Restart  string                    `json:"restart"`
	Next     string                    `json:"next"`
	Binding  profileStore.AgentBinding `json:"binding"`
}

var managedAgentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ActivateAgent points one Agent at a Provider while leaving all other Agent
// files untouched. The write lock covers model resolution and every write so
// concurrent Wails calls cannot interleave backups or publish stale bindings.
func (u *UseCases) ActivateAgent(ctx context.Context, options ActivateAgentOptions) (ActivateAgentResult, error) {
	if u == nil {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Agent activation request was cancelled"); err != nil {
		return ActivateAgentResult{}, err
	}

	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	return u.activateAgentLocked(ctx, options)
}

// activateAgentLocked is ActivateAgent without the lock so callers that already
// hold writeMu, such as a Provider edit re-applying its Agents, can reuse it.
func (u *UseCases) activateAgentLocked(ctx context.Context, options ActivateAgentOptions) (ActivateAgentResult, error) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return ActivateAgentResult{}, err
	}
	agentID := options.AgentID
	if !managedAgentIDPattern.MatchString(agentID) {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Invalid Agent ID: %s", agentID))
	}
	agent, ok := manifest.Agents[agentID]
	if !ok {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "Unknown Agent: "+agentID)
	}
	if !contains(agent.Platforms, u.status.Platform.OS) {
		return ActivateAgentResult{}, oneerrors.New(
			oneerrors.PrerequisiteMissing,
			fmt.Sprintf("%s is not supported on %s", agent.Name, u.status.Platform.OS),
		)
	}
	if agent.ConfigMode != "auto" {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("%s is guide-only and has no managed configuration", agentID))
	}

	providerID := strings.TrimSpace(options.Provider)
	if providerID == "" {
		providerID = "ppio"
	}
	target, err := u.providers.Resolve(providerID, options.APIBaseURL)
	if err != nil {
		return ActivateAgentResult{}, err
	}
	apiKey := options.APIKey
	profileID := strings.TrimSpace(options.ProfileID)
	if apiKey == "" && profileID != "" {
		apiKey, err = u.profiles.ReadSecret(ctx, profileID)
		if err != nil {
			return ActivateAgentResult{}, err
		}
	}
	if apiKey == "" {
		apiKey = target.APIKey
	}
	if apiKey == "" {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	if err := u.providers.SaveKey(ctx, providerID, apiKey); err != nil {
		return ActivateAgentResult{}, err
	}

	model, err := u.resolveProviderModel(ctx, target, apiKey, options.Model)
	if err != nil {
		return ActivateAgentResult{}, err
	}
	if model == "" {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "model is required")
	}

	protocol := provider.ProtocolForAdapter(agent.ConfigAdapter)
	configBaseURL := target.BaseFor(protocol)
	providerName := target.Name
	configPath := configPath(u.status.Home, u.status.Platform.OS, agent)
	if configPath == "" {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "Managed Agent has no configuration path")
	}

	writer := configWriter.NewWriter(u.status.Home, u.status.Platform.OS, u.filesystem)
	if err := writeManagedAgentConfig(ctx, writer, agentID, agent, configPath, providerName, configBaseURL, apiKey, model, options.SmallFastModel); err != nil {
		return ActivateAgentResult{}, err
	}
	binding, err := u.profiles.WriteAgentBinding(ctx, agentID, profileStore.BindingWriteRequest{
		Provider:   providerID,
		BaseURL:    configBaseURL,
		Model:      model,
		ProfileRef: profileID,
	})
	if err != nil {
		return ActivateAgentResult{}, err
	}

	return ActivateAgentResult{
		AgentID:  agentID,
		Config:   configPath,
		Provider: providerID,
		Model:    model,
		Restart:  restartHint(agentID, agent),
		Next:     nextStep(u.status.Platform.OS, agentID, agent, model),
		Binding:  binding,
	}, nil
}

func contextError(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return oneerrors.New(oneerrors.Timeout, message, oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

func writeManagedAgentConfig(ctx context.Context, writer configWriter.Writer, agentID string, agent catalog.Agent, path, providerName, baseURL, apiKey, model, smallFastModel string) error {
	switch agent.ConfigAdapter {
	case "codex":
		return writer.WriteCodex(ctx, path, providerName, baseURL, apiKey, model)
	case "claude-code":
		return writer.WriteClaude(ctx, path, baseURL, apiKey, model, smallFastModel)
	case "opencode":
		return writer.WriteOpenAICompatible(ctx, path, "https://opencode.ai/config.json", providerName, baseURL, apiKey, model)
	case "kilo-cli":
		return writer.WriteOpenAICompatible(ctx, path, "https://app.kilo.ai/config.json", providerName, baseURL, apiKey, model)
	case "aider":
		return writer.WriteAider(ctx, path, baseURL, apiKey)
	case "qwen-code":
		return writer.WriteQwen(ctx, path, baseURL, apiKey, model)
	default:
		return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Unsupported auto-config Agent: %s", agentID))
	}
}

func restartHint(agentID string, agent catalog.Agent) string {
	if agent.Command == "" {
		return "Restart " + agentID
	}
	if agentID == "aider" {
		return "Quit any running " + agent.Command + " process, then launch it again from OneAgent"
	}
	return fmt.Sprintf("Quit any running %s process, then start it again", agent.Command)
}

func nextStep(osID, agentID string, agent catalog.Agent, model string) string {
	if agent.ConfigMode != "auto" || agent.Command == "" {
		return ""
	}
	if agentID == "aider" {
		envFile := "~/.oneagent/aider.env"
		if osID == "windows" {
			envFile = `"%USERPROFILE%\.oneagent\aider.env"`
		}
		return fmt.Sprintf("%s --env-file %s --model openai/%s", agent.Command, envFile, model)
	}
	return agent.Command
}
