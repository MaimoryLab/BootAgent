package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/platform"
)

type marketplaceDoer func(*http.Request) (*http.Response, error)

func (d marketplaceDoer) Do(request *http.Request) (*http.Response, error) { return d(request) }

func marketplaceResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func marketplaceCore(t *testing.T, doer marketplaceDoer) *UseCases {
	t.Helper()
	core := NewUseCases(StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	core.SetRuntimeDownloader(doer)
	return core
}

// The slug is the only renderer-supplied value that reaches the proxied URL,
// so everything that is not a plain skillhub slug must be rejected before a
// request is built -- and no request may go out for a rejected slug.
func TestFetchMarketplaceSkillDetailValidatesSlug(t *testing.T) {
	tests := []struct {
		name string
		slug string
		ok   bool
	}{
		{"plain slug", "self-improving-agent", true},
		{"single char", "a", true},
		{"dots underscores dashes", "a.b_c-d0", true},
		{"max length", "a" + strings.Repeat("b", 127), true},
		{"empty", "", false},
		{"path traversal", "../../etc/passwd", false},
		{"slash", "a/b", false},
		{"encoded slash", "a%2fb", false},
		{"uppercase", "Upper", false},
		{"leading dash", "-lead", false},
		{"space", "a b", false},
		{"overlong", "a" + strings.Repeat("b", 128), false},
		{"non-ascii", "技能", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requested := ""
			core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
				requested = request.URL.String()
				return marketplaceResponse(http.StatusOK, `{"skill":{}}`), nil
			})
			body, err := core.FetchMarketplaceSkillDetail(context.Background(), test.slug)
			if test.ok {
				if err != nil || body != `{"skill":{}}` {
					t.Fatalf("valid slug %q: body=%q err=%v", test.slug, body, err)
				}
				want := "https://api.skillhub.cn/api/v1/skills/" + test.slug
				if requested != want {
					t.Fatalf("requested URL = %q, want %q", requested, want)
				}
				return
			}
			if err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
				t.Fatalf("invalid slug %q was not rejected as InvalidRequest: %v", test.slug, err)
			}
			if requested != "" {
				t.Fatalf("invalid slug %q still produced a request to %q", test.slug, requested)
			}
		})
	}
}

func TestFetchMarketplaceShowcaseReturnsUpstreamBody(t *testing.T) {
	var requested *http.Request
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requested = request
		return marketplaceResponse(http.StatusOK, `{"skills":[{"slug":"a"}]}`), nil
	})
	body, err := core.FetchMarketplaceShowcase(context.Background())
	if err != nil || body != `{"skills":[{"slug":"a"}]}` {
		t.Fatalf("showcase body=%q err=%v", body, err)
	}
	if requested == nil || requested.Method != http.MethodGet || requested.URL.String() != "https://api.skillhub.cn/api/v1/showcase/hot" {
		t.Fatalf("unexpected upstream request: %#v", requested)
	}
}

func TestFetchMarketplaceSkillFileReturnsLatestSkillMarkdown(t *testing.T) {
	var requested *http.Request
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requested = request
		return marketplaceResponse(http.StatusOK, "# Skill README"), nil
	})
	body, err := core.FetchMarketplaceSkillFile(context.Background(), "self-improving-agent")
	if err != nil || body != "# Skill README" {
		t.Fatalf("skill file body=%q err=%v", body, err)
	}
	if requested == nil || requested.URL.String() != "https://api.skillhub.cn/api/v1/skills/self-improving-agent/file?path=SKILL.md" {
		t.Fatalf("unexpected upstream request: %#v", requested)
	}
	if accept := requested.Header.Get("Accept"); accept != "text/markdown, text/plain;q=0.9" {
		t.Fatalf("Accept = %q", accept)
	}
}

func TestFetchMarketplaceMCPServerDetailUsesPublicSlugEndpoint(t *testing.T) {
	var requested *http.Request
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requested = request
		return marketplaceResponse(http.StatusOK, `{"slug":"demo-mcp","name":"Demo MCP"}`), nil
	})
	item, err := core.FetchMarketplaceMCPServerDetail(context.Background(), "demo-mcp")
	if err != nil || item.ID != "mcp-demo-mcp" || item.Name != "Demo MCP" {
		t.Fatalf("MCP detail item=%+v err=%v", item, err)
	}
	if requested == nil || requested.URL.String() != "https://api.skillhub.cn/api/v1/mcp/servers/demo-mcp" {
		t.Fatalf("unexpected detail request: %#v", requested)
	}
}

func TestFetchMarketplaceMCPServerReadmeUsesReadmeEndpoint(t *testing.T) {
	var requested *http.Request
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requested = request
		return marketplaceResponse(http.StatusOK, "# Demo MCP README"), nil
	})
	body, err := core.FetchMarketplaceMCPServerReadme(context.Background(), "demo-mcp")
	if err != nil || body != "# Demo MCP README" {
		t.Fatalf("MCP README body=%q err=%v", body, err)
	}
	if requested == nil || requested.URL.String() != "https://api.skillhub.cn/api/v1/mcp/servers/demo-mcp/readme" || requested.Header.Get("Accept") != "text/markdown, text/plain;q=0.9" {
		t.Fatalf("unexpected README request: %#v", requested)
	}
}

func TestFetchMarketplaceMCPServerDetailRejectsUnsafeSlug(t *testing.T) {
	requested := false
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requested = true
		return marketplaceResponse(http.StatusOK, "{}"), nil
	})
	for _, slug := range []string{"../escape", "MCP-Server", "demo/mcp", ""} {
		if _, err := core.FetchMarketplaceMCPServerDetail(context.Background(), slug); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
			t.Errorf("unsafe MCP slug %q error = %v", slug, err)
		}
	}
	if requested {
		t.Fatal("unsafe MCP slug produced an upstream request")
	}
}

// A non-200 answer must fail the call rather than hand an upstream error page
// to the frontend as if it were catalog JSON.
func TestFetchMarketplaceShowcaseRejectsNon200(t *testing.T) {
	core := marketplaceCore(t, func(*http.Request) (*http.Response, error) {
		return marketplaceResponse(http.StatusBadGateway, "upstream error page"), nil
	})
	if _, err := core.FetchMarketplaceShowcase(context.Background()); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("non-200 was not surfaced: %v", err)
	}
}

// The cap distinguishes "exactly at the limit" (passes) from one byte over
// (fails), so the boundary cannot silently truncate a payload.
func TestFetchMarketplaceShowcaseCapsResponseSize(t *testing.T) {
	buildCore := func(size int) *UseCases {
		return marketplaceCore(t, func(*http.Request) (*http.Response, error) {
			return marketplaceResponse(http.StatusOK, strings.Repeat("a", size)), nil
		})
	}
	if body, err := buildCore(marketplaceMaxBody).FetchMarketplaceShowcase(context.Background()); err != nil || len(body) != marketplaceMaxBody {
		t.Fatalf("payload at the cap failed: len=%d err=%v", len(body), err)
	}
	if _, err := buildCore(marketplaceMaxBody + 1).FetchMarketplaceShowcase(context.Background()); err == nil {
		t.Fatal("payload over the cap was accepted")
	}
}

func TestParseSkillHubItemsValidatesAndNormalizes(t *testing.T) {
	items, err := parseSkillHubItems([]byte(`{"skills":[{"slug":"good-skill","name":"Good","description":"desc","description_zh":"描述","iconUrl":"https://example.com/i.png"},{"slug":"../bad","name":"Bad"},{"slug":"","name":"Missing"}]}`))
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	if items[0].ID != "skillhub-good-skill" || items[0].Description != "描述" || items[0].Source != "skillhub" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestParseSkillHubPageParsesPublicPaginatedResponse(t *testing.T) {
	items, total, err := parseSkillHubPage([]byte(`{"code":0,"data":{"total":133272,"skills":[{"slug":"demo-skill","name":"Demo","description":"English","description_zh":"中文","category":"office-efficiency","iconUrl":"https://cdn.example/icon.png","downloads":12,"stars":3,"score":92991.71534618577}]}}`))
	if err != nil || total != 133272 || len(items) != 1 {
		t.Fatalf("items=%d total=%d err=%v", len(items), total, err)
	}
	if items[0].ID != "skillhub-demo-skill" || items[0].Category != "skill" || len(items[0].Tags) != 1 || items[0].Tags[0] != "office-efficiency" || items[0].Description != "中文" || items[0].Downloads != 12 || items[0].Score != 92991 {
		t.Fatalf("unexpected normalized SkillHub item: %+v", items[0])
	}
}

func TestParseMCPServerPageParsesMetadata(t *testing.T) {
	items, total, err := parseMCPServerPage([]byte(`{"page":1,"pageSize":100,"total":1,"items":[{"slug":"demo-mcp","name":"Demo MCP","summary":"summary","iconUrl":"https://skillhub-1388575217.cos.accelerate.myqcloud.com/mcp/demo.png","repoUrl":"https://github.com/example/demo-mcp","sourceUrl":"https://example.com/demo-mcp","stats":{"downloads":42},"tags":["search"]}]}`))
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("items=%d total=%d err=%v", len(items), total, err)
	}
	if items[0].ID != "mcp-demo-mcp" || items[0].RepositoryURL != "https://github.com/example/demo-mcp" || items[0].IconURL == "" || items[0].Downloads != 42 || !containsString(items[0].Tags, "search") || !containsString(items[0].Tags, "MCP") {
		t.Fatalf("unexpected normalized MCP item: %+v", items[0])
	}
}

func TestParseMCPServerDetailParsesInstallAndDocumentationMetadata(t *testing.T) {
	item, err := parseMCPServerDetail([]byte(`{"slug":"demo-mcp","name":"Demo MCP","nameEn":"demo-mcp","homepage":"https://www.npmjs.com/package/demo-mcp","sourceUrl":"https://example.com/demo","publisher":"Demo Team","publisherType":"tencent","summary":"summary","stats":{"downloads":42},"updatedAt":1778851419243}`))
	if err != nil {
		t.Fatalf("parse MCP detail: %v", err)
	}
	if item.ID != "mcp-demo-mcp" || item.ExternalURL != "https://www.npmjs.com/package/demo-mcp" || item.SourceURL != "https://example.com/demo" || item.Downloads != 42 {
		t.Fatalf("unexpected detail item: %+v", item)
	}
	if item.InstallPrompt == "" || !strings.Contains(item.InstallPrompt, "demo-mcp") || strings.Contains(item.InstallPrompt, `\\n`) {
		t.Fatalf("detail install prompt is missing package name: %q", item.InstallPrompt)
	}
}

func TestFetchSkillHubMCPPagePassesKeywordToPublicAPI(t *testing.T) {
	var requested *http.Request
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requested = request
		return marketplaceResponse(http.StatusOK, `{"total":1,"items":[{"slug":"demo-mcp","name":"Demo MCP","summary":"target"}]}`), nil
	})
	items, _, _, _, _, err := core.fetchSkillHubMCPPage(context.Background(), MarketplaceDiscoverOptions{Query: "target", Limit: 50}, "")
	if err != nil || len(items) != 1 || items[0].ID != "mcp-demo-mcp" {
		t.Fatalf("MCP keyword result=%+v err=%v", items, err)
	}
	if requested == nil {
		t.Fatal("MCP keyword request was not made")
	}
	query := requested.URL.Query()
	if query.Get("keyword") != "target" || query.Get("sortBy") != "updated_at" || query.Get("order") != "desc" || query.Get("page") != "1" || query.Get("pageSize") != fmt.Sprint(marketplacePageSize) {
		t.Fatalf("unexpected MCP keyword request: %s", requested.URL.String())
	}
}

func TestNormalizeMCPServerDoesNotInventNPMInstallCommand(t *testing.T) {
	item, err := parseMCPServerDetail([]byte(`{"slug":"hosted-mcp","name":"Hosted MCP","summary":"hosted"}`))
	if err != nil {
		t.Fatalf("parse hosted MCP: %v", err)
	}
	if strings.Contains(item.InstallPrompt, "npx -y") || !strings.Contains(item.InstallPrompt, "/readme") {
		t.Fatalf("hosted MCP prompt contains an unsupported command: %q", item.InstallPrompt)
	}
}

func TestFetchMarketplaceMCPServerReadmeRejectsUnsafeSlug(t *testing.T) {
	requested := false
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requested = true
		return marketplaceResponse(http.StatusOK, "# unexpected"), nil
	})
	if _, err := core.FetchMarketplaceMCPServerReadme(context.Background(), "../escape"); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("unsafe MCP README slug error = %v", err)
	}
	if requested {
		t.Fatal("unsafe MCP README slug produced an upstream request")
	}
}

func TestMCPSitemapItemRejectsUntrustedURL(t *testing.T) {
	if _, ok := mcpSitemapItem("http://mcpservers.org/servers/test"); ok {
		t.Fatal("non-HTTPS URL accepted")
	}
	if _, ok := mcpSitemapItem("https://evil.example/servers/test"); ok {
		t.Fatal("untrusted host accepted")
	}
	item, ok := mcpSitemapItem("https://mcpservers.org/servers/example-mcp-server")
	if !ok || item.ID != "mcp-example-mcp-server" || item.Category != "mcp-server" {
		t.Fatalf("unexpected result: %+v %v", item, ok)
	}
}

func TestNormalizeMarketplaceOptionsBoundsAndRejectsInvalidValues(t *testing.T) {
	options, err := normalizeMarketplaceOptions(MarketplaceDiscoverOptions{Limit: 999, Offset: -4, Query: "  search  ", QueryID: " q-1 "})
	if err != nil {
		t.Fatalf("normalize valid options: %v", err)
	}
	if options.Limit != marketplacePageSize || options.Offset != 0 || options.Query != "search" || options.QueryID != "q-1" {
		t.Fatalf("unexpected normalized options: %+v", options)
	}
	if _, err := normalizeMarketplaceOptions(MarketplaceDiscoverOptions{Source: "unknown"}); err == nil {
		t.Fatal("unknown source was accepted")
	}
	if _, err := normalizeMarketplaceOptions(MarketplaceDiscoverOptions{Query: strings.Repeat("x", 201)}); err == nil {
		t.Fatal("overlong query was accepted")
	}
}

func TestDiscoverMarketplaceSourcesUsesSourceCursorAndEchoesQueryID(t *testing.T) {
	var requests []*http.Request
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request)
		page := request.URL.Query().Get("page")
		body := fmt.Sprintf(`{"code":0,"data":{"total":4,"skills":[{"slug":"skill-%s","name":"Skill %s","description":"description"}]}}`, page, page)
		return marketplaceResponse(http.StatusOK, body), nil
	})
	result, err := core.DiscoverMarketplaceSources(context.Background(), MarketplaceDiscoverOptions{Source: "skillhub", Limit: 2, Offset: 2, Query: "needle", QueryID: "q-1"})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if result.QueryID != "q-1" || len(result.Items) != 1 || result.Items[0].ID != "skillhub-skill-2" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(requests) != 1 {
		t.Fatalf("request count = %d", len(requests))
	}
	query := requests[0].URL.Query()
	if query.Get("page") != "2" || query.Get("pageSize") != "2" || query.Get("keyword") != "needle" {
		t.Fatalf("unexpected source request: %s", requests[0].URL.String())
	}
	if result.Sources[0].Total != 4 || result.Sources[0].NextOffset != 4 || result.Sources[0].HasMore {
		t.Fatalf("unexpected source cursor: %+v", result.Sources[0])
	}
}

func TestDiscoverMarketplaceSources304UsesCachedPage(t *testing.T) {
	requests := 0
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Header.Get("If-None-Match") == "v1" {
			response := marketplaceResponse(http.StatusNotModified, "")
			response.Header.Set("ETag", "v1")
			return response, nil
		}
		response := marketplaceResponse(http.StatusOK, `{"code":0,"data":{"total":1,"skills":[{"slug":"cached","name":"Cached skill","description":"description"}]}}`)
		response.Header.Set("ETag", "v1")
		return response, nil
	})
	first, err := core.DiscoverMarketplaceSources(context.Background(), MarketplaceDiscoverOptions{Source: "skillhub", Limit: 50})
	if err != nil || len(first.Items) != 1 {
		t.Fatalf("initial discover: items=%d err=%v", len(first.Items), err)
	}
	second, err := core.DiscoverMarketplaceSources(context.Background(), MarketplaceDiscoverOptions{Source: "skillhub", Limit: 50})
	if err != nil || len(second.Items) != 1 || second.Sources[0].State != "live" {
		t.Fatalf("304 discover: result=%+v err=%v", second, err)
	}
	if requests != 2 {
		t.Fatalf("request count = %d, want 2", requests)
	}
}

func TestDiscoverMarketplaceSourcesFallsBackToCacheOnNetworkError(t *testing.T) {
	first := true
	core := marketplaceCore(t, func(*http.Request) (*http.Response, error) {
		if first {
			first = false
			return marketplaceResponse(http.StatusOK, `{"code":0,"data":{"total":1,"skills":[{"slug":"offline","name":"Offline skill","description":"description"}]}}`), nil
		}
		return nil, fmt.Errorf("network unavailable")
	})
	if _, err := core.DiscoverMarketplaceSources(context.Background(), MarketplaceDiscoverOptions{Source: "skillhub"}); err != nil {
		t.Fatalf("initial discover: %v", err)
	}
	result, err := core.DiscoverMarketplaceSources(context.Background(), MarketplaceDiscoverOptions{Source: "skillhub"})
	if err != nil || len(result.Items) != 1 || result.Sources[0].State != "cached" || !result.Stale {
		t.Fatalf("cache fallback: result=%+v err=%v", result, err)
	}
}

func TestFetchSkillHubMCPPageSearchesBeyondFirstPage(t *testing.T) {
	var requestedPages []string
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		page := request.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		if page == "1" {
			items := make([]string, 0, marketplacePageSize)
			for index := 0; index < marketplacePageSize; index++ {
				items = append(items, fmt.Sprintf(`{"slug":"server-%d","name":"Server %d","summary":"other"}`, index, index))
			}
			return marketplaceResponse(http.StatusOK, fmt.Sprintf(`{"total":101,"items":[%s]}`, strings.Join(items, ","))), nil
		}
		return marketplaceResponse(http.StatusOK, `{"total":101,"items":[{"slug":"target-server","name":"Target server","summary":"match"}]}`), nil
	})
	items, _, _, _, _, err := core.fetchSkillHubMCPPage(context.Background(), MarketplaceDiscoverOptions{Query: "target", Limit: marketplaceDefaultLimit}, "")
	if err != nil || len(items) != 1 || items[0].ID != "mcp-target-server" {
		t.Fatalf("MCP search result=%+v err=%v", items, err)
	}
	if strings.Join(requestedPages, ",") != "1,2" {
		t.Fatalf("requested pages = %v", requestedPages)
	}
}

func TestFetchSkillHubMCPPageSearchesAfterInvalidRecords(t *testing.T) {
	var requestedPages []string
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		page := request.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		if page == "1" {
			items := make([]string, 0, marketplacePageSize)
			items = append(items, `{"slug":"../invalid","name":"Invalid"}`)
			for index := 0; index < marketplacePageSize-1; index++ {
				items = append(items, fmt.Sprintf(`{"slug":"server-%d","name":"Server %d","summary":"other"}`, index, index))
			}
			return marketplaceResponse(http.StatusOK, fmt.Sprintf(`{"total":101,"items":[%s]}`, strings.Join(items, ","))), nil
		}
		return marketplaceResponse(http.StatusOK, `{"total":101,"items":[{"slug":"target-after-invalid","name":"Target server","summary":"match"},{"slug":"server-final","name":"Server final","summary":"other"}]}`), nil
	})
	items, _, _, _, _, err := core.fetchSkillHubMCPPage(context.Background(), MarketplaceDiscoverOptions{Query: "target", Limit: marketplaceDefaultLimit}, "")
	if err != nil || len(items) != 1 || items[0].ID != "mcp-target-after-invalid" {
		t.Fatalf("MCP search after invalid record=%+v err=%v", items, err)
	}
	if strings.Join(requestedPages, ",") != "1,2" {
		t.Fatalf("requested pages = %v", requestedPages)
	}
}

func TestDiscoverMarketplaceSourcesAcceptsEmptySkillHubPageAndAdvancesCursor(t *testing.T) {
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("page") != "2" {
			t.Fatalf("unexpected page request: %s", request.URL.String())
		}
		return marketplaceResponse(http.StatusOK, `{"code":0,"data":{"total":100,"skills":[]}}`), nil
	})
	result, err := core.DiscoverMarketplaceSources(context.Background(), MarketplaceDiscoverOptions{Source: "skillhub", Limit: 50, Offset: 50})
	if err != nil || len(result.Items) != 0 {
		t.Fatalf("empty SkillHub page result=%+v err=%v", result, err)
	}
	if len(result.Sources) != 1 || result.Sources[0].State != "live" || result.Sources[0].NextOffset != 100 || result.Sources[0].HasMore {
		t.Fatalf("unexpected empty-page cursor: %+v", result.Sources)
	}
}

func TestFetchSkillHubMCPPageSearchesWithoutTotal(t *testing.T) {
	var requestedPages []string
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		page := request.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		if page == "1" {
			items := make([]string, 0, marketplacePageSize)
			for index := 0; index < marketplacePageSize; index++ {
				items = append(items, fmt.Sprintf(`{"slug":"server-%d","name":"Server %d","summary":"other"}`, index, index))
			}
			return marketplaceResponse(http.StatusOK, fmt.Sprintf(`{"items":[%s]}`, strings.Join(items, ","))), nil
		}
		return marketplaceResponse(http.StatusOK, `{"items":[{"slug":"target-without-total","name":"Target server","summary":"match"}]}`), nil
	})
	items, _, _, _, _, err := core.fetchSkillHubMCPPage(context.Background(), MarketplaceDiscoverOptions{Query: "target", Limit: marketplaceDefaultLimit}, "")
	if err != nil || len(items) != 1 || items[0].ID != "mcp-target-without-total" {
		t.Fatalf("MCP search without total=%+v err=%v", items, err)
	}
	if strings.Join(requestedPages, ",") != "1,2" {
		t.Fatalf("requested pages = %v", requestedPages)
	}
}

func TestMarketplaceCachePageDoesNotAliasStoredItems(t *testing.T) {
	cache := marketplaceCache{Items: []catalog.MarketplaceItem{{ID: "one"}}, Total: 2}
	items, total := cachedPage(cache, 0, 1)
	items[0].ID = "changed"
	if cache.Items[0].ID != "one" || total != 2 {
		t.Fatalf("cached page aliases storage: cache=%+v total=%d", cache, total)
	}
}

func TestUpdateMarketplaceCacheReplacesStaleFirstPage(t *testing.T) {
	existing := marketplaceCache{FetchedAt: time.Now().UTC(), Items: []catalog.MarketplaceItem{{ID: "old-first"}, {ID: "old-second"}}, Total: 4}
	updated := updateMarketplaceCache(existing, true, []catalog.MarketplaceItem{{ID: "new-first"}}, 4, "etag-2", 0)
	if len(updated.Items) != 1 || updated.Items[0].ID != "new-first" {
		t.Fatalf("stale later page was retained: %+v", updated.Items)
	}
}

func TestMarketplaceErrorStringDoesNotExposeLocalDetails(t *testing.T) {
	message := errorString(fmt.Errorf("open /Users/private/.bootagent/cache/marketplace.json: permission denied"))
	if message == "" || strings.Contains(message, "/Users/private") || strings.Contains(message, "permission denied") {
		t.Fatalf("unsafe marketplace error: %q", message)
	}
}

func TestMarketplaceRequestURLRemainsPublic(t *testing.T) {
	parsed, err := url.Parse(skillHubSkillsURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "api.skillhub.cn" {
		t.Fatalf("unexpected SkillHub endpoint: %q", skillHubSkillsURL)
	}
}
