package app

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// AgentConversionResult is ActivateAgent's result for an Agent pointed at the
// local adapter, with the checks that ran before anything was written.
type AgentConversionResult struct {
	ActivateAgentResult
	UpstreamVerified bool `json:"upstreamVerified"`
	EndToEndVerified bool `json:"endToEndVerified"`
}

// convertibleProtocol reports whether the adapter converts from `protocol`.
// An Agent already speaking Chat Completions has nothing to gain from it.
func convertibleProtocol(protocol string) bool {
	return protocol == provider.ProtocolAnthropic || protocol == provider.ProtocolResponses
}

// conversionFallbackWorthTrying reports whether a native probe failed in a way
// Chat Completions could get around: the endpoint answered, but not for this
// protocol. A rejected key, a rate limit, a server error or no answer at all
// would fail the same way over Chat Completions, so a second request would be
// spent for nothing.
func conversionFallbackWorthTrying(result provider.ProbeResult) bool {
	if result.OK || !result.Reachable {
		return false
	}
	if result.ErrorCode != nil && *result.ErrorCode == oneerrors.APIKeyRejected {
		return false
	}
	return result.Status >= 400 && result.Status < 500 && result.Status != http.StatusTooManyRequests && result.Status != http.StatusRequestTimeout
}

// ConfigureAgentWithConversion points a command-line Agent at the local
// adapter, which forwards to a Chat Completions Profile. It is the CLI
// counterpart of ConfigureDesktopAgentWithConversion: the upstream is checked
// at the moment of the change, the adapter is started and verified end to end,
// and only then is the Agent's configuration rewritten. Any failure restores
// the adapter, its generated Profiles, the Agent binding and the Agent's own
// files to what they were.
//
// The Agent gets a route of its own, so this never changes where any other
// Agent's requests go.
//
// Both checks are billed requests, which is why this runs only from a button.
func (u *UseCases) ConfigureAgentWithConversion(ctx context.Context, agentID, profileID string) (result AgentConversionResult, err error) {
	agentID, profileID = strings.TrimSpace(agentID), strings.TrimSpace(profileID)
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return result, err
	}
	agent, ok := manifest.Agents[agentID]
	if !ok || agent.ConfigMode != "auto" {
		return result, oneerrors.New(oneerrors.InvalidRequest, "Unknown configurable Agent: "+agentID)
	}
	if !convertibleProtocol(provider.ProtocolForAdapter(agent.ConfigAdapter)) {
		return result, oneerrors.New(oneerrors.InvalidRequest, agent.Name+" already uses Chat Completions and does not need compatibility mode")
	}
	profile := u.profileByID(profileID)
	if profile.ID == "" {
		return result, oneerrors.New(oneerrors.InvalidRequest, "Profile not found: "+profileID)
	}
	if profile.Protocol != provider.ProtocolOpenAI {
		return result, oneerrors.New(oneerrors.InvalidRequest, "Compatibility mode forwards to a Chat Completions Profile")
	}

	u.conversionMu.Lock()
	defer u.conversionMu.Unlock()
	target, err := u.providers.Get(profile.Provider)
	if err != nil {
		return result, err
	}
	model := profileModel(profile)
	if model == "" || strings.TrimSpace(target.APIKey) == "" {
		return result, oneerrors.New(oneerrors.InvalidRequest, "Profile requires a model and Provider API key")
	}
	// Live, not cached: the upstream has to work at the moment the Agent is
	// changed, as for Claude Desktop.
	verdicts, err := u.probeProtocols(ctx, []string{provider.ProtocolOpenAI}, target.APIKey, model, target.BaseFor)
	if err != nil {
		return result, err
	}
	if chat := verdicts[provider.ProtocolOpenAI]; !chat.OK {
		code := oneerrors.ProtocolUnsupported
		if chat.ErrorCode != nil && *chat.ErrorCode != "" {
			code = *chat.ErrorCode
		}
		return result, oneerrors.New(code, chat.Message)
	}

	previous, err := u.Conversion(ctx)
	if err != nil {
		return result, err
	}
	paths, err := u.agentConversionPaths(agentID, agent)
	if err != nil {
		return result, err
	}
	snapshots, err := snapshotManagedFiles(paths)
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		err = u.rollbackConversionFailure(ctx, err, previous, snapshots)
	}()

	config, key, err := u.ensureConversionRoute(ctx, agentID, profileID)
	if err != nil {
		return result, err
	}
	result.UpstreamVerified = true
	if err = verifyLocalConversion(ctx, config.Listen, key, config.AnthropicModel); err != nil {
		return result, oneerrors.New(oneerrors.ConversionVerificationFailed, "Local protocol adaptation verification failed", oneerrors.WithStatus(502), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	result.EndToEndVerified = true
	converter := u.profileByID(converterRouteID(agentID))
	if result.ActivateAgentResult, err = u.ActivateAgent(ctx, ActivateAgentOptions{
		AgentID:   agentID,
		Provider:  converter.Provider,
		Model:     profileModel(converter),
		ProfileID: converter.ID,
	}); err != nil {
		return result, err
	}
	committed = true
	return result, nil
}

// agentConversionPaths is everything ConfigureAgentWithConversion may write:
// the adapter's own state, the Agent's binding, and the Agent's configuration
// files. Codex keeps its key beside config.toml in auth.json.
func (u *UseCases) agentConversionPaths(agentID string, agent catalog.Agent) ([]managedFileSnapshot, error) {
	c, err := u.Conversion(context.Background())
	if err != nil {
		return nil, err
	}
	paths, err := u.conversionSnapshotPaths(c, agentID)
	if err != nil {
		return nil, err
	}
	bindingPath, err := u.profiles.AgentBindingPath(agentID)
	if err != nil {
		return nil, err
	}
	paths = append(paths, managedFileSnapshot{path: bindingPath})
	config := configPath(u.status.Home, u.status.Platform.OS, agent)
	if config == "" {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "Managed Agent has no configuration path")
	}
	paths = append(paths, managedFileSnapshot{path: config, secret: true})
	if agent.ConfigAdapter == "codex" {
		paths = append(paths, managedFileSnapshot{path: filepath.Join(filepath.Dir(config), "auth.json"), secret: true})
	}
	return paths, nil
}
