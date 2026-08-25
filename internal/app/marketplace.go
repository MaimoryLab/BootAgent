package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

// The marketplace pages read the public skillhub API, but the renderer cannot
// call it directly: api.skillhub.cn only echoes CORS headers for skillhub's
// own origins, so every browser-side fetch is blocked and the marketplace
// would be stuck on its bundled snapshot. These use cases proxy the two GET
// endpoints instead -- a server-to-server request has no CORS to satisfy.
//
// Responses pass through as raw JSON strings on purpose: the frontend already
// owns payload normalisation, and parsing here would create a second schema
// to keep in sync. Response content is never logged (metadata-only logging is
// a product commitment).
const (
	marketplaceShowcaseURL  = "https://api.skillhub.cn/api/v1/showcase/hot"
	marketplaceSkillURLBase = "https://api.skillhub.cn/api/v1/skills/"
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

func (u *UseCases) fetchMarketplace(ctx context.Context, target, accept string) (string, error) {
	if u == nil {
		return "", oneerrors.New(oneerrors.InternalError, "Marketplace proxy is not configured", oneerrors.WithStatus(501))
	}
	// httpDoer is only set when a caller injected one, which in production is
	// nobody: it exists so tests can answer without a network. The fallback
	// matches latestAgentVersions.
	client := u.httpDoer
	if client == nil {
		client = &http.Client{Timeout: marketplaceTimeout}
	}
	requestCtx, cancel := context.WithTimeout(ctx, marketplaceTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, target, nil)
	if err != nil {
		return "", oneerrors.New(oneerrors.InternalError, "Could not build the marketplace request", oneerrors.WithCause(err))
	}
	request.Header.Set("Accept", accept)
	response, err := client.Do(request)
	if err != nil {
		return "", oneerrors.New(oneerrors.InternalError, "Could not reach the marketplace API", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		// Status only, never body content: upstream error pages are response
		// content too and stay out of messages and logs.
		return "", oneerrors.New(
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
		return "", oneerrors.New(oneerrors.InternalError, "Could not read the marketplace response", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if len(body) > marketplaceMaxBody {
		return "", oneerrors.New(oneerrors.InternalError, "Marketplace response exceeded the size limit", oneerrors.WithStatus(502))
	}
	return string(body), nil
}
