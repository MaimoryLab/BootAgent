package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	configWriter "github.com/MaimoryLab/BootAgent/internal/config"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// ActivateAgentOptions is the transport-independent input for pointing one
// managed Agent at a Provider. APIKey is deliberately kept inside the use
// case and never appears in ActivateAgentResult.
type ActivateAgentOptions struct {
	AgentID         string
	Provider        string
	APIBaseURL      string
	APIKey          string
	Model           string
	ProfileID       string
	SmallFastModel  string
	ReasoningEffort string
	Context1M       bool
}

// ActivateAgentResult contains only the public outcome needed by the UI. The
// binding itself is persisted separately and is not repeated here.
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
	if err := guideOnlyRejection(agentID, agent); err != nil {
		return ActivateAgentResult{}, err
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
	if apiKey == "" {
		apiKey = target.APIKey
	}
	if apiKey == "" {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	if err := u.providers.SaveKey(ctx, providerID, apiKey); err != nil {
		return ActivateAgentResult{}, err
	}

	// The auto-selected flag only matters to the probe, which has to explain a
	// failure; activation just needs a usable model.
	model, _, err := u.resolveProviderModel(ctx, target, apiKey, options.Model)
	if err != nil {
		return ActivateAgentResult{}, err
	}
	if model == "" {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "model is required")
	}

	protocol := provider.ProtocolForAdapter(agent.ConfigAdapter)
	// The effort in options wins; a Profile-driven activation falls back to what
	// the Profile carries. Resolved before the write so the binding below records
	// what actually took effect, not what the caller happened to pass.
	reasoningEffort := strings.TrimSpace(options.ReasoningEffort)
	context1M := options.Context1M
	if profileID != "" {
		profiles, err := u.profiles.List()
		if err != nil {
			return ActivateAgentResult{}, err
		}
		for _, saved := range profiles {
			if saved.ID != profileID {
				continue
			}
			if saved.Protocol != "" && saved.Protocol != protocol {
				return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Profile %s uses %s API mode, but %s requires %s", profileID, saved.Protocol, agentID, protocol))
			}
			if reasoningEffort == "" {
				reasoningEffort = saved.ReasoningEffort
			}
			context1M = saved.Context1M
		}
	}
	configBaseURL := target.BaseFor(protocol)
	providerName := target.Name
	configPath := configPath(u.status.Home, u.status.Platform.OS, agent)
	if configPath == "" {
		return ActivateAgentResult{}, oneerrors.New(oneerrors.InvalidRequest, "Managed Agent has no configuration path")
	}

	writer := configWriter.NewWriter(u.status.Home, u.status.Platform.OS, u.filesystem)
	if err := writeManagedAgentConfig(ctx, writer, agentID, agent, configPath, dshRouteProviderID(target, options.APIBaseURL), providerName, configBaseURL, apiKey, model, options.SmallFastModel, reasoningEffort, context1M); err != nil {
		return ActivateAgentResult{}, err
	}
	binding, err := u.profiles.WriteAgentBinding(ctx, agentID, profileStore.BindingWriteRequest{
		Provider:        providerID,
		BaseURL:         configBaseURL,
		Model:           model,
		ReasoningEffort: reasoningEffort,
		ProfileRef:      profileID,
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

// guideOnlyRejection refuses to write a managed configuration for an Agent the
// catalog only documents. Named rather than inlined because the catalog currently
// holds no guide-only Agent to reach it through, and an unreachable guard with no
// test is one refactor away from being deleted as dead code.
func guideOnlyRejection(agentID string, agent catalog.Agent) error {
	if agent.ConfigMode == "auto" {
		return nil
	}
	return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("%s is guide-only and has no managed configuration", agentID))
}

func contextError(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return oneerrors.New(oneerrors.Timeout, message, oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

// dshRouteProviderID names the Provider for writeManagedAgentConfig's dsh route
// selection: the built-in ID only while the entry still points at its catalog
// endpoint. An explicit base override aims the Provider somewhere else, which
// dsh's shipped deepseek-official route cannot represent -- its endpoint is
// fixed -- so an overridden DeepSeek entry must fall back to the hand-declared
// route like any other custom endpoint. Custom Providers have no shipped route
// either way and report empty.
func dshRouteProviderID(target provider.Entry, explicitBase string) string {
	if !target.BuiltIn || strings.TrimSpace(explicitBase) != "" {
		return ""
	}
	return target.ID
}

// profileReasoningEffort is the thinking depth a Profile carries, empty when
// the Profile is absent or names none. For callers that do not already hold
// the Profile list; activateAgentLocked resolves inline instead.
func (u *UseCases) profileReasoningEffort(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ""
	}
	profiles, err := u.profiles.List()
	if err != nil {
		return ""
	}
	for _, saved := range profiles {
		if saved.ID == profileID {
			return saved.ReasoningEffort
		}
	}
	return ""
}

func (u *UseCases) profileContext1M(profileID string) bool {
	profiles, err := u.profiles.List()
	if err != nil {
		return false
	}
	for _, saved := range profiles {
		if saved.ID == strings.TrimSpace(profileID) {
			return saved.Context1M
		}
	}
	return false
}

// writeManagedAgentConfig hands the activation to the Agent's config adapter.
//
// reasoningEffort reaches the adapters whose file format documents a place for
// it: Codex (model_reasoning_effort), aider (AIDER_REASONING_EFFORT), the
// OpenCode/Kilo model options, and dsh's shipped official route. The others
// deliberately drop it. Claude Code has no depth scale to write -- its
// documented controls are a thinking-token budget (MAX_THINKING_TOKENS) and an
// always-think boolean, and mapping a five-level scale onto either would
// invent semantics the tool never promised. The remaining adapters write
// config shapes read off one observed version with no documented reasoning
// field, and inventing keys in files those apps own risks corrupting state
// they manage (see WriteZCode).
func writeManagedAgentConfig(ctx context.Context, writer configWriter.Writer, agentID string, agent catalog.Agent, path, providerID, providerName, baseURL, apiKey, model, smallFastModel, reasoningEffort string, context1M bool) error {
	switch agent.ConfigAdapter {
	case "codex":
		return writer.WriteCodex(ctx, path, providerName, baseURL, apiKey, model, reasoningEffort)
	case "claude-code":
		return writer.WriteClaude(ctx, path, baseURL, apiKey, model, smallFastModel)
	case "opencode":
		return writer.WriteOpenAICompatible(ctx, path, "https://opencode.ai/config.json", providerName, baseURL, apiKey, model, reasoningEffort)
	case "kilo-cli":
		return writer.WriteOpenAICompatible(ctx, path, "https://app.kilo.ai/config.json", providerName, baseURL, apiKey, model, reasoningEffort)
	case "openclaw":
		return writer.WriteOpenClaw(ctx, path, providerName, baseURL, apiKey, model)
	case "aider":
		return writer.WriteAider(ctx, path, baseURL, apiKey, reasoningEffort)
	case "dsh":
		// DeepSeek's own service activates through the shipped deepseek-official
		// route rather than a hand-declared bootagent route: that shipped route
		// already carries the endpoint and the model catalog, so declaring one
		// would be a duplicate that misfiles the credential. Only when the Provider
		// is something else -- a gateway or custom endpoint -- does a route have to
		// be declared.
		//
		// reasoningEffort reaches only the official path. A hand-declared pi-ai
		// model carries no reasoning metadata, so dsh rejects every effort against
		// it at request time (UNSUPPORTED_REASONING_EFFORT) -- writing one there
		// would break each request instead of deepening any thinking.
		if providerID == "deepseek" {
			return writer.WriteDSHOfficial(ctx, path, apiKey, model, reasoningEffort)
		}
		return writer.WriteDSH(ctx, path, providerName, baseURL, apiKey, model)
	case "hermes":
		return writer.WriteHermes(ctx, path, baseURL, apiKey, model)
	case "kimi-code":
		return writer.WriteKimiCode(ctx, path, baseURL, apiKey, model, context1M)
	case "workbuddy":
		return writer.WriteWorkBuddy(ctx, path, baseURL, apiKey, model)
	case "zcode":
		return writer.WriteZCode(ctx, path, providerName, baseURL, apiKey, model)
	default:
		return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Unsupported auto-config Agent: %s", agentID))
	}
}

func restartHint(agentID string, agent catalog.Agent) string {
	if agent.Command == "" {
		return "Restart " + agentID
	}
	if agentID == "aider" {
		return "Quit any running " + agent.Command + " process, then launch it again from BootAgent"
	}
	// OpenClaw's gateway is a long-lived process, often registered as a launchd
	// or systemd user service, so "quit and start again" would be wrong advice:
	// the config is re-read on restart of the gateway, not of a foreground CLI.
	if agentID == "openclaw" {
		return fmt.Sprintf("Restart the gateway so it re-reads the config: %s gateway restart", agent.Command)
	}
	// Both files BootAgent writes for dsh are watched: the settings document is
	// hot-reloaded and the credential store hot-publishes external edits, and the
	// adapter re-reads both once per request. Telling the user to restart a running
	// session would be busywork.
	if agentID == "dsh" {
		return "No restart needed: " + agent.Command + " picks up the new endpoint and key on the next request"
	}
	return fmt.Sprintf("Quit any running %s process, then start it again", agent.Command)
}

func nextStep(osID, agentID string, agent catalog.Agent, model string) string {
	if agent.ConfigMode != "auto" || agent.Command == "" {
		return ""
	}
	if agentID == "aider" {
		envFile := "~/.bootagent/aider.env"
		if osID == "windows" {
			envFile = `"%USERPROFILE%\.bootagent\aider.env"`
		}
		return fmt.Sprintf("%s --env-file %s --model openai/%s", agent.Command, envFile, model)
	}
	// The model provider is configured, but a gateway still needs its channels
	// paired before it can do anything. That step is OpenClaw's own interactive
	// flow and stays with the user, so point at it rather than at a bare command
	// that would appear to be all that is left.
	if agentID == "openclaw" {
		return agent.Command + " onboard"
	}
	// dsh boots a profile, and the launcher requires one: the bare command exits
	// nonzero with "--profile <name> is required". `web` is its hardcoded alias for
	// --profile web, the local web app this Agent is configured for. LaunchAgent
	// runs this line in a terminal, so a bare command here is a window that opens
	// only to print an error.
	if agentID == "dsh" {
		return agent.Command + " web"
	}
	return agent.Command
}
