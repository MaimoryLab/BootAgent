// Package binding contains transport DTOs and the narrow Wails-facing service
// layer. Business logic belongs in internal/app and is not duplicated here.
package binding

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/app"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/install"
	"github.com/MaimoryLab/BootAgent/internal/process"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// maxInstallTimeoutSeconds caps an explicitly requested timeout. It sits above
// DefaultCommandTimeout on purpose: a ceiling equal to the default would mean a
// caller could not ask for the default explicitly.
const maxInstallTimeoutSeconds = 6 * 60 * 60

type Services struct {
	Status       *StatusService
	Provider     *ProviderService
	Agent        *AgentService
	Profile      *ProfileService
	Runtime      *RuntimeService
	DesktopAgent *DesktopAgentService
	Transfer     *TransferService
	MCP          *MCPService
	Skill        *SkillService
	Conversion   *ConversionService
}

type ServicesOptions struct {
	AfterGetStatus func(app.StatusResponse)
	InstallOutput  process.OutputListener
	Autostart      AutostartCallbacks
}

type AutostartCallbacks struct {
	IsEnabled  func() (bool, error)
	SetEnabled func(bool) error
}

func NewServicesWithOptions(core *app.UseCases, opener BrowserOpener, options ServicesOptions) *Services {
	return &Services{
		Status:       &StatusService{core: core, afterGetStatus: options.AfterGetStatus},
		Provider:     NewProviderService(core, opener),
		Agent:        &AgentService{core: core, onOutput: options.InstallOutput},
		Profile:      NewProfileService(core),
		Runtime:      &RuntimeService{core: core, onOutput: options.InstallOutput, autostart: options.Autostart},
		DesktopAgent: NewDesktopAgentService(core, options.InstallOutput),
		Transfer:     &TransferService{},
		MCP:          NewMCPService(core),
		Skill:        NewSkillService(core),
		Conversion:   NewConversionService(core),
	}
}

type ConversionService struct{ core *app.UseCases }

func NewConversionService(core *app.UseCases) *ConversionService {
	return &ConversionService{core: core}
}
func (s *ConversionService) Get(ctx context.Context) (app.ConversionConfig, error) {
	if err := contextError(ctx); err != nil {
		return app.ConversionConfig{}, err
	}
	if s == nil || s.core == nil {
		return app.ConversionConfig{}, notReady("Conversion service is not configured")
	}
	return s.core.Conversion(ctx)
}
func (s *ConversionService) Save(ctx context.Context, c app.ConversionConfig) (app.ConversionConfig, error) {
	if err := contextError(ctx); err != nil {
		return app.ConversionConfig{}, err
	}
	if s == nil || s.core == nil {
		return app.ConversionConfig{}, notReady("Conversion service is not configured")
	}
	return s.core.SaveConversion(ctx, c)
}

// DesktopAgentService exposes the configured desktop Agent lifecycle. Every
// operation names its target explicitly.
type DesktopAgentService struct {
	core     *app.UseCases
	onOutput process.OutputListener
}

func NewDesktopAgentService(core *app.UseCases, output process.OutputListener) *DesktopAgentService {
	return &DesktopAgentService{core: core, onOutput: output}
}

func (s *DesktopAgentService) GetStatus(ctx context.Context, request DesktopAgentRequest) (app.DesktopAgentStatus, error) {
	if err := contextError(ctx); err != nil {
		return app.DesktopAgentStatus{}, err
	}
	if s == nil || s.core == nil {
		return app.DesktopAgentStatus{}, notReady("Desktop agent service is not configured")
	}
	return s.core.DesktopAgentStatus(ctx, request.AgentID)
}

func (s *DesktopAgentService) Install(ctx context.Context, request DesktopAgentRequest) (app.DesktopAgentActionResult, error) {
	if err := contextError(ctx); err != nil {
		return app.DesktopAgentActionResult{}, err
	}
	if s == nil || s.core == nil {
		return app.DesktopAgentActionResult{}, notReady("Desktop agent service is not configured")
	}
	return s.core.InstallDesktopAgent(ctx, request.AgentID, s.onOutput)
}

func (s *DesktopAgentService) Open(ctx context.Context, request DesktopAgentRequest) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.core == nil {
		return notReady("Desktop agent service is not configured")
	}
	return s.core.OpenDesktopAgent(ctx, request.AgentID)
}

// Configure applies a saved Profile to the selected desktop Agent. The profile
// ID is the only user-supplied value; secrets stay in the Go profile store.
func (s *DesktopAgentService) Configure(ctx context.Context, request DesktopAgentProfileRequest) (app.DesktopAgentProfileResult, error) {
	if err := contextError(ctx); err != nil {
		return app.DesktopAgentProfileResult{}, err
	}
	if s == nil || s.core == nil {
		return app.DesktopAgentProfileResult{}, notReady("Desktop agent service is not configured")
	}
	return s.core.ConfigureDesktopAgent(ctx, request.AgentID, request.ProfileID)
}

// RuntimeService exposes the Node.js and uv bootstrap. It reuses the install
// output listener so the UI can render runtime byte progress alongside Agent
// install output; runtime bootstrap does not emit a fake command line.
type RuntimeService struct {
	core      *app.UseCases
	onOutput  process.OutputListener
	autostart AutostartCallbacks
}

func (s *RuntimeService) ListRuntimes(ctx context.Context) ([]app.RuntimeStatus, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("Runtime service is not configured")
	}
	return s.core.RuntimeStatuses(ctx)
}

func (s *RuntimeService) InstallRuntime(ctx context.Context, request InstallRuntimeRequest) (app.InstallRuntimeResult, error) {
	if err := contextError(ctx); err != nil {
		return app.InstallRuntimeResult{}, err
	}
	if s == nil || s.core == nil {
		return app.InstallRuntimeResult{}, notReady("Runtime service is not configured")
	}
	if strings.TrimSpace(request.Runtime) == "" {
		return app.InstallRuntimeResult{}, oneerrors.New(oneerrors.InvalidRequest, "runtime is required")
	}
	return s.core.InstallRuntime(ctx, app.InstallRuntimeOptions{
		RuntimeID: strings.TrimSpace(request.Runtime),
		Output:    s.onOutput,
	})
}

// GetSettings reads the machine-level download preferences.
func (s *RuntimeService) GetSettings(ctx context.Context) (app.Settings, error) {
	if err := contextError(ctx); err != nil {
		return app.Settings{}, err
	}
	if s == nil || s.core == nil {
		return app.Settings{}, notReady("Runtime service is not configured")
	}
	settings, err := s.core.Settings(ctx)
	if err != nil || s.autostart.IsEnabled == nil {
		return settings, err
	}
	settings.Autostart, err = s.autostart.IsEnabled()
	return settings, err
}

// SaveSettings persists the download preferences and returns what was stored.
func (s *RuntimeService) SaveSettings(ctx context.Context, request app.SettingsPatch) (app.Settings, error) {
	if err := contextError(ctx); err != nil {
		return app.Settings{}, err
	}
	if s == nil || s.core == nil {
		return app.Settings{}, notReady("Runtime service is not configured")
	}
	previous, changed := false, false
	if request.Autostart != nil && s.autostart.SetEnabled != nil {
		if s.autostart.IsEnabled != nil {
			var err error
			previous, err = s.autostart.IsEnabled()
			if err != nil {
				return app.Settings{}, err
			}
			changed = previous != *request.Autostart
		}
		if err := s.autostart.SetEnabled(*request.Autostart); err != nil {
			return app.Settings{}, err
		}
	}
	saved, err := s.core.UpdateSettings(ctx, request)
	if err != nil && changed {
		_ = s.autostart.SetEnabled(previous)
	}
	return saved, err
}

// HelpURL is the published help site. It lives here rather than in the frontend
// so the URL the app opens is not something a compromised or tampered renderer
// can choose -- the same reason OpenRegistration re-resolves a Provider's URL
// instead of accepting one over the bridge.
const HelpURL = "https://bootagentpro.ai/help/"
const GitHubURL = "https://github.com/MaimoryLab/BootAgent"

type StatusService struct {
	core           *app.UseCases
	afterGetStatus func(app.StatusResponse)
}

func (s *StatusService) GetStatus(ctx context.Context) (app.StatusResponse, error) {
	if s == nil || s.core == nil {
		return app.StatusResponse{}, notReady("Status service is not configured")
	}
	status, err := s.core.GetStatus(ctx)
	if err == nil && s.afterGetStatus != nil {
		s.afterGetStatus(status)
	}
	return status, err
}

type BrowserOpener func(string) error

type ProviderService struct {
	opener BrowserOpener
	core   *app.UseCases
}

func NewProviderService(core *app.UseCases, opener BrowserOpener) *ProviderService {
	return &ProviderService{opener: opener, core: core}
}

// OpenHelp opens the published help site in the user's real browser. Not an <a
// target="_blank">: the webview has no tab to open one in, so a link either
// navigates away from the app or does nothing.
func (s *ProviderService) OpenHelp(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.opener == nil {
		return notReady("Desktop browser is not configured")
	}
	if err := s.opener(HelpURL); err != nil {
		return oneerrors.New(oneerrors.InternalError, "Unable to open the help site", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

func (s *ProviderService) OpenGitHub(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.opener == nil {
		return notReady("Desktop browser is not configured")
	}
	if err := s.opener(GitHubURL); err != nil {
		return oneerrors.New(oneerrors.InternalError, "Unable to open the GitHub repository", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

func (s *ProviderService) Probe(ctx context.Context, request ProbeRequest) (ProbeResponse, error) {
	if err := contextError(ctx); err != nil {
		return ProbeResponse{}, err
	}
	if s == nil || s.core == nil {
		return ProbeResponse{}, notReady("Provider probing is not configured")
	}
	result, err := s.core.ProbeProvider(ctx, app.ProviderProbeOptions{
		Provider:         request.Provider,
		APIBaseURL:       request.APIBaseURL,
		APIKey:           request.APIKey,
		Model:            request.Model,
		AgentIDs:         request.Agents,
		AnthropicBaseURL: request.AnthropicBaseURL,
		Draft:            request.Draft,
	})
	if err != nil {
		return ProbeResponse{}, err
	}
	response := probeResponse(result.Primary)
	// Set on the top-level response only: the per-protocol entries all probed the
	// same model, so repeating it there would suggest they could differ.
	response.Model = result.Model
	response.AutoSelectedModel = result.AutoSelectedModel
	response.Protocols = make(map[string]ProbeResponse, len(result.Protocols))
	for protocolID, protocolResult := range result.Protocols {
		response.Protocols[protocolID] = probeResponse(protocolResult)
	}
	return response, nil
}

func (s *ProviderService) ListModels(ctx context.Context, request ModelsRequest) (ModelsResponse, error) {
	if err := contextError(ctx); err != nil {
		return ModelsResponse{}, err
	}
	if s == nil || s.core == nil {
		return ModelsResponse{}, notReady("Model discovery is not configured")
	}
	result, err := s.core.ListProviderModels(ctx, request.Provider, request.APIKey, request.APIBaseURL)
	if err != nil {
		return ModelsResponse{}, err
	}
	return modelsResponse(result), nil
}

func (s *ProviderService) GetProvider(ctx context.Context, request ProviderIDRequest) (provider.Entry, error) {
	if err := contextError(ctx); err != nil {
		return provider.Entry{}, err
	}
	if s == nil || s.core == nil {
		return provider.Entry{}, notReady("Provider service is not configured")
	}
	return s.core.GetProvider(ctx, request.ID)
}

func (s *ProviderService) SaveProvider(ctx context.Context, request SaveProviderRequest) (app.SaveProviderResult, error) {
	if err := contextError(ctx); err != nil {
		return app.SaveProviderResult{}, err
	}
	if s == nil || s.core == nil {
		return app.SaveProviderResult{}, notReady("Provider service is not configured")
	}
	return s.core.SaveProvider(ctx, provider.Entry{
		ID: request.ID, Name: request.Name, Home: request.Home,
		BaseURL: request.BaseURL, AnthropicBaseURL: request.AnthropicBaseURL, APIKey: request.APIKey,
	}, request.Create, request.KeepExistingKey)
}

func (s *ProviderService) DeleteProvider(ctx context.Context, request ProviderIDRequest) (ProviderMutationResponse, error) {
	if err := contextError(ctx); err != nil {
		return ProviderMutationResponse{}, err
	}
	if s == nil || s.core == nil {
		return ProviderMutationResponse{}, notReady("Provider service is not configured")
	}
	if err := s.core.DeleteProvider(ctx, request.ID); err != nil {
		return ProviderMutationResponse{}, err
	}
	return ProviderMutationResponse{OK: true}, nil
}

func (s *ProviderService) OpenRegistration(ctx context.Context, request OpenRegistrationRequest) (OpenRegistrationResponse, error) {
	if err := contextError(ctx); err != nil {
		return OpenRegistrationResponse{}, err
	}
	if s == nil || s.core == nil {
		return OpenRegistrationResponse{}, notReady("Provider service is not configured")
	}
	if s.opener == nil {
		return OpenRegistrationResponse{}, notReady("Desktop browser is not configured")
	}
	entry, err := s.core.GetProvider(ctx, request.Provider)
	if err != nil {
		return OpenRegistrationResponse{}, oneerrors.New(oneerrors.InvalidRequest, "Registration is only available for a configured Provider")
	}
	// Prefer the key page: it is where a user actually obtains a key, whereas
	// Home is a marketing site they then have to navigate. Home remains the
	// fallback so a Provider without a published key page still opens something,
	// and so a user-added Provider keeps working unchanged.
	target := entry.KeyManagementURL
	if target == "" {
		target = entry.Home
	}
	if target == "" {
		return OpenRegistrationResponse{}, oneerrors.New(oneerrors.InvalidRequest, "Registration is only available for a Provider with a home URL")
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return OpenRegistrationResponse{}, oneerrors.New(oneerrors.InvalidRequest, "Provider registration URL is invalid")
	}
	if err := s.opener(target); err != nil {
		return OpenRegistrationResponse{}, oneerrors.New(oneerrors.InternalError, "Unable to open Provider registration", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return OpenRegistrationResponse{OK: true, URL: target, Message: "Provider registration opened"}, nil
}

type AgentService struct {
	core     *app.UseCases
	onOutput process.OutputListener
}

func (s *AgentService) MigrateConversations(ctx context.Context) (app.ConversationMigrationResult, error) {
	if err := contextError(ctx); err != nil {
		return app.ConversationMigrationResult{}, err
	}
	if s == nil || s.core == nil {
		return app.ConversationMigrationResult{}, notReady("Conversation migration is not configured")
	}
	return s.core.MigrateCodexConversations(ctx)
}

func (s *AgentService) Update(ctx context.Context, request UpdateRequest) (app.AgentUpdateResult, error) {
	if err := contextError(ctx); err != nil {
		return app.AgentUpdateResult{}, err
	}
	if s == nil || s.core == nil {
		return app.AgentUpdateResult{}, notReady("Agent update is not configured")
	}
	return s.core.UpdateAgent(ctx, request.AgentID, s.onOutput)
}

func (s *AgentService) Install(ctx context.Context, request InstallRequest) (InstallResponse, error) {
	if err := contextError(ctx); err != nil {
		return InstallResponse{}, err
	}
	if s == nil || s.core == nil {
		return InstallResponse{}, notReady("Agent installation is not configured")
	}
	// The default lives in internal/install so Go owns it alone. The frontend used
	// to send a hardcoded 180, which made this a second source of truth that could
	// disagree silently.
	timeout := install.DefaultCommandTimeout
	if request.Timeout < 0 || request.Timeout > maxInstallTimeoutSeconds {
		return InstallResponse{}, oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("timeout must be an integer between 0 and %d, where 0 selects the default", maxInstallTimeoutSeconds))
	}
	if request.Timeout > 0 {
		timeout = time.Duration(request.Timeout) * time.Second
	}
	result, err := s.core.InstallAgents(ctx, app.InstallAgentsOptions{
		Agents:         append([]string(nil), request.Agents...),
		Provider:       request.Provider,
		APIBaseURL:     request.APIBaseURL,
		APIKey:         request.APIKey,
		Model:          request.Model,
		ProfileID:      request.ProfileID,
		ProfileLabel:   request.ProfileLabel,
		Configure:      request.Configure,
		InstallAgent:   request.InstallAgent,
		CheckAgentOnly: false,
		SkipTest:       request.SkipTest,
		AgentVersion:   request.AgentVersion,
		Timeout:        timeout,
		Registry:       request.Registry,
		Output:         s.onOutput,
	})
	if err != nil {
		return InstallResponse{}, err
	}
	return installResponse(result), nil
}

func (s *AgentService) Activate(ctx context.Context, request ActivateRequest) (ActivateResponse, error) {
	if err := contextError(ctx); err != nil {
		return ActivateResponse{}, err
	}
	if s == nil || s.core == nil {
		return ActivateResponse{}, notReady("Agent activation is not configured")
	}
	result, err := s.core.ActivateAgent(ctx, app.ActivateAgentOptions{
		AgentID:    request.AgentID,
		Provider:   request.Provider,
		APIBaseURL: request.APIBaseURL,
		APIKey:     request.APIKey,
		Model:      request.Model,
		ProfileID:  request.ProfileID,
	})
	if err != nil {
		return ActivateResponse{}, err
	}
	return ActivateResponse{
		OK:       true,
		Agent:    result.AgentID,
		Config:   result.Config,
		Provider: result.Provider,
		Model:    result.Model,
		Restart:  result.Restart,
		Next:     result.Next,
	}, nil
}

// Launch opens a terminal window running one Agent with its BootAgent
// configuration already sourced.
func (s *AgentService) Launch(ctx context.Context, request LaunchRequest) (LaunchResponse, error) {
	if err := contextError(ctx); err != nil {
		return LaunchResponse{}, err
	}
	if s == nil || s.core == nil {
		return LaunchResponse{}, notReady("Agent launch is not configured")
	}
	result, err := s.core.LaunchAgent(ctx, strings.TrimSpace(request.AgentID), strings.TrimSpace(request.WorkingDirectory))
	if err != nil {
		return LaunchResponse{}, err
	}
	return LaunchResponse{OK: true, Agent: result.Agent, Command: result.Command, Terminal: result.Terminal}, nil
}

type ProfileService struct {
	core *app.UseCases
}

func NewProfileService(core *app.UseCases) *ProfileService {
	return &ProfileService{core: core}
}

func (s *ProfileService) ListProfiles(ctx context.Context) ([]app.ProfileSummary, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("Profile listing is not configured")
	}
	return s.core.ListProfiles(ctx)
}

func (s *ProfileService) SaveProfile(ctx context.Context, request SaveProfileRequest) (app.SaveProfileResult, error) {
	if err := contextError(ctx); err != nil {
		return app.SaveProfileResult{}, err
	}
	if s == nil || s.core == nil {
		return app.SaveProfileResult{}, notReady("Profile service is not configured")
	}
	return s.core.SaveProfile(ctx, app.SaveProfileOptions{
		ID:              request.ID,
		Label:           request.Label,
		Provider:        request.Provider,
		APIBaseURL:      request.APIBaseURL,
		APIKey:          request.APIKey,
		Model:           request.Model,
		ReasoningEffort: request.ReasoningEffort,
		Context1M:       request.Context1M,
		ConfigMode:      request.ConfigMode,
		Protocol:        request.Protocol,
	})
}

func (s *ProfileService) DeleteProfile(ctx context.Context, request ProviderIDRequest) (ProviderMutationResponse, error) {
	if err := contextError(ctx); err != nil {
		return ProviderMutationResponse{}, err
	}
	if s == nil || s.core == nil {
		return ProviderMutationResponse{}, notReady("Profile service is not configured")
	}
	if err := s.core.DeleteProfile(ctx, request.ID); err != nil {
		return ProviderMutationResponse{}, err
	}
	return ProviderMutationResponse{OK: true}, nil
}

type ProbeRequest struct {
	Provider   string   `json:"provider"`
	APIBaseURL string   `json:"api_base_url"`
	APIKey     string   `json:"api_key"`
	Model      string   `json:"model"`
	Agents     []string `json:"agents"`
	// AnthropicBaseURL and Draft let the Provider editor test what is on screen
	// before it is saved. Draft is explicit rather than inferred from a non-empty
	// base URL: the wizard also sends a base URL, and silently switching it to
	// draft resolution would stop it reading the stored record.
	AnthropicBaseURL string `json:"anthropic_base_url"`
	Draft            bool   `json:"draft"`
}

type ModelsRequest struct {
	Provider   string `json:"provider"`
	APIBaseURL string `json:"api_base_url"`
	APIKey     string `json:"api_key"`
}

type OpenRegistrationRequest struct {
	Provider string   `json:"provider"`
	Agents   []string `json:"agents"`
}

type DesktopAgentProfileRequest struct {
	AgentID   string `json:"agent_id"`
	ProfileID string `json:"profile_id"`
}

type DesktopAgentRequest struct {
	AgentID string `json:"agent_id"`
}

type ProviderIDRequest struct {
	ID string `json:"id"`
}

type InstallRuntimeRequest struct {
	Runtime string `json:"runtime"`
}

type SaveProviderRequest struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Home             string `json:"home"`
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	APIKey           string `json:"api_key"`
	// Create refuses an ID that already exists rather than overwriting it. It
	// defaults to false so an edit, which legitimately writes over an existing
	// entry, is the behaviour a caller gets without asking.
	Create bool `json:"create"`
	// KeepExistingKey leaves a stored key in place when APIKey is empty, for an
	// import of a file exported without keys. Defaults to false so the Provider
	// editor keeps clearing the key when the user empties the field.
	KeepExistingKey bool `json:"keep_existing_key"`
}

type ProviderMutationResponse struct {
	OK bool `json:"ok"`
}

type OpenRegistrationResponse struct {
	OK      bool   `json:"ok"`
	URL     string `json:"url"`
	Message string `json:"message"`
}

type ProbeResponse struct {
	OK        bool                     `json:"ok"`
	Reachable bool                     `json:"reachable"`
	Status    int                      `json:"status"`
	Message   string                   `json:"message"`
	ErrorCode *string                  `json:"error_code"`
	Retryable bool                     `json:"retryable"`
	Protocol  *string                  `json:"protocol,omitempty"`
	Protocols map[string]ProbeResponse `json:"protocols,omitempty"`
	// Model is the ID that was probed, and AutoSelectedModel says BootAgent chose
	// it rather than the user. A failure on a model we picked is not evidence
	// about the user's key, and the UI has to be able to say which it is.
	Model             string `json:"model,omitempty"`
	AutoSelectedModel bool   `json:"auto_selected_model,omitempty"`
}

type ModelsResponse struct {
	ProbeResponse
	Models []string `json:"models"`
}

type InstallRequest struct {
	Agents       []string `json:"agents"`
	Provider     string   `json:"provider"`
	APIBaseURL   string   `json:"api_base_url"`
	APIKey       string   `json:"api_key"`
	Model        string   `json:"model"`
	ProfileID    string   `json:"profile_id"`
	ProfileLabel string   `json:"profile_label"`
	Configure    bool     `json:"configure"`
	InstallAgent bool     `json:"install_agent"`
	AgentVersion string   `json:"agent_version"`
	SkipTest     bool     `json:"skip_test"`
	Registry     string   `json:"registry"`
	Timeout      int      `json:"timeout"`
}

type AgentInstallResult struct {
	Agent         string   `json:"agent"`
	Status        string   `json:"status"`
	Config        *string  `json:"config,omitempty"`
	Installed     *bool    `json:"installed,omitempty"`
	Version       **string `json:"version,omitempty"`
	LockedVersion **string `json:"lockedVersion,omitempty"`
	Registry      *string  `json:"registry,omitempty"`
	Code          *int     `json:"code,omitempty"`
	ErrorCode     *string  `json:"error_code,omitempty"`
	Message       *string  `json:"message,omitempty"`
	Retryable     bool     `json:"retryable"`
}

type InstallResponse struct {
	OK      bool                     `json:"ok"`
	Code    int                      `json:"code"`
	Results []AgentInstallResult     `json:"results"`
	Log     string                   `json:"log"`
	Next    string                   `json:"next"`
	Probe   *ProbeResponse           `json:"probe"`
	Probes  map[string]ProbeResponse `json:"probes"`
}

type ActivateRequest struct {
	AgentID    string `json:"agent_id"`
	Provider   string `json:"provider"`
	APIBaseURL string `json:"api_base_url"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	ProfileID  string `json:"profile_id"`
}

type ActivateResponse struct {
	OK       bool   `json:"ok"`
	Agent    string `json:"agent"`
	Config   string `json:"config"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Restart  string `json:"restart"`
	Next     string `json:"next"`
}

type LaunchRequest struct {
	AgentID          string `json:"agent_id"`
	WorkingDirectory string `json:"working_directory"`
}

type UpdateRequest struct {
	AgentID string `json:"agent_id"`
}

type LaunchResponse struct {
	OK      bool   `json:"ok"`
	Agent   string `json:"agent"`
	Command string `json:"command"`
	// Terminal is the terminal that actually opened, which can differ from the
	// stored preference when that one is no longer installed.
	Terminal string `json:"terminal"`
}

type SaveProfileRequest struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Provider        string `json:"provider"`
	APIBaseURL      string `json:"api_base_url"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Context1M       bool   `json:"context_1m,omitempty"`
	ConfigMode      string `json:"config_mode"`
	Protocol        string `json:"protocol"`
}

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return oneerrors.New(oneerrors.Timeout, "Request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

func notReady(message string) error {
	return oneerrors.New(oneerrors.InternalError, strings.TrimSpace(message), oneerrors.WithStatus(501))
}

func probeResponse(result provider.ProbeResult) ProbeResponse {
	return ProbeResponse{
		OK:        result.OK,
		Reachable: result.Reachable,
		Status:    result.Status,
		Message:   result.Message,
		ErrorCode: result.ErrorCode,
		Retryable: result.Retryable,
		Protocol:  result.Protocol,
	}
}

func modelsResponse(result provider.ModelsResult) ModelsResponse {
	return ModelsResponse{
		ProbeResponse: ProbeResponse{
			OK:        result.OK,
			Reachable: result.Reachable,
			Status:    result.Status,
			Message:   result.Message,
			ErrorCode: result.ErrorCode,
			Retryable: result.Retryable,
			Protocol:  result.Protocol,
		},
		Models: result.Models,
	}
}

func installResponse(result app.InstallAgentsResult) InstallResponse {
	response := InstallResponse{
		OK:      result.OK,
		Code:    result.Code,
		Log:     result.Log,
		Next:    result.Next,
		Results: make([]AgentInstallResult, 0, len(result.Results)),
		Probes:  make(map[string]ProbeResponse, len(result.Probes)),
	}
	for _, item := range result.Results {
		response.Results = append(response.Results, installResult(item))
	}
	if result.Probe != nil {
		probe := probeResponse(*result.Probe)
		response.Probe = &probe
	}
	for protocolID, verdict := range result.Probes {
		response.Probes[protocolID] = probeResponse(verdict)
	}
	return response
}

func installResult(item app.AgentInstallResult) AgentInstallResult {
	result := AgentInstallResult{
		Agent:     item.Agent,
		Status:    item.Status,
		Retryable: item.Retryable,
	}
	if item.Status == "configured" || item.Status == "skipped" || item.Status == "installed" {
		if !item.IsCheckOnly() {
			result.Config = new(item.Config)
		}
		result.Installed = new(item.Installed)
		result.Version = nullableStringPointer(item.Version)
		result.LockedVersion = nullableStringPointer(item.LockedVersion)
		if item.Registry != "" {
			result.Registry = new(item.Registry)
		}
	}
	switch item.Status {
	case "failed":
		result.Code = new(item.Code)
		result.ErrorCode = new(item.ErrorCode)
		result.Message = new(item.Message)
	case "guide-only":
		result.Message = new(item.Message)
	}
	return result
}

func nullableStringPointer(value string) **string {
	if value == "" {
		var nilValue *string
		return &nilValue
	}
	valuePointer := &value
	return &valuePointer
}
