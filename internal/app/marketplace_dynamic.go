package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const (
	skillHubSkillsURL   = "https://api.skillhub.cn/api/skills"
	mcpServersAPIURL    = "https://api.skillhub.cn/api/v1/mcp/servers"
	marketplaceCacheTTL = 6 * time.Hour
	marketplacePageSize = 100
)

type MarketplaceSourceStatus struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	ItemCount int    `json:"item_count"`
	FetchedAt string `json:"fetched_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

type MarketplaceDynamicResult struct {
	Items      []catalog.MarketplaceItem `json:"items"`
	Sources    []MarketplaceSourceStatus `json:"sources"`
	Stale      bool                      `json:"stale"`
	FetchedAt  string                    `json:"fetched_at,omitempty"`
	Total      int                       `json:"total"`
	HasMore    bool                      `json:"has_more"`
	NextOffset int                       `json:"next_offset"`
}

type MarketplaceDiscoverOptions struct {
	Source   string `json:"source,omitempty"`
	Category string `json:"category,omitempty"`
	Query    string `json:"query,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

type marketplaceCache struct {
	FetchedAt time.Time                 `json:"fetched_at"`
	ETag      string                    `json:"etag,omitempty"`
	Items     []catalog.MarketplaceItem `json:"items"`
}

var marketplaceDynamicMu sync.Mutex

// DiscoverMarketplaceSources refreshes dynamic sources and falls back to the
// last successful cache per source. The static manifest remains the offline
// baseline and is intentionally not rewritten by this operation.
func (u *UseCases) DiscoverMarketplaceSources(ctx context.Context, options MarketplaceDiscoverOptions) (MarketplaceDynamicResult, error) {
	if u == nil {
		return MarketplaceDynamicResult{}, oneerrors.New(oneerrors.InternalError, "Marketplace service is not configured")
	}
	marketplaceDynamicMu.Lock()
	defer marketplaceDynamicMu.Unlock()
	result := MarketplaceDynamicResult{}
	all := make([]catalog.MarketplaceItem, 0)
	remotePage := options.Offset > 0 || strings.TrimSpace(options.Query) != ""
	anyFresh := false
	for _, source := range []string{"skillhub", "mcpservers"} {
		if options.Source != "" && options.Source != source {
			continue
		}
		items, fetchedAt, fresh, err := u.loadDynamicSource(ctx, source, options)
		status := MarketplaceSourceStatus{ID: source, ItemCount: len(items)}
		if fresh {
			status.State = "live"
			status.FetchedAt = fetchedAt.Format(time.RFC3339)
			anyFresh = true
		} else if len(items) > 0 {
			status.State = "cached"
			status.FetchedAt = fetchedAt.Format(time.RFC3339)
			status.Error = errorString(err)
			result.Stale = true
		} else {
			status.State = "unavailable"
			status.Error = errorString(err)
		}
		result.Sources = append(result.Sources, status)
		all = append(all, items...)
	}
	if !anyFresh && len(all) == 0 {
		return result, oneerrors.New(oneerrors.InternalError, "Marketplace dynamic sources are unavailable", oneerrors.WithRetryable(true))
	}
	all = dedupeMarketplaceItems(all)
	filtered := all[:0]
	needle := strings.ToLower(strings.TrimSpace(options.Query))
	for _, item := range all {
		if options.Category != "" && item.Category != options.Category {
			continue
		}
		if !remotePage && needle != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.Description), needle) {
			continue
		}
		filtered = append(filtered, item)
	}
	result.Total = len(filtered)
	if remotePage {
		result.Items = filtered
		result.HasMore = len(filtered) >= max(1, options.Limit)
		pageSize := options.Limit
		if pageSize <= 0 {
			pageSize = 50
		}
		result.NextOffset = options.Offset + pageSize
		if len(result.Items) > 0 {
			result.FetchedAt = time.Now().UTC().Format(time.RFC3339)
		}
		return result, nil
	}
	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(filtered) {
		offset = len(filtered)
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	result.Items = filtered[offset:end]
	result.HasMore = end < len(filtered)
	result.NextOffset = end
	if len(result.Items) > 0 {
		result.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return result, nil
}

func (u *UseCases) loadDynamicSource(ctx context.Context, source string, options MarketplaceDiscoverOptions) ([]catalog.MarketplaceItem, time.Time, bool, error) {
	cachePath := filepath.Join(u.status.Home, ".bootagent", "cache", "marketplace", source+".json")
	cached, cacheErr := readMarketplaceCache(cachePath)
	// Category is a local facet. Only keyword search needs a remote query;
	// applying a category must never replace or expand the current catalog.
	useCache := strings.TrimSpace(options.Query) == "" && options.Offset == 0
	if useCache && cacheErr == nil && time.Since(cached.FetchedAt) < marketplaceCacheTTL {
		return cached.Items, cached.FetchedAt, true, nil
	}
	var items []catalog.MarketplaceItem
	var err error
	switch source {
	case "skillhub":
		if !useCache {
			items, err = u.fetchSkillHubQuery(ctx, options)
			break
		}
		// The first page is enough for the initial render. Subsequent pages are
		// requested with the same query as the user scrolls; never block the UI
		// while walking the entire (100k+) SkillHub catalog.
		items, err = u.fetchSkillHubQuery(ctx, options)
	case "mcpservers":
		if !useCache {
			items, err = u.fetchMCPQuery(ctx, options)
			break
		}
		var etag string
		var unchanged bool
		items, etag, unchanged, err = u.fetchMCPServerPages(ctx, cached.ETag)
		if unchanged && cacheErr == nil {
			cached.FetchedAt = time.Now().UTC()
			_ = writeMarketplaceCache(cachePath, cached)
			return cached.Items, cached.FetchedAt, true, nil
		}
		if err == nil {
			cached.ETag = etag
		}
	default:
		err = fmt.Errorf("unknown marketplace source %q", source)
	}
	if err == nil && len(items) > 0 {
		now := time.Now().UTC()
		if !useCache {
			return items, now, true, nil
		}
		if cacheErr := writeMarketplaceCache(cachePath, marketplaceCache{FetchedAt: now, ETag: cached.ETag, Items: items}); cacheErr != nil {
			return items, now, true, cacheErr
		}
		return items, now, true, nil
	}
	if cached, cacheErr := readMarketplaceCache(cachePath); cacheErr == nil {
		return cached.Items, cached.FetchedAt, false, err
	}
	return nil, time.Time{}, false, err
}

func (u *UseCases) fetchSkillHubQuery(ctx context.Context, options MarketplaceDiscoverOptions) ([]catalog.MarketplaceItem, error) {
	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > marketplacePageSize {
		limit = marketplacePageSize
	}
	page := options.Offset/limit + 1
	query := url.Values{"page": {fmt.Sprint(page)}, "pageSize": {fmt.Sprint(limit)}, "sortBy": {"score"}, "order": {"desc"}}
	if strings.TrimSpace(options.Query) != "" {
		query.Set("keyword", strings.TrimSpace(options.Query))
	}
	body, err := u.fetchMarketplace(ctx, skillHubSkillsURL+"?"+query.Encode(), "application/json")
	if err != nil {
		return nil, err
	}
	items, _, err := parseSkillHubPage([]byte(body))
	return items, err
}

func (u *UseCases) fetchMCPQuery(ctx context.Context, options MarketplaceDiscoverOptions) ([]catalog.MarketplaceItem, error) {
	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > marketplacePageSize {
		limit = marketplacePageSize
	}
	page := options.Offset/limit + 1
	target := fmt.Sprintf("%s?page=%d&pageSize=%d", mcpServersAPIURL, page, limit)
	body, err := u.fetchMarketplace(ctx, target, "application/json")
	if err != nil {
		return nil, err
	}
	items, _, err := parseMCPServerPage([]byte(body))
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(options.Query))
	if needle == "" {
		return items, nil
	}
	filtered := items[:0]
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Name+" "+item.Description+" "+strings.Join(item.Tags, " ")), needle) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (u *UseCases) fetchSkillHubPages(ctx context.Context, etag string) ([]catalog.MarketplaceItem, string, bool, error) {
	items := make([]catalog.MarketplaceItem, 0)
	var total int
	for page := 1; page <= 2000; page++ {
		target := fmt.Sprintf("%s?page=%d&pageSize=%d&sortBy=score&order=desc", skillHubSkillsURL, page, marketplacePageSize)
		body, pageETag, unchanged, err := u.fetchMarketplaceWithETag(ctx, target, "application/json", func() string {
			if page == 1 {
				return etag
			}
			return ""
		}())
		if err != nil {
			return nil, "", false, err
		}
		if page == 1 && unchanged {
			return nil, pageETag, true, nil
		}
		parsed, pageTotal, err := parseSkillHubPage([]byte(body))
		if err != nil {
			return nil, "", false, err
		}
		items = append(items, parsed...)
		if page == 1 {
			etag = pageETag
		}
		total = pageTotal
		if len(parsed) == 0 || len(items) >= total || len(parsed) < marketplacePageSize {
			break
		}
	}
	if len(items) == 0 {
		return nil, "", false, fmt.Errorf("SkillHub API contains no valid skills")
	}
	return items, etag, false, nil
}

func (u *UseCases) fetchMCPServerPages(ctx context.Context, etag string) ([]catalog.MarketplaceItem, string, bool, error) {
	items := make([]catalog.MarketplaceItem, 0)
	firstETag := ""
	for page := 1; page <= 100; page++ {
		target := fmt.Sprintf("%s?page=%d&pageSize=%d", mcpServersAPIURL, page, marketplacePageSize)
		body, pageETag, unchanged, err := u.fetchMarketplaceWithETag(ctx, target, "application/json", func() string {
			if page == 1 {
				return etag
			}
			return ""
		}())
		if err != nil {
			return nil, "", false, err
		}
		if page == 1 && unchanged {
			return nil, pageETag, true, nil
		}
		if page == 1 {
			firstETag = pageETag
		}
		parsed, total, err := parseMCPServerPage([]byte(body))
		if err != nil {
			return nil, "", false, err
		}
		items = append(items, parsed...)
		if len(parsed) == 0 || len(items) >= total || len(parsed) < marketplacePageSize {
			break
		}
	}
	if len(items) == 0 {
		return nil, "", false, fmt.Errorf("MCP API contains no visible servers")
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, firstETag, false, nil
}

var mcpSlugPattern = regexp.MustCompile(`/servers/([^/?#]+)$`)

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
	name = strings.Title(name)
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
		Slug          string            `json:"slug"`
		Name          string            `json:"name"`
		Description   string            `json:"description"`
		DescriptionZH string            `json:"description_zh"`
		IconURL       string            `json:"iconUrl"`
		Category      string            `json:"category"`
		Homepage      string            `json:"homepage"`
		Source        string            `json:"source"`
		Stars         int               `json:"stars"`
		Downloads     int               `json:"downloads"`
		Score         int               `json:"score"`
		Labels        map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return catalog.MarketplaceItem{}, err
	}
	if !marketplaceSlugPattern.MatchString(record.Slug) || strings.TrimSpace(record.Name) == "" {
		return catalog.MarketplaceItem{}, nil
	}
	requiresAPIKey := strings.EqualFold(record.Labels["requires_api_key"], "true")
	return catalog.MarketplaceItem{ID: "skillhub-" + record.Slug, Category: firstNonEmpty(record.Category, "skill"), Type: "installable", Name: record.Name, Description: firstNonEmpty(record.DescriptionZH, record.Description), DescriptionEn: record.Description, Icon: "Puzzle", IconColor: "oklch(60% 0.16 75)", Scene: "productivity", Source: "skillhub", RequiresAPIKey: requiresAPIKey, SourceLabel: "SkillHub", SourceURL: "https://skillhub.cloud.tencent.com/skills/" + url.PathEscape(record.Slug), DocumentationURL: "https://skillhub.cloud.tencent.com/skills/" + url.PathEscape(record.Slug), IconURL: record.IconURL, Stars: record.Stars, Downloads: record.Downloads, Score: record.Score, ExternalURL: record.Homepage, InstallableKind: "skill"}, nil
}

func parseMCPServerPage(data []byte) ([]catalog.MarketplaceItem, int, error) {
	var payload struct {
		Items []struct {
			Slug, Name, NameEn, Category, Summary, SummaryZh, IconURL, RepoURL, SourceURL string
			Tags                                                                          []string                          `json:"tags"`
			Stats                                                                         struct{ Downloads, Installs int } `json:"stats"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, 0, fmt.Errorf("invalid MCP API payload: %w", err)
	}
	items := make([]catalog.MarketplaceItem, 0, len(payload.Items))
	for _, server := range payload.Items {
		if !marketplaceSlugPattern.MatchString(server.Slug) || strings.TrimSpace(server.Name) == "" {
			continue
		}
		docs := "https://skillhub.cloud.tencent.com/mcp-servers/" + url.PathEscape(server.Slug)
		items = append(items, catalog.MarketplaceItem{ID: "mcp-" + server.Slug, Category: "mcp-server", Type: "installable", Name: server.Name, Description: firstNonEmpty(server.SummaryZh, server.Summary), Icon: "Puzzle", IconColor: "oklch(55% 0.15 160)", Tags: server.Tags, Scene: "integration", Source: "mcpservers", SourceLabel: "MCP Servers", SourceURL: firstNonEmpty(server.SourceURL, server.RepoURL, docs), RepositoryURL: server.RepoURL, DocumentationURL: docs, ExternalURL: firstNonEmpty(server.RepoURL, server.SourceURL, docs), InstallableKind: "mcp", Downloads: server.Stats.Downloads})
	}
	return items, payload.Total, nil
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
	return err.Error()
}
func isPublicHTTPSURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() == "mcpservers.org" && parsed.User == nil
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
	return cache, nil
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
