// Package binding contains transport DTOs and the narrow Wails-facing service
// layer. Business logic belongs in internal/app and is not duplicated here.
package binding

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

type Services struct {
	Status   *StatusService
	Provider *ProviderService
	Agent    *AgentService
	Profile  *ProfileService
}

func NewServices(core *app.UseCases, opener BrowserOpener) *Services {
	return &Services{
		Status:   &StatusService{core: core},
		Provider: NewProviderService(core, opener),
		Agent:    NewAgentService(core),
		Profile:  NewProfileService(core),
	}
}

type StatusService struct {
	core *app.UseCases
}

func (s *StatusService) GetStatus(ctx context.Context) (app.StatusResponse, error) {
	if s == nil || s.core == nil {
		return app.StatusResponse{}, notReady("Status service is not configured")
	}
	return s.core.GetStatus(ctx)
}

type BrowserOpener func(string) error

type ProviderService struct {
	opener BrowserOpener
	core   *app.UseCases
}

func NewProviderService(core *app.UseCases, opener BrowserOpener) *ProviderService {
	return &ProviderService{opener: opener, core: core}
}

func (s *ProviderService) Probe(ctx context.Context, request ProbeRequest) (ProbeResponse, error) {
	if err := contextError(ctx); err != nil {
		return ProbeResponse{}, err
	}
	if s == nil || s.core == nil {
		return ProbeResponse{}, notReady("Provider probing is not configured")
	}
	result, err := s.core.ProbeProvider(ctx, app.ProviderProbeOptions{
		Provider:   request.Provider,
		APIBaseURL: request.APIBaseURL,
		APIKey:     request.APIKey,
		Model:      request.Model,
		AgentIDs:   request.Agents,
	})
	if err != nil {
		return ProbeResponse{}, err
	}
	response := probeResponse(result.Primary)
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

func (s *ProviderService) OpenRegistration(ctx context.Context, request OpenRegistrationRequest) (OpenRegistrationResponse, error) {
	if err := contextError(ctx); err != nil {
		return OpenRegistrationResponse{}, err
	}
	provider, ok := catalog.ProviderByID(request.Provider)
	if !ok {
		return OpenRegistrationResponse{}, oneerrors.New(oneerrors.InvalidRequest, "Registration is only available for an allowlisted Provider")
	}
	parsed, err := url.Parse(provider.Home)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return OpenRegistrationResponse{}, oneerrors.New(oneerrors.InvalidRequest, "Provider registration URL is invalid")
	}
	if s == nil || s.opener == nil {
		return OpenRegistrationResponse{OK: true, URL: provider.Home, Message: "Provider registration URL validated"}, nil
	}
	if err := s.opener(provider.Home); err != nil {
		return OpenRegistrationResponse{}, oneerrors.New(oneerrors.InternalError, "Unable to open Provider registration", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return OpenRegistrationResponse{OK: true, URL: provider.Home, Message: "Provider registration opened"}, nil
}

type AgentService struct {
	core *app.UseCases
}

func NewAgentService(core *app.UseCases) *AgentService {
	return &AgentService{core: core}
}

func (s *AgentService) Install(ctx context.Context, request InstallRequest) (InstallResponse, error) {
	if err := contextError(ctx); err != nil {
		return InstallResponse{}, err
	}
	if s == nil || s.core == nil {
		return InstallResponse{}, notReady("Agent installation is not configured")
	}
	timeout := 180 * time.Second
	if request.Timeout < 0 || request.Timeout > 3600 {
		return InstallResponse{}, oneerrors.New(oneerrors.InvalidRequest, "timeout must be an integer between 1 and 3600")
	}
	if request.Timeout > 0 {
		timeout = time.Duration(request.Timeout) * time.Second
	}
	result, err := s.core.InstallAgents(ctx, app.InstallAgentsOptions{
		Agents:         append([]string(nil), request.Agents...),
		ProfileAgents:  append([]string(nil), request.ProfileAgents...),
		Provider:       request.Provider,
		APIBaseURL:     request.APIBaseURL,
		APIKey:         request.APIKey,
		Model:          request.Model,
		SmallFastModel: request.SmallFastModel,
		ProfileID:      request.ProfileID,
		Configure:      request.Configure,
		InstallAgent:   request.InstallAgent,
		CheckAgentOnly: false,
		SkipTest:       request.SkipTest,
		LockedVersion:  request.LockedVersion,
		Latest:         request.Latest,
		Timeout:        timeout,
		Registry:       request.Registry,
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
		AgentID:        request.AgentID,
		Provider:       request.Provider,
		APIBaseURL:     request.APIBaseURL,
		APIKey:         request.APIKey,
		Model:          request.Model,
		ProfileID:      request.ProfileID,
		SmallFastModel: request.SmallFastModel,
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

func (s *ProfileService) SaveProfile(ctx context.Context, request SaveProfileRequest) (app.ProfileSummary, error) {
	if err := contextError(ctx); err != nil {
		return app.ProfileSummary{}, err
	}
	if s == nil || s.core == nil {
		return app.ProfileSummary{}, notReady("Profile service is not configured")
	}
	return s.core.SaveProfile(ctx, app.SaveProfileOptions{
		ID:         request.ID,
		Label:      request.Label,
		Provider:   request.Provider,
		APIBaseURL: request.APIBaseURL,
		APIKey:     request.APIKey,
		Model:      request.Model,
		ConfigMode: request.ConfigMode,
		AgentIDs:   append([]string(nil), request.AgentIDs...),
	})
}

type ProbeRequest struct {
	Provider   string   `json:"provider"`
	APIBaseURL string   `json:"api_base_url"`
	APIKey     string   `json:"api_key"`
	Model      string   `json:"model"`
	Agents     []string `json:"agents"`
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
}

type ModelsResponse struct {
	ProbeResponse
	Models []string `json:"models"`
}

type InstallRequest struct {
	Agents         []string `json:"agents"`
	ProfileAgents  []string `json:"profile_agents"`
	Provider       string   `json:"provider"`
	APIBaseURL     string   `json:"api_base_url"`
	APIKey         string   `json:"api_key"`
	Model          string   `json:"model"`
	SmallFastModel string   `json:"small_fast_model"`
	ProfileID      string   `json:"profile_id"`
	Configure      bool     `json:"configure"`
	InstallAgent   bool     `json:"install_agent"`
	LockedVersion  bool     `json:"locked_version"`
	Latest         bool     `json:"latest"`
	SkipTest       bool     `json:"skip_test"`
	Registry       string   `json:"registry"`
	Timeout        int      `json:"timeout"`
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
	AgentID        string `json:"agent_id"`
	Provider       string `json:"provider"`
	APIBaseURL     string `json:"api_base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	ProfileID      string `json:"profile_id"`
	SmallFastModel string `json:"small_fast_model"`
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

type SaveProfileRequest struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Provider   string   `json:"provider"`
	APIBaseURL string   `json:"api_base_url"`
	APIKey     string   `json:"api_key"`
	Model      string   `json:"model"`
	ConfigMode string   `json:"config_mode"`
	AgentIDs   []string `json:"agent_ids"`
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
