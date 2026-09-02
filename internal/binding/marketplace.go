package binding

import (
	"context"
	"net"
	"net/url"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/app"
	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

// MarketplaceService proxies the public SkillHub API for marketplace pages.
// It exists because api.skillhub.cn only echoes CORS headers for SkillHub's
// own origins, so the renderer cannot fetch it directly. List and README
// payloads remain bounded proxy responses; MCP detail metadata is normalized
// into catalog.MarketplaceItem before it crosses the binding.
type MarketplaceService struct {
	core   *app.UseCases
	opener BrowserOpener
}

func NewMarketplaceService(core *app.UseCases, opener BrowserOpener) *MarketplaceService {
	return &MarketplaceService{core: core, opener: opener}
}

// Catalog returns the embedded marketplace snapshot. It is parsed and
// validated by the catalog package, keeping the renderer free of a second
// static copy of the market data.
func (s *MarketplaceService) Catalog(ctx context.Context) (catalog.MarketplaceManifest, error) {
	if err := contextError(ctx); err != nil {
		return catalog.MarketplaceManifest{}, err
	}
	return catalog.LoadEmbeddedMarketplace()
}

// MarketplaceProxyResponse carries one upstream JSON payload verbatim.
type MarketplaceProxyResponse struct {
	Body string `json:"body"`
}

// SkillDetailRequest names the skillhub skill to look up by its public slug.
type SkillDetailRequest struct {
	Slug string `json:"slug"`
}

// MCPServerDetailRequest names an MCP Server by its public SkillHub slug.
// Keeping this request distinct from SkillDetailRequest makes the generated
// binding self-documenting and prevents a caller from confusing the two
// upstream resource families.
type MCPServerDetailRequest struct {
	Slug string `json:"slug"`
}

// MCPServersDirectoryDetailRequest names a server in the public
// mcpservers.org directory. Path is an owner/name path (or a single slug),
// never an arbitrary URL; the app layer validates and canonicalizes it.
type MCPServersDirectoryDetailRequest struct {
	Path string `json:"path"`
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

func (s *MarketplaceService) FetchMCPServerDetail(ctx context.Context, request MCPServerDetailRequest) (catalog.MarketplaceItem, error) {
	if err := contextError(ctx); err != nil {
		return catalog.MarketplaceItem{}, err
	}
	if s == nil || s.core == nil {
		return catalog.MarketplaceItem{}, notReady("Marketplace service is not configured")
	}
	// The app layer normalizes the upstream response into the same contract used
	// by the list adapter, so the renderer can merge it without a second schema.
	return s.core.FetchMarketplaceMCPServerDetail(ctx, request.Slug)
}

func (s *MarketplaceService) FetchMCPServerReadme(ctx context.Context, request MCPServerDetailRequest) (MarketplaceProxyResponse, error) {
	if err := contextError(ctx); err != nil {
		return MarketplaceProxyResponse{}, err
	}
	if s == nil || s.core == nil {
		return MarketplaceProxyResponse{}, notReady("Marketplace service is not configured")
	}
	body, err := s.core.FetchMarketplaceMCPServerReadme(ctx, request.Slug)
	if err != nil {
		return MarketplaceProxyResponse{}, err
	}
	return MarketplaceProxyResponse{Body: body}, nil
}

// FetchMCPServersDirectoryDetail loads live metadata from mcpservers.org.
func (s *MarketplaceService) FetchMCPServersDirectoryDetail(ctx context.Context, request MCPServersDirectoryDetailRequest) (catalog.MarketplaceItem, error) {
	if err := contextError(ctx); err != nil {
		return catalog.MarketplaceItem{}, err
	}
	if s == nil || s.core == nil {
		return catalog.MarketplaceItem{}, notReady("Marketplace service is not configured")
	}
	return s.core.FetchMarketplaceMCPServersDirectoryDetail(ctx, request.Path)
}

// FetchMCPServersDirectoryReadme proxies the Markdown document published by
// mcpservers.org and keeps external HTML out of the renderer.
func (s *MarketplaceService) FetchMCPServersDirectoryReadme(ctx context.Context, request MCPServersDirectoryDetailRequest) (MarketplaceProxyResponse, error) {
	if err := contextError(ctx); err != nil {
		return MarketplaceProxyResponse{}, err
	}
	if s == nil || s.core == nil {
		return MarketplaceProxyResponse{}, notReady("Marketplace service is not configured")
	}
	body, err := s.core.FetchMarketplaceMCPServersDirectoryReadme(ctx, request.Path)
	if err != nil {
		return MarketplaceProxyResponse{}, err
	}
	return MarketplaceProxyResponse{Body: body}, nil
}

// DiscoverSources returns the dynamic SkillHub and MCP Servers catalog. The
// response uses the same MarketplaceItem contract as the embedded manifest.
func (s *MarketplaceService) DiscoverSources(ctx context.Context, options app.MarketplaceDiscoverOptions) (app.MarketplaceDynamicResult, error) {
	if err := contextError(ctx); err != nil {
		return app.MarketplaceDynamicResult{}, err
	}
	if s == nil || s.core == nil {
		return app.MarketplaceDynamicResult{}, notReady("Marketplace service is not configured")
	}
	return s.core.DiscoverMarketplaceSources(ctx, options)
}

// RecommendationAgents lists only installed CLI Agents whose non-interactive
// mode can be constrained to recommendation output without write tools.
func (s *MarketplaceService) RecommendationAgents(ctx context.Context) ([]app.MarketplaceRecommendationAgent, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("Marketplace recommendation is not configured")
	}
	return s.core.MarketplaceRecommendationAgents(ctx)
}

// Recommend passes a bounded, public catalog projection to the selected local
// Agent. The app layer validates both the projection and the returned item IDs.
func (s *MarketplaceService) Recommend(ctx context.Context, request app.MarketplaceRecommendRequest) (app.MarketplaceRecommendResult, error) {
	if err := contextError(ctx); err != nil {
		return app.MarketplaceRecommendResult{}, err
	}
	if s == nil || s.core == nil {
		return app.MarketplaceRecommendResult{}, notReady("Marketplace recommendation is not configured")
	}
	return s.core.RecommendMarketplace(ctx, request)
}

func (s *MarketplaceService) ListRecommendationHistory(ctx context.Context) ([]app.MarketplaceRecommendationHistory, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("Marketplace history is not configured")
	}
	return s.core.ListMarketplaceRecommendationHistory(ctx)
}

func (s *MarketplaceService) SaveRecommendationHistory(ctx context.Context, record app.MarketplaceRecommendationHistory) (app.MarketplaceRecommendationHistory, error) {
	if err := contextError(ctx); err != nil {
		return app.MarketplaceRecommendationHistory{}, err
	}
	if s == nil || s.core == nil {
		return app.MarketplaceRecommendationHistory{}, notReady("Marketplace history is not configured")
	}
	return s.core.SaveMarketplaceRecommendationHistory(ctx, record)
}

func (s *MarketplaceService) DeleteRecommendationHistory(ctx context.Context, id string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.core == nil {
		return notReady("Marketplace history is not configured")
	}
	return s.core.DeleteMarketplaceRecommendationHistory(ctx, id)
}

func (s *MarketplaceService) ClearRecommendationHistory(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.core == nil {
		return notReady("Marketplace history is not configured")
	}
	return s.core.ClearMarketplaceRecommendationHistory(ctx)
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
