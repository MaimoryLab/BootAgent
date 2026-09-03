package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const (
	skillHubSkillsURL         = "https://api.skillhub.cn/api/skills"
	mcpServersAPIURL          = "https://api.skillhub.cn/api/v1/mcp/servers"
	marketplacePageSize       = 100
	marketplaceDefaultLimit   = 50
	marketplaceMaxSearchPages = 100
	// MCP search has no keyword endpoint, so a query may require walking the
	// public catalog. Bound the aggregate response size as well as page count;
	// otherwise a future upstream expansion could turn one keystroke into an
	// unbounded memory allocation.
	marketplaceMaxSearchBytes = 32 << 20
)

type MarketplaceSourceStatus struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	ItemCount  int    `json:"item_count"`
	Total      int    `json:"total"`
	HasMore    bool   `json:"has_more"`
	NextOffset int    `json:"next_offset"`
	FetchedAt  string `json:"fetched_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

type MarketplaceDynamicResult struct {
	Items      []catalog.MarketplaceItem `json:"items"`
	Sources    []MarketplaceSourceStatus `json:"sources"`
	Stale      bool                      `json:"stale"`
	FetchedAt  string                    `json:"fetched_at,omitempty"`
	Total      int                       `json:"total"`
	HasMore    bool                      `json:"has_more"`
	NextOffset int                       `json:"next_offset"`
	QueryID    string                    `json:"query_id,omitempty"`
}

type MarketplaceDiscoverOptions struct {
	Source       string `json:"source,omitempty"`
	Category     string `json:"category,omitempty"`
	Query        string `json:"query,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	Offset       int    `json:"offset,omitempty"`
	ForceRefresh bool   `json:"force_refresh,omitempty"`
	QueryID      string `json:"query_id,omitempty"`
}

type marketplaceCache struct {
	FetchedAt time.Time                 `json:"fetched_at"`
	ETag      string                    `json:"etag,omitempty"`
	Items     []catalog.MarketplaceItem `json:"items"`
	Total     int                       `json:"total,omitempty"`
}

type marketplacePage struct {
	Items       []catalog.MarketplaceItem
	Total       int
	HasMore     bool
	NextOffset  int
	FetchedAt   time.Time
	Fresh       bool
	NotModified bool
	Err         error
}

var dynamicMarketplaceSources = []string{"skillhub", "mcpservers"}

// Requests for the two sources run concurrently in the renderer. Keep the
// network outside this lock, but serialize cache read/merge/write so a slow
// second page cannot overwrite a page that completed just before it.
var marketplaceCacheMu sync.Mutex

// DiscoverMarketplaceSources fetches exactly one page for one source. When no
// source is specified it keeps the legacy aggregate response for callers that
// still use it, but the UI uses source-specific calls so cursors never cross.
func (u *UseCases) DiscoverMarketplaceSources(ctx context.Context, options MarketplaceDiscoverOptions) (MarketplaceDynamicResult, error) {
	if u == nil {
		return MarketplaceDynamicResult{}, oneerrors.New(oneerrors.InternalError, "Marketplace service is not configured")
	}
	options, err := normalizeMarketplaceOptions(options)
	if err != nil {
		return MarketplaceDynamicResult{}, err
	}
	result := MarketplaceDynamicResult{QueryID: options.QueryID}
	sources := dynamicMarketplaceSources
	if options.Source != "" {
		sources = []string{options.Source}
	}
	anyItems, anySuccess := false, false
	for _, source := range sources {
		page := u.discoverMarketplacePage(ctx, source, options)
		items := page.Items
		if options.Category != "" {
			items = filterMarketplaceCategory(items, options.Category)
		}
		status := MarketplaceSourceStatus{ID: source, ItemCount: len(items), Total: page.Total, HasMore: page.HasMore, NextOffset: page.NextOffset}
		if !page.FetchedAt.IsZero() {
			status.FetchedAt = page.FetchedAt.Format(time.RFC3339)
		}
		if page.Fresh {
			status.State = "live"
		} else if len(page.Items) > 0 {
			status.State = "cached"
			status.Error = errorString(page.Err)
			result.Stale = true
		} else {
			status.State = "unavailable"
			status.Error = errorString(page.Err)
		}
		result.Sources = append(result.Sources, status)
		result.Items = append(result.Items, items...)
		result.Total += page.Total
		result.HasMore = result.HasMore || page.HasMore
		if options.Source != "" {
			result.NextOffset = page.NextOffset
		}
		anyItems = anyItems || len(items) > 0
		anySuccess = anySuccess || page.Fresh
	}
	result.Items = dedupeMarketplaceItems(result.Items)
	if options.Source != "" && len(result.Sources) == 1 {
		result.Total = result.Sources[0].Total
		result.HasMore = result.Sources[0].HasMore
		result.NextOffset = result.Sources[0].NextOffset
	}
	if anyItems || anySuccess {
		result.FetchedAt = time.Now().UTC().Format(time.RFC3339)
		return result, nil
	}
	return result, oneerrors.New(oneerrors.InternalError, "Marketplace dynamic sources are unavailable", oneerrors.WithRetryable(true))
}

func normalizeMarketplaceOptions(options MarketplaceDiscoverOptions) (MarketplaceDiscoverOptions, error) {
	options.Source = strings.TrimSpace(options.Source)
	if options.Source != "" && !containsString(dynamicMarketplaceSources, options.Source) {
		return MarketplaceDiscoverOptions{}, oneerrors.New(oneerrors.InvalidRequest, "Unsupported marketplace source")
	}
	options.Query = strings.TrimSpace(options.Query)
	if len([]rune(options.Query)) > 200 {
		return MarketplaceDiscoverOptions{}, oneerrors.New(oneerrors.InvalidRequest, "Marketplace query is too long")
	}
	options.Category = strings.TrimSpace(options.Category)
	if options.Limit <= 0 {
		options.Limit = marketplaceDefaultLimit
	}
	if options.Limit > marketplacePageSize {
		options.Limit = marketplacePageSize
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	if options.Offset > 10_000_000 {
		return MarketplaceDiscoverOptions{}, oneerrors.New(oneerrors.InvalidRequest, "Marketplace offset is too large")
	}
	options.QueryID = strings.TrimSpace(options.QueryID)
	if len([]rune(options.QueryID)) > 128 {
		return MarketplaceDiscoverOptions{}, oneerrors.New(oneerrors.InvalidRequest, "Marketplace query id is too long")
	}
	return options, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func filterMarketplaceCategory(items []catalog.MarketplaceItem, category string) []catalog.MarketplaceItem {
	filtered := make([]catalog.MarketplaceItem, 0, len(items))
	for _, item := range items {
		if item.Category == category {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (u *UseCases) discoverMarketplacePage(ctx context.Context, source string, options MarketplaceDiscoverOptions) marketplacePage {
	cachePath := filepath.Join(u.status.Home, ".bootagent", "cache", "marketplace", source+".json")
	marketplaceCacheMu.Lock()
	cached, cacheErr := readMarketplaceCache(cachePath)
	marketplaceCacheMu.Unlock()
	var page marketplacePage
	var etag string
	if cacheErr == nil {
		etag = cached.ETag
	}
	var err error
	switch source {
	case "skillhub":
		page.Items, page.Total, etag, page.Fresh, page.NotModified, err = u.fetchSkillHubPage(ctx, options, etag)
	case "mcpservers":
		page.Items, page.Total, etag, page.Fresh, page.NotModified, err = u.fetchMCPPage(ctx, options, etag)
	default:
		err = fmt.Errorf("unknown marketplace source %q", source)
	}
	page.Err = err
	if page.Fresh {
		page.FetchedAt = time.Now().UTC()
	}
	if page.NotModified {
		if cacheErr != nil {
			page.Fresh = false
			page.Err = fmt.Errorf("marketplace returned not modified without a cache")
		} else {
			page.Items, page.Total = cachedPage(cached, options.Offset, options.Limit)
			page.FetchedAt = time.Now().UTC()
			page.Err = nil
			// A 304 is a successful refresh. Persist the new fetch time so a
			// subsequent offline fallback accurately reports when it was checked.
			marketplaceCacheMu.Lock()
			cached.FetchedAt = page.FetchedAt
			if etag != "" {
				cached.ETag = etag
			}
			_ = writeMarketplaceCache(cachePath, cached)
			marketplaceCacheMu.Unlock()
		}
	}
	setMarketplacePageCursor(&page, options)
	if page.Err == nil {
		if options.Query == "" {
			marketplaceCacheMu.Lock()
			latest, latestErr := readMarketplaceCache(cachePath)
			updated := updateMarketplaceCache(latest, latestErr == nil, page.Items, page.Total, etag, options.Offset)
			writeErr := writeMarketplaceCache(cachePath, updated)
			marketplaceCacheMu.Unlock()
			if writeErr != nil {
				page.Err = writeErr
			}
		}
		return page
	}
	// A query result must never fall back to an unrelated unfiltered cache.
	if options.Query == "" && cacheErr == nil {
		page.Items, page.Total = cachedPage(cached, options.Offset, options.Limit)
		page.FetchedAt = cached.FetchedAt
		page.Fresh = false
		setMarketplacePageCursor(&page, options)
	}
	return page
}

func setMarketplacePageCursor(page *marketplacePage, options MarketplaceDiscoverOptions) {
	if page.Total > 0 {
		// The remote APIs paginate raw records. Invalid/filtered records must not
		// compress the cursor and make the next request fetch the same page again.
		if options.Offset >= page.Total {
			page.NextOffset = options.Offset
			page.HasMore = false
			return
		}
		page.NextOffset = min(page.Total, options.Offset+options.Limit)
		page.HasMore = page.NextOffset < page.Total
		return
	}
	page.NextOffset = options.Offset + len(page.Items)
	page.HasMore = len(page.Items) >= options.Limit
}

func (u *UseCases) fetchSkillHubPage(ctx context.Context, options MarketplaceDiscoverOptions, etag string) ([]catalog.MarketplaceItem, int, string, bool, bool, error) {
	limit := options.Limit
	page := options.Offset/limit + 1
	query := url.Values{"page": {fmt.Sprint(page)}, "pageSize": {fmt.Sprint(limit)}, "sortBy": {"score"}, "order": {"desc"}}
	if strings.TrimSpace(options.Query) != "" {
		query.Set("keyword", strings.TrimSpace(options.Query))
	}
	body, responseETag, unchanged, err := u.fetchMarketplaceWithETag(ctx, skillHubSkillsURL+"?"+query.Encode(), "application/json", func() string {
		if options.Offset == 0 && options.Query == "" && !options.ForceRefresh {
			return etag
		}
		return ""
	}())
	if err != nil {
		return nil, 0, "", false, false, err
	}
	if unchanged {
		return nil, 0, responseETag, true, true, nil
	}
	items, total, err := parseSkillHubPage([]byte(body))
	return items, total, responseETag, true, false, err
}

// fetchSkillHubMCPPage keeps the SkillHub API adapter available as a
// supplemental source and as a fallback when the MCP Servers directory is
// unavailable. The public MCP Servers directory is composed in
// fetchMCPPage (marketplace_mcpservers.go).
func (u *UseCases) fetchSkillHubMCPPage(ctx context.Context, options MarketplaceDiscoverOptions, etag string) ([]catalog.MarketplaceItem, int, string, bool, bool, error) {
	needle := strings.ToLower(strings.TrimSpace(options.Query))
	if needle == "" {
		page := options.Offset/options.Limit + 1
		target := mcpServersPageURL(page, options.Limit, "")
		body, responseETag, unchanged, err := u.fetchMarketplaceWithETag(ctx, target, "application/json", func() string {
			if options.Offset == 0 && !options.ForceRefresh {
				return etag
			}
			return ""
		}())
		if err != nil {
			return nil, 0, "", false, false, err
		}
		if unchanged {
			return nil, 0, responseETag, true, true, nil
		}
		items, total, err := parseMCPServerPage([]byte(body))
		if total == 0 {
			total = len(items)
		}
		return items, total, responseETag, true, false, err
	}

	// The public API supports keyword filtering. Keep walking a bounded number
	// of pages and apply a local match as well: older deployments have ignored
	// the keyword parameter, and a malformed response must not make a valid
	// later-page match disappear.
	all := make([]catalog.MarketplaceItem, 0)
	remoteTotal := 0
	lastPageSize := 0
	scannedBytes := 0
	for page := 1; page <= marketplaceMaxSearchPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, "", false, false, err
		}
		target := mcpServersPageURL(page, marketplacePageSize, strings.TrimSpace(options.Query))
		body, _, unchanged, err := u.fetchMarketplaceWithETag(ctx, target, "application/json", "")
		if err != nil {
			return nil, 0, "", false, false, err
		}
		if unchanged {
			return nil, 0, "", false, false, fmt.Errorf("unexpected not-modified response for MCP search")
		}
		scannedBytes += len(body)
		if scannedBytes > marketplaceMaxSearchBytes {
			return nil, 0, "", false, false, fmt.Errorf("MCP search catalog is too large")
		}
		items, total, err := parseMCPServerPage([]byte(body))
		if err != nil {
			return nil, 0, "", false, false, err
		}
		remoteTotal = total
		all = append(all, items...)
		lastPageSize = len(items)
		if len(items) == 0 || (remoteTotal > 0 && len(all) >= remoteTotal) || (remoteTotal == 0 && len(items) < marketplacePageSize) {
			break
		}
	}
	if len(all) >= marketplaceMaxSearchPages*marketplacePageSize && lastPageSize >= marketplacePageSize && (remoteTotal == 0 || remoteTotal > len(all)) {
		return nil, 0, "", false, false, fmt.Errorf("MCP search catalog is too large")
	}
	filtered := all[:0]
	for _, item := range all {
		searchText := strings.ToLower(item.ID + " " + item.Name + " " + item.Description + " " + strings.Join(item.Tags, " "))
		if strings.Contains(searchText, needle) {
			filtered = append(filtered, item)
		}
	}
	total := len(filtered)
	start := min(options.Offset, total)
	end := min(start+options.Limit, total)
	return filtered[start:end], total, "", true, false, nil
}

func mcpServersPageURL(page, pageSize int, keyword string) string {
	query := url.Values{
		"page":     {fmt.Sprint(page)},
		"pageSize": {fmt.Sprint(pageSize)},
		"sortBy":   {"updated_at"},
		"order":    {"desc"},
	}
	if strings.TrimSpace(keyword) != "" {
		query.Set("keyword", strings.TrimSpace(keyword))
	}
	return mcpServersAPIURL + "?" + query.Encode()
}

var mcpSlugPattern = regexp.MustCompile(`/servers/([^/?#]+)$`)
var npmPackagePattern = regexp.MustCompile(`^(?:@[A-Za-z0-9._~-]+/)?[A-Za-z0-9._~-]+$`)

func mcpSitemapItem(raw string) (catalog.MarketplaceItem, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "mcpservers.org" {
		return catalog.MarketplaceItem{}, false
	}
	match := mcpSlugPattern.FindStringSubmatch(parsed.Path)
	if len(match) != 2 {
		return catalog.MarketplaceItem{}, false
	}
	slug := match[1]
	name := strings.NewReplacer("-", " ", "_", " ").Replace(slug)
	if name == "" {
		return catalog.MarketplaceItem{}, false
	}
	name = titleMarketplaceWords(name)
	return catalog.MarketplaceItem{ID: "mcp-" + slug, Category: "mcp-server", Type: "installable", Name: name, Description: "MCP Server from MCP Servers", Icon: "Puzzle", IconColor: "oklch(55% 0.15 160)", Tags: []string{"MCP"}, Scene: "integration", Source: "mcpservers", SourceLabel: "MCP Servers", SourceURL: raw, DocumentationURL: raw, ExternalURL: raw, InstallableKind: "mcp"}, true
}

func parseSkillHubItems(data []byte) ([]catalog.MarketplaceItem, error) {
	items, _, err := parseSkillHubPageLegacy(data)
	return items, err
}

func parseSkillHubPage(data []byte) ([]catalog.MarketplaceItem, int, error) {
	var wrapper struct {
		Code int `json:"code"`
		Data struct {
			Skills []json.RawMessage `json:"skills"`
			Total  int               `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, 0, fmt.Errorf("invalid SkillHub payload: %w", err)
	}
	if wrapper.Code != 0 {
		return nil, 0, fmt.Errorf("SkillHub API returned code %d", wrapper.Code)
	}
	items := make([]catalog.MarketplaceItem, 0, len(wrapper.Data.Skills))
	for _, raw := range wrapper.Data.Skills {
		item, err := parseSkillHubRecord(raw)
		if err != nil {
			return nil, 0, err
		}
		if item.ID != "" {
			items = append(items, item)
		}
	}
	return items, wrapper.Data.Total, nil
}

func parseSkillHubPageLegacy(data []byte) ([]catalog.MarketplaceItem, int, error) {
	var payload struct {
		Skills []struct {
			Slug          string `json:"slug"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			DescriptionZH string `json:"description_zh"`
			IconURL       string `json:"iconUrl"`
			Category      string `json:"category"`
			Stars         int    `json:"stars"`
			Downloads     int    `json:"downloads"`
			Score         int    `json:"score"`
			Homepage      string `json:"homepage"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, 0, fmt.Errorf("invalid SkillHub payload: %w", err)
	}
	items := make([]catalog.MarketplaceItem, 0, len(payload.Skills))
	for _, skill := range payload.Skills {
		if !marketplaceSlugPattern.MatchString(skill.Slug) || strings.TrimSpace(skill.Name) == "" {
			continue
		}
		items = append(items, catalog.MarketplaceItem{ID: "skillhub-" + skill.Slug, Category: "skill", Type: "installable", Name: skill.Name, Description: firstNonEmpty(skill.DescriptionZH, skill.Description), DescriptionEn: skill.Description, Icon: "Puzzle", IconColor: "oklch(60% 0.16 75)", Scene: "productivity", Source: "skillhub", SourceLabel: "SkillHub", SourceURL: "https://skillhub.cloud.tencent.com/skills/" + url.PathEscape(skill.Slug), DocumentationURL: "https://skillhub.cloud.tencent.com/skills/" + url.PathEscape(skill.Slug), IconURL: skill.IconURL, Stars: skill.Stars, Downloads: skill.Downloads, Score: skill.Score, ExternalURL: skill.Homepage, InstallableKind: "skill"})
	}
	if len(items) == 0 {
		return nil, 0, fmt.Errorf("SkillHub payload contains no valid skills")
	}
	return items, len(items), nil
}

func parseSkillHubRecord(data []byte) (catalog.MarketplaceItem, error) {
	var record struct {
		Slug          string         `json:"slug"`
		Name          string         `json:"name"`
		Description   string         `json:"description"`
		DescriptionZH string         `json:"description_zh"`
		IconURL       string         `json:"iconUrl"`
		Category      string         `json:"category"`
		Homepage      string         `json:"homepage"`
		UpstreamURL   string         `json:"upstream_url"`
		Stars         int            `json:"stars"`
		Downloads     int            `json:"downloads"`
		Score         float64        `json:"score"`
		UpdatedAt     int64          `json:"updated_at"`
		Verified      bool           `json:"verified"`
		Labels        map[string]any `json:"labels"`
		Namespace     struct {
			CanonicalName string `json:"canonicalName"`
		} `json:"namespace"`
		Publisher struct {
			Verified bool `json:"verified"`
		} `json:"publisher"`
		SubCategories []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"subCategories"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return catalog.MarketplaceItem{}, err
	}
	if !marketplaceSlugPattern.MatchString(record.Slug) || strings.TrimSpace(record.Name) == "" {
		return catalog.MarketplaceItem{}, nil
	}
	requiresAPIKey := strings.EqualFold(fmt.Sprint(record.Labels["requires_api_key"]), "true")
	// SkillHub's category is a domain facet (for example
	// "office-efficiency"), while the marketplace's top-level category is the
	// tool type. Keep every remote record in the Skill tab and retain the
	// upstream facet as a searchable tag instead of creating an invisible tab.
	tagKeys := make([]string, 0, len(record.SubCategories))
	tags := make([]string, 0, len(record.SubCategories)+1)
	for _, sub := range record.SubCategories {
		key := strings.TrimSpace(sub.Key)
		if key == "" || containsString(tagKeys, key) {
			continue
		}
		tagKeys = append(tagKeys, key)
		if label := strings.TrimSpace(sub.Name); label != "" {
			tags = append(tags, label)
		}
	}
	if category := strings.TrimSpace(record.Category); category != "" && category != "skill" && !containsString(tags, category) {
		tags = append(tags, category)
	}
	scenes := skillHubScenes(tagKeys)
	scene := "productivity"
	if len(scenes) > 0 {
		scene = scenes[0]
	}
	name := compactMarketplaceText(record.Name, 200)
	description := compactMarketplaceText(firstNonEmpty(record.DescriptionZH, record.Description), 4000)
	namespace := strings.TrimSpace(record.Namespace.CanonicalName)
	if !skillHubNamespacePattern.MatchString(namespace) {
		namespace = record.Slug
	}
	detailURL := "https://skillhub.cloud.tencent.com/skills/" + url.PathEscape(record.Slug)
	upstreamURL := safeMarketplaceHTTPSURL(record.UpstreamURL)
	homepage := safeMarketplaceHTTPSURL(record.Homepage)
	if homepage == "" {
		homepage = upstreamURL
	}
	trust := "community"
	if record.Verified || record.Publisher.Verified {
		trust = "verified"
	}
	return catalog.MarketplaceItem{
		ID: "skillhub-" + record.Slug, Category: "skill", Type: "installable", InstallableKind: "skill",
		Name: name, Description: firstNonEmpty(description, "SkillHub skill"), DescriptionEn: compactMarketplaceText(record.Description, 4000),
		Icon: "Puzzle", IconColor: "oklch(60% 0.16 75)", Tags: tags, TagKeys: tagKeys, Scene: scene, Scenes: scenes,
		Source: "skillhub", RequiresAPIKey: requiresAPIKey, SourceLabel: "SkillHub", SourceURL: detailURL,
		RepositoryURL: upstreamURL, DocumentationURL: detailURL, IconURL: safeMarketplaceHTTPSURL(record.IconURL),
		Stars: record.Stars, Downloads: record.Downloads, Score: boundedScore(record.Score), ExternalURL: homepage,
		TrustLevel: trust, UpdatedAt: formatMarketplaceUnixMillis(record.UpdatedAt),
		InstallPrompt: skillHubInstallPrompt(name, namespace),
		TargetHint:    "粘贴到任意命令行 Agent 对话框执行，它会调用 skillhub CLI 完成安装",
	}, nil
}

var skillHubNamespacePattern = regexp.MustCompile(`^@[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// skillHubScenes mirrors the product taxonomy used by the static catalog. The
// remote API exposes sub-category keys, not the marketplace's scene enum.
func skillHubScenes(keys []string) []string {
	seen := map[string]bool{}
	for _, key := range keys {
		var scene string
		switch {
		case strings.HasPrefix(key, "agent-"), strings.HasPrefix(key, "dev-"):
			scene = "coding"
		case strings.HasPrefix(key, "knowledge-"):
			scene = "memory"
		case strings.HasPrefix(key, "office-"), strings.HasPrefix(key, "content-"), strings.HasPrefix(key, "biz-"), strings.HasPrefix(key, "life-"):
			scene = "productivity"
		case strings.HasPrefix(key, "data-"):
			scene = "reasoning"
		case strings.HasPrefix(key, "design-"):
			scene = "design"
		case strings.HasPrefix(key, "it-"), strings.HasPrefix(key, "itops-"):
			scene = "integration"
		}
		if scene != "" && !seen[scene] {
			seen[scene] = true
		}
	}
	ordered := []string{"coding", "design", "reasoning", "memory", "integration", "productivity", "learning"}
	result := make([]string, 0, len(seen))
	for _, scene := range ordered {
		if seen[scene] {
			result = append(result, scene)
		}
	}
	return result
}

func skillHubInstallPrompt(name, namespace string) string {
	return fmt.Sprintf("请帮我安装 SkillHub 上的 Skill「%s」（%s）。\n\n执行步骤：\n1. 检查本机是否已安装 skillhub CLI（运行 `skillhub -v`）。若未安装，先运行：\n   curl -fsSL https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/install/install.sh | bash\n2. 找到当前 Agent 的 skills 目录（例如 Claude Code 是 ~/.claude/skills，其他 Agent 请查阅其文档）\n3. 运行安装命令：\n   skillhub install %s --dir <skills 目录>\n4. 安装完成后列出该 Skill 的说明，确认它已可用", name, namespace, namespace)
}

func compactMarketplaceText(value string, max int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if max > 0 && len([]rune(value)) > max {
		value = string([]rune(value)[:max])
	}
	return value
}

func boundedScore(value float64) int {
	if value <= 0 {
		return 0
	}
	if value >= float64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(value)
}

func formatMarketplaceUnixMillis(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.UnixMilli(value).UTC().Format(time.RFC3339)
}

func safeMarketplaceHTTPSURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	return parsed.String()
}

type mcpServerRecord struct {
	Banned        bool     `json:"banned"`
	Status        string   `json:"status"`
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	NameEn        string   `json:"nameEn"`
	Category      string   `json:"category"`
	Summary       string   `json:"summary"`
	SummaryZh     string   `json:"summaryZh"`
	IconURL       string   `json:"iconUrl"`
	RepoURL       string   `json:"repoUrl"`
	SourceURL     string   `json:"sourceUrl"`
	Homepage      string   `json:"homepage"`
	Publisher     string   `json:"publisher"`
	PublisherType string   `json:"publisherType"`
	UpdatedAt     int64    `json:"updatedAt"`
	Tags          []string `json:"tags"`
	Stats         struct {
		Downloads int `json:"downloads"`
		Installs  int `json:"installs"`
	} `json:"stats"`
}

type mcpServerPagePayload struct {
	Items []mcpServerRecord `json:"items"`
	Total int               `json:"total"`
	Data  *struct {
		Items []mcpServerRecord `json:"items"`
		Total int               `json:"total"`
	} `json:"data"`
}

func parseMCPServerPage(data []byte) ([]catalog.MarketplaceItem, int, error) {
	var payload mcpServerPagePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, 0, fmt.Errorf("invalid MCP API payload: %w", err)
	}
	if len(payload.Items) == 0 && payload.Data != nil {
		payload.Items = payload.Data.Items
		if payload.Total == 0 {
			payload.Total = payload.Data.Total
		}
	}
	items := make([]catalog.MarketplaceItem, 0, len(payload.Items))
	for _, server := range payload.Items {
		item, ok := normalizeMCPServerRecord(server)
		if ok {
			items = append(items, item)
		}
	}
	return items, payload.Total, nil
}

// parseMCPServerDetail accepts both the current single-record response and the
// envelope used by older SkillHub deployments. Keeping this tolerant lets a
// client update independently from the public API without losing detail pages.
func parseMCPServerDetail(data []byte) (catalog.MarketplaceItem, error) {
	var record mcpServerRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return catalog.MarketplaceItem{}, fmt.Errorf("invalid MCP detail payload: %w", err)
	}
	if strings.TrimSpace(record.Slug) == "" {
		var envelope struct {
			Data mcpServerRecord `json:"data"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return catalog.MarketplaceItem{}, fmt.Errorf("invalid MCP detail payload: %w", err)
		}
		record = envelope.Data
	}
	item, ok := normalizeMCPServerRecord(record)
	if !ok {
		return catalog.MarketplaceItem{}, fmt.Errorf("MCP detail payload contains no valid server")
	}
	return item, nil
}

func normalizeMCPServerRecord(server mcpServerRecord) (catalog.MarketplaceItem, bool) {
	if server.Banned || (server.Status != "" && !strings.EqualFold(server.Status, "visible")) {
		return catalog.MarketplaceItem{}, false
	}
	if !marketplaceSlugPattern.MatchString(server.Slug) || strings.TrimSpace(server.Name) == "" {
		return catalog.MarketplaceItem{}, false
	}
	slug := server.Slug
	docs := "https://skillhub.cloud.tencent.com/mcp-servers/" + url.PathEscape(slug)
	repositoryURL := safeMarketplaceHTTPSURL(server.RepoURL)
	sourceURL := safeMarketplaceHTTPSURL(server.SourceURL)
	homepage := safeMarketplaceHTTPSURL(server.Homepage)
	trust := "community"
	if strings.EqualFold(server.PublisherType, "tencent") {
		trust = "official"
	}
	tags := make([]string, 0, len(server.Tags)+2)
	for _, tag := range server.Tags {
		tag = compactMarketplaceText(tag, 80)
		if tag != "" && !containsString(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if category := compactMarketplaceText(server.Category, 80); category != "" && !containsString(tags, category) {
		tags = append(tags, category)
	}
	if !containsString(tags, "MCP") {
		tags = append(tags, "MCP")
	}
	name := compactMarketplaceText(server.Name, 200)
	description := compactMarketplaceText(firstNonEmpty(server.SummaryZh, server.Summary), 4000)
	if description == "" {
		description = "MCP Server"
	}
	readmeURL := marketplaceMCPServerURLBase + url.PathEscape(slug) + "/readme"
	return catalog.MarketplaceItem{
		ID: "mcp-" + slug, Category: "mcp-server", Type: "installable", Name: name,
		Description: description, DescriptionEn: compactMarketplaceText(server.Summary, 4000),
		Icon: "Puzzle", IconColor: "oklch(55% 0.15 160)", Tags: tags, Scene: "integration",
		Source: "mcpservers", SourceLabel: "MCP Servers", SourceURL: firstNonEmpty(sourceURL, homepage, repositoryURL, docs),
		RepositoryURL: repositoryURL, DocumentationURL: docs, ReadmeURL: readmeURL,
		IconURL: safeMarketplaceHTTPSURL(server.IconURL), ExternalURL: firstNonEmpty(homepage, repositoryURL, sourceURL, docs),
		InstallableKind: "mcp", InstallPrompt: mcpInstallPrompt(name, npmPackageName(homepage), homepage, readmeURL),
		TargetHint: "复制提示词到 MCP 配置页或 Agent 对话框，具体参数以实时 README 为准",
		Downloads:  server.Stats.Downloads, TrustLevel: trust, UpdatedAt: formatMarketplaceUnixMillis(server.UpdatedAt),
	}, true
}

func mcpInstallPrompt(name, packageName, homepage, readmeURL string) string {
	packageName = strings.TrimSpace(packageName)
	if !npmPackagePattern.MatchString(packageName) {
		packageName = ""
	}
	if packageFromURL := npmPackageName(homepage); packageFromURL != "" {
		packageName = packageFromURL
	}
	if packageName != "" {
		return fmt.Sprintf("请帮我配置 MCP Server「%s」。\n\n推荐使用 npm 临时运行：\n1. 确认 Node.js 和 npx 可用。\n2. 在当前 Agent 的 MCP 配置中使用命令 `npx -y %s@latest`。\n3. 按官方 README 填写所需环境变量或密钥，密钥只写入本地安全配置，不要写入日志。\n\n官方 README：%s", name, packageName, readmeURL)
	}
	return fmt.Sprintf("请帮我配置 MCP Server「%s」。\n\n请先打开并阅读官方 README：%s\n根据当前 Agent 的 MCP 配置格式完成安装；如果需要密钥，使用占位符并提醒我在本地安全配置中填写，不要把密钥写入日志。", name, readmeURL)
}

// npmPackageName is intentionally conservative. The MCP detail endpoint's
// homepage is untrusted metadata; only a real npm package path is accepted as
// an executable package name in the generated prompt.
func npmPackageName(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "www.npmjs.com" && parsed.Hostname() != "npmjs.com" {
		return ""
	}
	const prefix = "/package/"
	if !strings.HasPrefix(parsed.Path, prefix) {
		return ""
	}
	name, err := url.PathUnescape(strings.TrimPrefix(parsed.Path, prefix))
	if err != nil || !npmPackagePattern.MatchString(name) {
		return ""
	}
	return name
}

func dedupeMarketplaceItems(items []catalog.MarketplaceItem) []catalog.MarketplaceItem {
	seen := map[string]bool{}
	out := make([]catalog.MarketplaceItem, 0, len(items))
	for _, item := range items {
		if item.ID == "" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
func errorString(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "Marketplace refresh failed"
	}
	// Parser and filesystem errors may wrap paths or response details. Only
	// expose short, deliberately stable diagnostics in the renderer status line.
	for _, prefix := range []string{
		"Marketplace API returned HTTP ",
		"SkillHub API returned code ",
		"SkillHub API contains no valid skills",
		"SkillHub payload contains no valid skills",
		"MCP search catalog is too large",
		"unexpected not-modified response for MCP search",
		"marketplace returned not modified without a cache",
	} {
		if strings.HasPrefix(message, prefix) {
			return compactMarketplaceText(message, 160)
		}
	}
	return "Marketplace refresh failed"
}
func titleMarketplaceWords(value string) string {
	words := strings.Fields(value)
	for index, word := range words {
		runes := []rune(word)
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
			words[index] = string(runes)
		}
	}
	return strings.Join(words, " ")
}

func readMarketplaceCache(path string) (marketplaceCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return marketplaceCache{}, err
	}
	var cache marketplaceCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.FetchedAt.IsZero() {
		return marketplaceCache{}, fmt.Errorf("invalid marketplace cache")
	}
	// Caches written before the total field was introduced remain valid. Their
	// first page length is the only trustworthy lower bound, so use it rather
	// than presenting a zero total to the renderer.
	if cache.Total < len(cache.Items) {
		cache.Total = len(cache.Items)
	}
	return cache, nil
}

// cachedPage returns a defensive copy of one contiguous cached page. The
// cache is populated in the same order as the renderer's source cursor, so a
// page beyond the stored prefix is correctly reported as empty.
func cachedPage(cache marketplaceCache, offset, limit int) ([]catalog.MarketplaceItem, int) {
	total := cache.Total
	if total < len(cache.Items) {
		total = len(cache.Items)
	}
	if offset < 0 || offset >= len(cache.Items) {
		return []catalog.MarketplaceItem{}, total
	}
	end := offset + limit
	if end > len(cache.Items) {
		end = len(cache.Items)
	}
	items := append([]catalog.MarketplaceItem(nil), cache.Items[offset:end]...)
	return items, total
}

// updateMarketplaceCache merges a successfully fetched page into the source
// prefix. A first-page refresh replaces the prefix (and drops later pages so
// changed upstream ordering cannot shift cached offsets); later pages replace
// their contiguous range or append at the end. IDs are deduplicated so retries
// cannot inflate the on-disk cache.
func updateMarketplaceCache(existing marketplaceCache, exists bool, page []catalog.MarketplaceItem, total int, etag string, offset int) marketplaceCache {
	if !exists {
		existing = marketplaceCache{}
	}
	current := append([]catalog.MarketplaceItem(nil), existing.Items...)
	fetched := append([]catalog.MarketplaceItem(nil), page...)
	if offset <= 0 {
		// Replacing the prefix is important when the upstream ranking changes:
		// prepending a refreshed page shifts every cached later page, so an
		// offline request for offset N would return the wrong records. Later
		// pages are deliberately discarded and will be refilled by the cursor.
		current = fetched
	} else if offset < len(current) {
		end := offset + len(fetched)
		if end > len(current) {
			end = len(current)
		}
		current = append(append(append([]catalog.MarketplaceItem(nil), current[:offset]...), fetched...), current[end:]...)
	} else if offset == len(current) {
		current = append(current, fetched...)
	}
	current = dedupeMarketplaceItems(current)
	if total <= 0 {
		total = existing.Total
	}
	if total < len(current) {
		total = len(current)
	}
	if etag == "" {
		etag = existing.ETag
	}
	return marketplaceCache{FetchedAt: time.Now().UTC(), ETag: etag, Items: current, Total: total}
}

func writeMarketplaceCache(path string, cache marketplaceCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".marketplace-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
