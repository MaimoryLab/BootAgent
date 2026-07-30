// Package binding contains transport DTOs and the narrow Wails-facing service
// layer. Business logic belongs in internal/app and is not duplicated here.
package binding

import (
	"context"
	"net/url"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
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
		Provider: &ProviderService{opener: opener},
		Agent:    &AgentService{},
		Profile:  &ProfileService{},
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
}

func (s *ProviderService) ListProviders(ctx context.Context) (map[string]catalog.Provider, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return catalog.PublicProviders(), nil
}

func (s *ProviderService) Probe(ctx context.Context, request ProbeRequest) (ProbeResponse, error) {
	if err := contextError(ctx); err != nil {
		return ProbeResponse{}, err
	}
	return ProbeResponse{}, notReady("Provider probing is not available in the migration foundation")
}

func (s *ProviderService) ListModels(ctx context.Context, request ModelsRequest) (ModelsResponse, error) {
	if err := contextError(ctx); err != nil {
		return ModelsResponse{}, err
	}
	return ModelsResponse{}, notReady("Model discovery is not available in the migration foundation")
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

type AgentService struct{}

func (s *AgentService) Install(ctx context.Context, request InstallRequest) (InstallResponse, error) {
	if err := contextError(ctx); err != nil {
		return InstallResponse{}, err
	}
	return InstallResponse{}, notReady("Agent installation is not available in the migration foundation")
}

func (s *AgentService) Activate(ctx context.Context, request ActivateRequest) (ActivateResponse, error) {
	if err := contextError(ctx); err != nil {
		return ActivateResponse{}, err
	}
	return ActivateResponse{}, notReady("Agent activation is not available in the migration foundation")
}

type ProfileService struct{}

func (s *ProfileService) ListProfiles(ctx context.Context) ([]app.ProfileSummary, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return []app.ProfileSummary{}, nil
}

func (s *ProfileService) SaveProfile(ctx context.Context, request SaveProfileRequest) (app.ProfileSummary, error) {
	if err := contextError(ctx); err != nil {
		return app.ProfileSummary{}, err
	}
	return app.ProfileSummary{}, notReady("Profile writes are not available in the migration foundation")
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
	OK        bool   `json:"ok"`
	Reachable bool   `json:"reachable"`
	Status    int    `json:"status"`
	Message   string `json:"message"`
	ErrorCode string `json:"error_code"`
	Retryable bool   `json:"retryable"`
	Protocol  string `json:"protocol"`
}

type ModelsResponse struct {
	ProbeResponse
	Models []string `json:"models"`
}

type InstallRequest struct {
	Agents         []string `json:"agents"`
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
	Timeout        int      `json:"timeout"`
}

type InstallResponse struct {
	OK      bool   `json:"ok"`
	Code    int    `json:"code"`
	Results []any  `json:"results"`
	Log     string `json:"log"`
	Next    string `json:"next"`
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
