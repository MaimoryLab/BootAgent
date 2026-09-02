package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

// The marketplace pages read the public skillhub API, but the renderer cannot
// call it directly: api.skillhub.cn only echoes CORS headers for skillhub's
// own origins, so every browser-side fetch is blocked and the marketplace
// would be stuck on its bundled snapshot. These use cases proxy the two GET
// endpoints instead -- a server-to-server request has no CORS to satisfy.
//
// List responses still pass through as raw JSON strings to the dynamic catalog
// adapter. Detail responses are normalized before crossing the binding so the
// renderer receives the same MarketplaceItem contract for static and live
// cards. Response content is never logged (metadata-only logging is a product
// commitment).
const (
	marketplaceShowcaseURL      = "https://api.skillhub.cn/api/v1/showcase/hot"
	marketplaceSkillURLBase     = "https://api.skillhub.cn/api/v1/skills/"
	marketplaceMCPServerURLBase = "https://api.skillhub.cn/api/v1/mcp/servers/"
	// marketplaceTimeout bounds one upstream request end to end.
	marketplaceTimeout = 10 * time.Second
	// marketplaceMaxBody caps a proxied payload. The showcase feed is well
	// under 1MB today; 4MB leaves room to grow while keeping a misbehaving
	// upstream from filling memory through the bridge.
	marketplaceMaxBody = 4 << 20
)

// marketplaceSlugPattern accepts skillhub skill slugs only. Anything else --
// path separators, a leading dot, uppercase, spaces, an overlong value -- is
// rejected before the slug can reach the request path.
var marketplaceSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// FetchMarketplaceShowcase proxies the skillhub showcase API. The renderer
// cannot call it directly: api.skillhub.cn only echoes CORS for skillhub's
// own origins.
func (u *UseCases) FetchMarketplaceShowcase(ctx context.Context) (string, error) {
	return u.fetchMarketplace(ctx, marketplaceShowcaseURL, "application/json")
}

// FetchMarketplaceSkillDetail proxies one skill's public detail record. The
// slug is the only renderer-supplied value and is validated before it is
// escaped into the URL path.
func (u *UseCases) FetchMarketplaceSkillDetail(ctx context.Context, slug string) (string, error) {
	if !marketplaceSlugPattern.MatchString(slug) {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Invalid marketplace skill slug")
	}
	return u.fetchMarketplace(ctx, marketplaceSkillURLBase+url.PathEscape(slug), "application/json")
}

// FetchMarketplaceSkillFile returns the latest SKILL.md published for a skill.
// SkillHub's detail payload intentionally contains metadata only; the file API
// is the source of the README-equivalent content shown in the marketplace.
func (u *UseCases) FetchMarketplaceSkillFile(ctx context.Context, slug string) (string, error) {
	if !marketplaceSlugPattern.MatchString(slug) {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Invalid marketplace skill slug")
	}
	target := marketplaceSkillURLBase + url.PathEscape(slug) + "/file?path=SKILL.md"
	return u.fetchMarketplace(ctx, target, "text/markdown, text/plain;q=0.9")
}

// FetchMarketplaceMCPServerDetail returns one MCP Server's live metadata in
// the same normalized contract used by the dynamic list adapter. The list
// endpoint intentionally keeps cards small; the detail endpoint is loaded only
// after a user opens a card so every card can retain the same compact first-page
// cost while its detail view still has authoritative links/stats.
func (u *UseCases) FetchMarketplaceMCPServerDetail(ctx context.Context, slug string) (catalog.MarketplaceItem, error) {
	if !marketplaceSlugPattern.MatchString(slug) {
		return catalog.MarketplaceItem{}, oneerrors.New(oneerrors.InvalidRequest, "Invalid marketplace MCP server slug")
	}
	body, err := u.fetchMarketplace(ctx, marketplaceMCPServerURLBase+url.PathEscape(slug), "application/json")
	if err != nil {
		return catalog.MarketplaceItem{}, err
	}
	item, err := parseMCPServerDetail([]byte(body))
	if err != nil {
		return catalog.MarketplaceItem{}, oneerrors.New(oneerrors.InternalError, "Could not parse MCP Server details", oneerrors.WithCause(err))
	}
	return item, nil
}

// FetchMarketplaceMCPServerReadme proxies the README published with one MCP
// Server. It follows the upstream API's redirect to its object store on the Go
// side, avoiding renderer CORS and keeping the raw document out of URL state.
func (u *UseCases) FetchMarketplaceMCPServerReadme(ctx context.Context, slug string) (string, error) {
	if !marketplaceSlugPattern.MatchString(slug) {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Invalid marketplace MCP server slug")
	}
	target := marketplaceMCPServerURLBase + url.PathEscape(slug) + "/readme"
	return u.fetchMarketplace(ctx, target, "text/markdown, text/plain;q=0.9")
}

func (u *UseCases) fetchMarketplace(ctx context.Context, target, accept string) (string, error) {
	body, _, _, err := u.fetchMarketplaceWithETag(ctx, target, accept, "")
	return body, err
}

func (u *UseCases) fetchMarketplaceWithETag(ctx context.Context, target, accept, etag string) (string, string, bool, error) {
	if u == nil {
		return "", "", false, oneerrors.New(oneerrors.InternalError, "Marketplace proxy is not configured", oneerrors.WithStatus(501))
	}
	// httpDoer is only set when a caller injected one, which in production is
	// nobody: it exists so tests can answer without a network. The fallback
	// matches latestAgentVersions.
	client := u.httpDoer
	if client == nil {
		client = marketplaceHTTPClient()
	}
	requestCtx, cancel := context.WithTimeout(ctx, marketplaceTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, target, nil)
	if err != nil {
		return "", "", false, oneerrors.New(oneerrors.InternalError, "Could not build the marketplace request", oneerrors.WithCause(err))
	}
	request.Header.Set("Accept", accept)
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", "", false, oneerrors.New(oneerrors.InternalError, "Could not reach the marketplace API", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotModified {
		return "", response.Header.Get("ETag"), true, nil
	}
	if response.StatusCode != http.StatusOK {
		// Status only, never body content: upstream error pages are response
		// content too and stay out of messages and logs.
		return "", "", false, oneerrors.New(
			oneerrors.InternalError,
			fmt.Sprintf("Marketplace API returned HTTP %d", response.StatusCode),
			oneerrors.WithStatus(502),
			oneerrors.WithRetryable(response.StatusCode >= http.StatusInternalServerError),
		)
	}
	// Read one byte past the cap so "exactly at the limit" and "over it" are
	// distinguishable.
	body, err := io.ReadAll(io.LimitReader(response.Body, marketplaceMaxBody+1))
	if err != nil {
		return "", "", false, oneerrors.New(oneerrors.InternalError, "Could not read the marketplace response", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if len(body) > marketplaceMaxBody {
		return "", "", false, oneerrors.New(oneerrors.InternalError, "Marketplace response exceeded the size limit", oneerrors.WithStatus(502))
	}
	return string(body), response.Header.Get("ETag"), false, nil
}

// Marketplace requests are built from fixed upstream hosts and validated
// directory paths, but redirects are still controlled by the remote server.
// Keep the default client from following a redirect into a private network or
// an unrelated host (an SSRF boundary); injected test clients intentionally
// remain unrestricted so their behavior can be asserted in isolation.
func marketplaceHTTPClient() *http.Client {
	return &http.Client{
		Timeout: marketplaceTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !marketplaceAllowedHTTPURL(request.URL) {
				return fmt.Errorf("marketplace redirect blocked")
			}
			return nil
		},
	}
}

func marketplaceAllowedHTTPURL(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.User != nil {
		return false
	}
	switch strings.ToLower(value.Hostname()) {
	case "api.skillhub.cn", "skillhub-1388575217.cos.accelerate.myqcloud.com", "mcpservers.org":
		return true
	default:
		return false
	}
}
