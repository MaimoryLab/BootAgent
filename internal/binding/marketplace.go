package binding

import (
	"context"
	"net"
	"net/url"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/app"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

// MarketplaceService proxies the public skillhub API for the marketplace
// pages. It exists because api.skillhub.cn only echoes CORS headers for
// skillhub's own origins, so the renderer cannot fetch it directly; the Go
// side has no CORS constraint. Payloads pass through as raw JSON strings --
// normalisation stays in the frontend, which already owns it for the bundled
// snapshot.
type MarketplaceService struct {
	core   *app.UseCases
	opener BrowserOpener
}

func NewMarketplaceService(core *app.UseCases, opener BrowserOpener) *MarketplaceService {
	return &MarketplaceService{core: core, opener: opener}
}

// MarketplaceProxyResponse carries one upstream JSON payload verbatim.
type MarketplaceProxyResponse struct {
	Body string `json:"body"`
}

// SkillDetailRequest names the skillhub skill to look up by its public slug.
type SkillDetailRequest struct {
	Slug string `json:"slug"`
}

// OpenExternalRequest carries a user-clicked marketplace link. Only public
// HTTPS destinations are accepted before handing the URL to the OS browser.
type OpenExternalRequest struct {
	URL string `json:"url"`
}

func (s *MarketplaceService) FetchShowcase(ctx context.Context) (MarketplaceProxyResponse, error) {
	if err := contextError(ctx); err != nil {
		return MarketplaceProxyResponse{}, err
	}
	if s == nil || s.core == nil {
		return MarketplaceProxyResponse{}, notReady("Marketplace service is not configured")
	}
	body, err := s.core.FetchMarketplaceShowcase(ctx)
	if err != nil {
		return MarketplaceProxyResponse{}, err
	}
	return MarketplaceProxyResponse{Body: body}, nil
}

func (s *MarketplaceService) FetchSkillDetail(ctx context.Context, request SkillDetailRequest) (MarketplaceProxyResponse, error) {
	if err := contextError(ctx); err != nil {
		return MarketplaceProxyResponse{}, err
	}
	if s == nil || s.core == nil {
		return MarketplaceProxyResponse{}, notReady("Marketplace service is not configured")
	}
	body, err := s.core.FetchMarketplaceSkillDetail(ctx, request.Slug)
	if err != nil {
		return MarketplaceProxyResponse{}, err
	}
	return MarketplaceProxyResponse{Body: body}, nil
}

func (s *MarketplaceService) FetchSkillFile(ctx context.Context, request SkillDetailRequest) (MarketplaceProxyResponse, error) {
	if err := contextError(ctx); err != nil {
		return MarketplaceProxyResponse{}, err
	}
	if s == nil || s.core == nil {
		return MarketplaceProxyResponse{}, notReady("Marketplace service is not configured")
	}
	body, err := s.core.FetchMarketplaceSkillFile(ctx, request.Slug)
	if err != nil {
		return MarketplaceProxyResponse{}, err
	}
	return MarketplaceProxyResponse{Body: body}, nil
}

func (s *MarketplaceService) OpenExternal(ctx context.Context, request OpenExternalRequest) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.opener == nil {
		return notReady("Desktop browser is not configured")
	}
	parsed, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || !publicMarketplaceHost(parsed.Hostname()) {
		return oneerrors.New(oneerrors.InvalidRequest, "Marketplace link must use HTTPS on a public host")
	}
	if err := s.opener(parsed.String()); err != nil {
		return oneerrors.New(oneerrors.InternalError, "Unable to open the marketplace link", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

func publicMarketplaceHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.Contains(host, "%") {
		return false
	}
	if address := net.ParseIP(host); address != nil {
		return !address.IsLoopback() && !address.IsPrivate() && !address.IsLinkLocalUnicast() && !address.IsUnspecified()
	}
	return true
}
