package binding

import (
	"context"

	"github.com/MaimoryLab/BootAgent/internal/app"
)

// MarketplaceService proxies the public skillhub API for the marketplace
// pages. It exists because api.skillhub.cn only echoes CORS headers for
// skillhub's own origins, so the renderer cannot fetch it directly; the Go
// side has no CORS constraint. Payloads pass through as raw JSON strings --
// normalisation stays in the frontend, which already owns it for the bundled
// snapshot.
type MarketplaceService struct{ core *app.UseCases }

func NewMarketplaceService(core *app.UseCases) *MarketplaceService {
	return &MarketplaceService{core: core}
}

// MarketplaceProxyResponse carries one upstream JSON payload verbatim.
type MarketplaceProxyResponse struct {
	Body string `json:"body"`
}

// SkillDetailRequest names the skillhub skill to look up by its public slug.
type SkillDetailRequest struct {
	Slug string `json:"slug"`
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
