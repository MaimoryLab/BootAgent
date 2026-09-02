package app

// The MCP Servers directory is a server-rendered application rather than a
// JSON API. Its public /all and /search pages are deliberately paginated and
// contain the same card metadata a user sees in a browser. This adapter parses
// those cards with an HTML tokenizer, while detail pages expose the published
// markdown document through the server-rendered serialized state.

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"golang.org/x/net/html"
)

const (
	mcpServersDirectoryBaseURL       = "https://mcpservers.org"
	mcpServersDirectoryPageSize      = 30
	mcpServersDirectoryMaxPageFetch  = 8
	mcpServersDirectoryMaxPage       = 10_000
	mcpServersDirectorySupplementMax = 100
	mcpServersDirectoryCacheTTL      = 5 * time.Minute
)

var (
	mcpServersDirectoryTotalPattern       = regexp.MustCompile(`(?i)Showing\s+[0-9,]+\s*-\s*[0-9,]+\s+of\s+([0-9,]+)\s+servers`)
	mcpServersDirectoryJSONTotalPattern   = regexp.MustCompile(`totalItems:(\d+)`)
	mcpServersDirectoryStarsPattern       = regexp.MustCompile(`(?i)([0-9][0-9,]*)\s+GitHub\s+stars`)
	mcpServersDirectoryPathSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)
	mcpServersDirectoryLocalePattern      = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z]{2,4})?$`)
)

type mcpServersDirectoryCacheEntry struct {
	items     []catalog.MarketplaceItem
	total     int
	fetchedAt time.Time
}

var mcpServersDirectorySupplementCache = struct {
	sync.Mutex
	entries map[string]mcpServersDirectoryCacheEntry
}{entries: make(map[string]mcpServersDirectoryCacheEntry)}

// fetchMCPPage composes the public MCP Servers directory with the small
// SkillHub MCP feed. The directory is the primary ordered stream; SkillHub
// records are prefixed as a bounded supplement so the existing 27 records do
// not disappear while the two upstream catalogs converge.
func (u *UseCases) fetchMCPPage(ctx context.Context, options MarketplaceDiscoverOptions, etag string) ([]catalog.MarketplaceItem, int, string, bool, bool, error) {
	_ = etag // The HTML directory exposes Last-Modified, not a stable ETag.
	query := strings.TrimSpace(options.Query)
	supplement, supplementErr := u.fetchMCPDirectorySupplement(ctx, query, options.ForceRefresh)

	// The first offset may straddle the SkillHub prefix. The directory range
	// helper returns an already-sliced range, so compose it only when the global
	// page still starts inside the prefix; otherwise return that range directly.
	supplementLen := len(supplement.items)
	orgOffset := options.Offset - supplementLen
	if orgOffset < 0 {
		orgOffset = 0
	}
	orgLimit := options.Limit
	if options.Offset < supplementLen {
		orgLimit = options.Limit - (supplementLen - options.Offset)
		if orgLimit < 0 {
			orgLimit = 0
		}
	}

	var directoryItems []catalog.MarketplaceItem
	var directoryTotal int
	var directoryErr error
	if orgLimit > 0 || options.Offset >= supplementLen {
		directoryOffset := orgOffset
		if options.Offset < supplementLen {
			directoryOffset = 0
		}
		directoryItems, directoryTotal, directoryErr = u.fetchMCPServersDirectoryRange(ctx, query, directoryOffset, orgLimit)
	}

	if directoryErr != nil && supplementErr != nil {
		return nil, 0, "", false, false, fmt.Errorf("MCP Servers directory unavailable: %v; SkillHub fallback unavailable: %v", directoryErr, supplementErr)
	}

	// If the directory is reachable, keep it as the authoritative total and
	// append the bounded SkillHub prefix before it. A future directory growth
	// therefore never causes the frontend cursor to stop at the first HTML page.
	if directoryErr == nil {
		combinedTotal := directoryTotal + supplementLen
		page := directoryItems
		if options.Offset < supplementLen {
			page = composeMCPDirectoryPage(supplement.items, directoryItems, options.Offset, options.Limit)
		}
		return page, combinedTotal, "", true, false, nil
	}

	// A temporary mcpservers.org failure still leaves the known SkillHub feed
	// usable. Marking this as fresh keeps the UI useful; the source status will
	// be refreshed again on the next interval.
	page := sliceMarketplaceItems(supplement.items, options.Offset, options.Limit)
	return page, len(supplement.items), supplement.etag, supplement.fresh, supplement.notModified, nil
}

type mcpDirectorySupplement struct {
	items       []catalog.MarketplaceItem
	etag        string
	fresh       bool
	notModified bool
}

func (u *UseCases) fetchMCPDirectorySupplement(ctx context.Context, query string, force bool) (mcpDirectorySupplement, error) {
	key := strings.ToLower(strings.TrimSpace(query))
	if u != nil && u.httpDoer == nil && !force {
		mcpServersDirectorySupplementCache.Lock()
		cached, ok := mcpServersDirectorySupplementCache.entries[key]
		mcpServersDirectorySupplementCache.Unlock()
		if ok && time.Since(cached.fetchedAt) < mcpServersDirectoryCacheTTL {
			// This is a bounded process cache, not a live response. Marking it
			// stale lets the source status explain why only the SkillHub fallback
			// is visible when the primary directory is unavailable.
			return mcpDirectorySupplement{items: append([]catalog.MarketplaceItem(nil), cached.items...), fresh: false}, nil
		}
	}

	// Keep the supplement bounded. The API currently has 27 records, and a
	// larger future feed must not turn every directory page into an unbounded
	// second catalog traversal.
	opts := MarketplaceDiscoverOptions{Query: query, Limit: marketplacePageSize, Offset: 0, ForceRefresh: force}
	items, total, etag, fresh, notModified, err := u.fetchSkillHubMCPPage(ctx, opts, "")
	if err != nil {
		return mcpDirectorySupplement{}, err
	}
	if total > len(items) {
		total = len(items)
	}
	if len(items) > mcpServersDirectorySupplementMax {
		items = items[:mcpServersDirectorySupplementMax]
	}
	entry := mcpServersDirectoryCacheEntry{items: append([]catalog.MarketplaceItem(nil), items...), total: total, fetchedAt: time.Now().UTC()}
	if u != nil && u.httpDoer == nil {
		mcpServersDirectorySupplementCache.Lock()
		mcpServersDirectorySupplementCache.entries[key] = entry
		mcpServersDirectorySupplementCache.Unlock()
	}
	return mcpDirectorySupplement{items: items, etag: etag, fresh: fresh, notModified: notModified}, nil
}

func composeMCPDirectoryPage(supplement, directory []catalog.MarketplaceItem, offset, limit int) []catalog.MarketplaceItem {
	combined := make([]catalog.MarketplaceItem, 0, len(supplement)+len(directory))
	combined = append(combined, supplement...)
	combined = append(combined, directory...)
	return sliceMarketplaceItems(combined, offset, limit)
}

func sliceMarketplaceItems(items []catalog.MarketplaceItem, offset, limit int) []catalog.MarketplaceItem {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || offset >= len(items) {
		return []catalog.MarketplaceItem{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return append([]catalog.MarketplaceItem(nil), items[offset:end]...)
}

func (u *UseCases) fetchMCPServersDirectoryRange(ctx context.Context, query string, offset, limit int) ([]catalog.MarketplaceItem, int, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		return []catalog.MarketplaceItem{}, 0, nil
	}
	firstPage := offset/mcpServersDirectoryPageSize + 1
	if firstPage > mcpServersDirectoryMaxPage {
		return nil, 0, fmt.Errorf("MCP Servers directory offset is too large")
	}
	skip := offset % mcpServersDirectoryPageSize
	needed := skip + limit
	collected := make([]catalog.MarketplaceItem, 0, needed)
	total := 0
	for page := firstPage; page < firstPage+mcpServersDirectoryMaxPageFetch && len(collected) < needed; page++ {
		items, pageTotal, err := u.fetchMCPServersDirectoryHTMLPage(ctx, query, page)
		if err != nil {
			return nil, 0, err
		}
		if pageTotal > total {
			total = pageTotal
		}
		collected = append(collected, items...)
		if len(items) < mcpServersDirectoryPageSize || (total > 0 && page*mcpServersDirectoryPageSize >= total) {
			break
		}
	}
	if total == 0 {
		total = offset + len(collected)
	}
	if skip >= len(collected) {
		return []catalog.MarketplaceItem{}, total, nil
	}
	end := min(skip+limit, len(collected))
	return append([]catalog.MarketplaceItem(nil), collected[skip:end]...), total, nil
}

func (u *UseCases) fetchMCPServersDirectoryHTMLPage(ctx context.Context, query string, page int) ([]catalog.MarketplaceItem, int, error) {
	target := mcpServersDirectoryPageURL(page, query)
	body, _, unchanged, err := u.fetchMarketplaceWithETag(ctx, target, "text/html, application/xhtml+xml;q=0.9", "")
	if err != nil {
		return nil, 0, err
	}
	if unchanged {
		return nil, 0, fmt.Errorf("MCP Servers directory returned not modified without a body")
	}
	items, total, err := parseMCPServersDirectoryPage([]byte(body))
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func mcpServersDirectoryPageURL(page int, query string) string {
	values := url.Values{"page": {strconv.Itoa(max(page, 1))}}
	if strings.TrimSpace(query) == "" {
		return mcpServersDirectoryBaseURL + "/all?" + values.Encode()
	}
	values.Set("query", strings.TrimSpace(query))
	return mcpServersDirectoryBaseURL + "/search?" + values.Encode()
}

func parseMCPServersDirectoryPage(data []byte) ([]catalog.MarketplaceItem, int, error) {
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("invalid MCP Servers directory HTML: %w", err)
	}
	items := make([]catalog.MarketplaceItem, 0, mcpServersDirectoryPageSize)
	seen := make(map[string]struct{})
	walkHTML(root, func(node *html.Node) {
		if node.Type != html.ElementNode || node.Data != "a" {
			return
		}
		path, ok := canonicalMCPServersDirectoryPath(attribute(node, "href"))
		if !ok || !hasHTMLAttribute(node, "data-slot", "card") && !hasDescendantHTMLAttribute(node, "data-slot", "card") {
			return
		}
		item := mcpServersDirectoryCardItem(node, path)
		if item.ID == "" || item.Name == "" {
			return
		}
		if _, exists := seen[item.ID]; exists {
			return
		}
		seen[item.ID] = struct{}{}
		items = append(items, item)
	})
	text := compactMarketplaceText(htmlText(root), 2_000_000)
	total := 0
	if match := mcpServersDirectoryTotalPattern.FindStringSubmatch(text); len(match) == 2 {
		total, _ = strconv.Atoi(strings.ReplaceAll(match[1], ",", ""))
	}
	if total == 0 {
		matches := mcpServersDirectoryJSONTotalPattern.FindAllSubmatch(data, -1)
		if len(matches) > 0 {
			total, _ = strconv.Atoi(string(matches[len(matches)-1][1]))
		}
	}
	if total == 0 {
		total = len(items)
	}
	// A page with no matches is a valid search result. A non-empty total with
	// no cards, however, indicates a changed upstream markup contract and must
	// fail loudly so the cached catalog is retained.
	if len(items) == 0 && total > 0 {
		return nil, 0, fmt.Errorf("MCP Servers directory page contains no recognizable cards")
	}
	return items, total, nil
}

func mcpServersDirectoryCardItem(anchor *html.Node, path string) catalog.MarketplaceItem {
	titleNode := firstHTMLNode(anchor, func(node *html.Node) bool {
		return hasHTMLAttribute(node, "data-slot", "card-title") || node.Type == html.ElementNode && node.Data == "h3"
	})
	name := compactMarketplaceText(htmlText(titleNode), 200)
	if name == "" {
		name = titleMarketplaceWords(strings.NewReplacer("-", " ", "_", " ").Replace(lastPathSegment(path)))
	}
	description := compactMarketplaceText(htmlText(firstHTMLNode(anchor, func(node *html.Node) bool {
		return hasHTMLAttribute(node, "data-slot", "card-description")
	})), 4_000)
	if description == "" {
		description = "MCP Server from MCP Servers"
	}
	category := mcpServersDirectoryCardCategory(titleNode)
	iconURL := mcpServersDirectoryImageURL(anchor)
	repositoryURL := mcpServersDirectoryRepositoryFromIcon(iconURL)
	stars := mcpServersDirectoryStars(anchor)
	official := mcpServersDirectoryHasBadge(anchor, "official")
	tags := []string{"MCP"}
	if category != "" {
		tags = append(tags, category)
	}
	if official && !containsString(tags, "official") {
		tags = append(tags, "official")
	}
	docs := mcpServersDirectoryCanonicalURL(path)
	trust := "community"
	if official {
		trust = "official"
	}
	return catalog.MarketplaceItem{
		ID: "mcp-" + strings.ReplaceAll(path, "/", "--"), Category: "mcp-server", Type: "installable", InstallableKind: "mcp",
		Name: name, Description: description, DescriptionEn: description,
		Icon: "Puzzle", IconColor: "oklch(55% 0.15 160)", Tags: tags, Scene: "integration",
		Source: "mcpservers", SourceLabel: "MCP Servers", SourceURL: docs, DocumentationURL: docs,
		RepositoryURL: repositoryURL, ExternalURL: firstNonEmpty(repositoryURL, docs), IconURL: iconURL,
		TrustLevel: trust, Stars: stars,
		InstallPrompt: mcpServersDirectoryInstallPrompt(name, docs),
		TargetHint:    "复制提示词到 MCP 配置页或 Agent 对话框，具体参数以官方文档为准",
	}
}

func mcpServersDirectoryInstallPrompt(name, docs string) string {
	return fmt.Sprintf("请帮我配置 MCP Server「%s」。\n\n请先打开 MCP Servers 官方说明：%s\n根据页面中的传输方式、安装命令和配置示例完成安装；如果需要密钥，使用占位符并提醒我在本地安全配置中填写，不要把密钥写入日志。", name, docs)
}

// FetchMarketplaceMCPServersDirectoryDetail returns live metadata for a card
// originating from mcpservers.org. The path, rather than a display slug, is
// validated so owner/name pairs remain unambiguous.
func (u *UseCases) FetchMarketplaceMCPServersDirectoryDetail(ctx context.Context, path string) (catalog.MarketplaceItem, error) {
	canonicalPath, err := normalizeMCPServersDirectoryPath(path)
	if err != nil {
		return catalog.MarketplaceItem{}, err
	}
	body, err := u.fetchMarketplace(ctx, mcpServersDirectoryCanonicalURL(canonicalPath), "text/html, application/xhtml+xml;q=0.9")
	if err != nil {
		return catalog.MarketplaceItem{}, err
	}
	item, _, err := parseMCPServersDirectoryDetail([]byte(body), canonicalPath)
	if err != nil {
		return catalog.MarketplaceItem{}, err
	}
	return item, nil
}

// FetchMarketplaceMCPServersDirectoryReadme returns the Markdown document
// published in the directory's detail page. It is extracted from the
// server-rendered serialized state, never executed as JavaScript.
func (u *UseCases) FetchMarketplaceMCPServersDirectoryReadme(ctx context.Context, path string) (string, error) {
	canonicalPath, err := normalizeMCPServersDirectoryPath(path)
	if err != nil {
		return "", err
	}
	body, err := u.fetchMarketplace(ctx, mcpServersDirectoryCanonicalURL(canonicalPath), "text/html, application/xhtml+xml;q=0.9")
	if err != nil {
		return "", err
	}
	_, markdown, err := parseMCPServersDirectoryDetail([]byte(body), canonicalPath)
	if err != nil {
		return "", err
	}
	return markdown, nil
}

func parseMCPServersDirectoryDetail(data []byte, path string) (catalog.MarketplaceItem, string, error) {
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return catalog.MarketplaceItem{}, "", fmt.Errorf("invalid MCP Servers detail HTML: %w", err)
	}
	region := mcpServersDirectorySerializedServerRegion(data)
	name := compactMarketplaceText(mcpServersDirectorySerializedString(region, "name"), 200)
	if name == "" {
		name = compactMarketplaceText(htmlText(firstHTMLNode(root, func(node *html.Node) bool { return node.Type == html.ElementNode && node.Data == "h1" })), 200)
	}
	description := compactMarketplaceText(mcpServersDirectorySerializedString(region, "description"), 4_000)
	if description == "" {
		description = compactMarketplaceText(mcpServersDirectoryMeta(root, "description"), 4_000)
	}
	if description == "" {
		description = compactMarketplaceText(mcpServersDirectoryMeta(root, "og:description"), 4_000)
	}
	if name == "" {
		name = titleMarketplaceWords(strings.NewReplacer("-", " ", "_", " ").Replace(lastPathSegment(path)))
	}
	if description == "" {
		description = "MCP Server from MCP Servers"
	}
	canonical := mcpServersDirectoryCanonicalURL(path)
	serializedURL := safeMarketplaceHTTPSURL(mcpServersDirectorySerializedString(region, "url"))
	repository := serializedURL
	if !isGitHubURL(repository) {
		repository = mcpServersDirectoryFirstExternalLink(root, true)
	}
	website := safeMarketplaceHTTPSURL(mcpServersDirectorySerializedString(region, "websiteUrl"))
	if website == "" && serializedURL != "" && !isGitHubURL(serializedURL) {
		// The directory calls its primary source `url`; for hosted/documentation
		// entries this is the useful external destination rather than a GitHub
		// repository. Preserve it as the website instead of silently falling back
		// to the directory page.
		website = serializedURL
	}
	if website == "" {
		website = mcpServersDirectoryFirstExternalLink(root, false)
	}
	iconURL := mcpServersDirectoryImageURL(root)
	if repository == "" {
		repository = mcpServersDirectoryRepositoryFromIcon(iconURL)
	}
	official := mcpServersDirectorySerializedBool(region, "official") || mcpServersDirectoryHasBadge(root, "official")
	category := compactMarketplaceText(mcpServersDirectorySerializedString(region, "category"), 80)
	if category == "" {
		category = mcpServersDirectoryDetailCategory(root)
	}
	tags := []string{"MCP"}
	for _, tag := range mcpServersDirectorySerializedArrayStrings(region, "tags") {
		tag = compactMarketplaceText(tag, 80)
		if tag != "" && !containsString(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if category != "" {
		if !containsString(tags, category) {
			tags = append(tags, category)
		}
	}
	if official && !containsString(tags, "official") {
		tags = append(tags, "official")
	}
	stars := mcpServersDirectorySerializedIntLast(data, "githubStars")
	if stars == 0 {
		stars = mcpServersDirectorySerializedInt(region, "githubStars")
	}
	updated := mcpServersDirectorySerializedDate(region, "repoPushedAt")
	markdown, ok := extractSerializedJSString(data, "markdownContent")
	if !ok || strings.TrimSpace(markdown) == "" {
		markdown = fmt.Sprintf("# %s\n\n%s\n\n官方目录：%s\n", name, description, canonical)
	}
	if len([]byte(markdown)) > 2<<20 {
		return catalog.MarketplaceItem{}, "", fmt.Errorf("MCP Servers documentation exceeds the size limit")
	}
	trust := "community"
	if official {
		trust = "official"
	}
	item := catalog.MarketplaceItem{
		ID: "mcp-" + strings.ReplaceAll(path, "/", "--"), Category: "mcp-server", Type: "installable", InstallableKind: "mcp",
		Name: name, Description: description, DescriptionEn: description,
		Icon: "Puzzle", IconColor: "oklch(55% 0.15 160)", Tags: tags, Scene: "integration",
		Source: "mcpservers", SourceLabel: "MCP Servers", SourceURL: canonical, DocumentationURL: canonical,
		RepositoryURL: repository, ExternalURL: firstNonEmpty(website, repository, canonical), IconURL: iconURL,
		TrustLevel: trust, Stars: stars, GitHubUpdatedAt: updated,
		InstallPrompt: mcpServersDirectoryInstallPrompt(name, canonical),
		TargetHint:    "复制提示词到 MCP 配置页或 Agent 对话框，具体参数以官方文档为准",
	}
	return item, markdown, nil
}

func canonicalMCPServersDirectoryPath(raw string) (string, bool) {
	path, err := normalizeMCPServersDirectoryPath(raw)
	return path, err == nil
}

func normalizeMCPServersDirectoryPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("invalid MCP Servers directory path")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid MCP Servers directory path")
	}
	if parsed.IsAbs() && (parsed.Scheme != "https" || parsed.Hostname() != "mcpservers.org" || parsed.User != nil) {
		return "", fmt.Errorf("invalid MCP Servers directory host")
	}
	if parsed.Host != "" && (parsed.Scheme != "https" || parsed.Hostname() != "mcpservers.org" || parsed.User != nil) {
		return "", fmt.Errorf("invalid MCP Servers directory host")
	}
	escapedPath := strings.ToLower(parsed.EscapedPath())
	if strings.Contains(escapedPath, "%2f") || strings.Contains(escapedPath, "%5c") || strings.Contains(escapedPath, "%2e") {
		return "", fmt.Errorf("invalid MCP Servers directory path")
	}
	path := parsed.Path
	if path == "" {
		path = raw
	}
	path = strings.Trim(path, "/")
	// Detail links may be localized. Accept only the documented root route or
	// one locale segment; never search for an arbitrary later "/servers/"
	// substring, which could turn malformed input into a different resource.
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) >= 2 && parts[0] == "servers" {
		path = strings.Join(parts[1:], "/")
	} else if len(parts) >= 3 && mcpServersDirectoryLocalePattern.MatchString(parts[0]) && parts[1] == "servers" {
		path = strings.Join(parts[2:], "/")
	} else if parsed.Host == "" && !strings.HasPrefix(raw, "/") && parsed.Path == raw {
		// The frontend passes the already-canonical owner/name path after it has
		// read DocumentationURL. Keep this form accepted for the binding while
		// rejecting arbitrary slash-containing URL paths above.
		path = strings.Join(parts, "/")
	} else {
		return "", fmt.Errorf("invalid MCP Servers directory path")
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 || len(segments) > 4 {
		return "", fmt.Errorf("invalid MCP Servers directory path")
	}
	for _, segment := range segments {
		if !mcpServersDirectoryPathSegmentPattern.MatchString(segment) {
			return "", fmt.Errorf("invalid MCP Servers directory path")
		}
	}
	return strings.Join(segments, "/"), nil
}

func mcpServersDirectoryCanonicalURL(path string) string {
	return mcpServersDirectoryBaseURL + "/servers/" + strings.Join(strings.Split(path, "/"), "/")
}

func mcpServersDirectoryCardCategory(title *html.Node) string {
	if title == nil || title.Parent == nil {
		return ""
	}
	foundTitle := false
	for child := title.Parent.FirstChild; child != nil; child = child.NextSibling {
		if child == title {
			foundTitle = true
			continue
		}
		if foundTitle && child.Type == html.ElementNode {
			value := compactMarketplaceText(htmlText(child), 80)
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func mcpServersDirectoryImageURL(root *html.Node) string {
	// The detail page starts with the site's favicon, then the server logo. Pick
	// the most specific candidate instead of accidentally publishing /icon.png
	// as every card's icon. Card anchors normally contain one image and therefore
	// take the same path through this scorer.
	var best string
	bestScore := -1
	walkHTML(root, func(node *html.Node) {
		if node.Type != html.ElementNode || node.Data != "img" {
			return
		}
		src := strings.TrimSpace(attribute(node, "src"))
		if src == "" || strings.HasPrefix(strings.ToLower(src), "data:") {
			return
		}
		candidate := src
		if strings.HasPrefix(candidate, "/") {
			candidate = mcpServersDirectoryBaseURL + candidate
		}
		candidate = safeMarketplaceHTTPSURL(candidate)
		if candidate == "" {
			return
		}
		score := 0
		lower := strings.ToLower(candidate)
		if strings.Contains(lower, "/api/logo") {
			score += 100
		}
		if strings.Contains(lower, "simpleicons.org") {
			score += 80
		}
		if strings.HasSuffix(lower, ".svg") || strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".webp") {
			score += 10
		}
		if strings.HasSuffix(strings.ToLower(strings.TrimSuffix(candidate, "/")), "/icon.png") {
			score -= 100
		}
		if alt := strings.TrimSpace(attribute(node, "alt")); alt != "" {
			score += 5
		}
		if score > bestScore {
			best, bestScore = candidate, score
		}
	})
	return best
}

func mcpServersDirectoryRepositoryFromIcon(iconURL string) string {
	parsed, err := url.Parse(iconURL)
	if err != nil || parsed.Hostname() != "mcpservers.org" {
		return ""
	}
	return safeMarketplaceHTTPSURL(parsed.Query().Get("url"))
}

func mcpServersDirectoryStars(root *html.Node) int {
	var stars int
	walkHTML(root, func(node *html.Node) {
		if stars > 0 {
			return
		}
		value := attribute(node, "title")
		match := mcpServersDirectoryStarsPattern.FindStringSubmatch(value)
		if len(match) == 2 {
			stars, _ = strconv.Atoi(strings.ReplaceAll(match[1], ",", ""))
		}
	})
	return stars
}

func mcpServersDirectoryHasBadge(root *html.Node, wanted string) bool {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	found := false
	walkHTML(root, func(node *html.Node) {
		if found || node.Type != html.ElementNode {
			return
		}
		text := strings.ToLower(strings.TrimSpace(htmlText(node)))
		if text == wanted {
			found = true
		}
	})
	return found
}

func mcpServersDirectoryDetailCategory(root *html.Node) string {
	var category string
	walkHTML(root, func(node *html.Node) {
		if category != "" || node.Type != html.ElementNode || node.Data != "a" {
			return
		}
		href := attribute(node, "href")
		if strings.HasPrefix(href, "/category/") {
			category = compactMarketplaceText(htmlText(node), 80)
		}
	})
	return category
}

func mcpServersDirectoryFirstExternalLink(root *html.Node, githubOnly bool) string {
	var result string
	walkHTML(root, func(node *html.Node) {
		if result != "" || node.Type != html.ElementNode || node.Data != "a" {
			return
		}
		href := safeMarketplaceHTTPSURL(attribute(node, "href"))
		if href == "" {
			return
		}
		// Do not turn sponsored placements, navigation, or links embedded in the
		// README into the card's installation target. The detail page's own
		// repository/source buttons carry data-slot="button"; only those are
		// considered as a conservative fallback when serialized metadata is absent.
		if !hasHTMLAttribute(node, "data-slot", "button") {
			return
		}
		rel := strings.ToLower(attribute(node, "rel"))
		if strings.Contains(rel, "sponsored") || attribute(node, "data-umami-event") != "" {
			return
		}
		host := ""
		if parsed, err := url.Parse(href); err == nil {
			host = parsed.Hostname()
		}
		isGitHub := host == "github.com" || strings.HasSuffix(host, ".github.com")
		if githubOnly == isGitHub && host != "mcpservers.org" {
			result = href
		}
	})
	return result
}

func mcpServersDirectoryMeta(root *html.Node, key string) string {
	var result string
	walkHTML(root, func(node *html.Node) {
		if result != "" || node.Type != html.ElementNode || node.Data != "meta" {
			return
		}
		property := attribute(node, "property")
		name := attribute(node, "name")
		if property == key || name == key {
			result = strings.TrimSpace(attribute(node, "content"))
		}
	})
	return result
}

func mcpServersDirectorySerializedInt(data []byte, key string) int {
	pattern := regexp.MustCompile(regexp.QuoteMeta(key) + `:(?:\$R\[\d+\]=)?(\d+)`)
	match := pattern.FindSubmatch(data)
	if len(match) != 2 {
		return 0
	}
	value, _ := strconv.Atoi(string(match[1]))
	return value
}

func mcpServersDirectorySerializedIntLast(data []byte, key string) int {
	needle := []byte(key + ":")
	index := bytes.LastIndex(data, needle)
	if index < 0 {
		return 0
	}
	return mcpServersDirectorySerializedInt(data[index:], key)
}

func mcpServersDirectorySerializedDate(data []byte, key string) string {
	pattern := regexp.MustCompile(regexp.QuoteMeta(key) + `:\$R\[\d+\]=new Date\("([^"]+)"\)`)
	match := pattern.FindSubmatch(data)
	if len(match) != 2 {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, string(match[1]))
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}

// mcpServersDirectorySerializedServerRegion isolates the detail page's
// `server` record from the preceding featured/list records. The page uses a
// small JavaScript serialization format, so parsing a bounded region keeps a
// generic property lookup from accidentally selecting metadata for another
// server rendered earlier in the document.
func mcpServersDirectorySerializedServerRegion(data []byte) []byte {
	start := bytes.Index(data, []byte("server:$R["))
	if start < 0 {
		return nil
	}
	openRelative := bytes.IndexByte(data[start:], '{')
	if openRelative < 0 || openRelative > 128 {
		return nil
	}
	open := start + openRelative
	end := len(data)
	for _, marker := range [][]byte{[]byte(",remoteServer:"), []byte(",descriptionTranslationFallback:"), []byte(",markdownContent:")} {
		if relative := bytes.Index(data[open:], marker); relative >= 0 && open+relative < end {
			end = open + relative
		}
	}
	if end <= open {
		return nil
	}
	return data[open:end]
}

func mcpServersDirectorySerializedProperty(region []byte, key string) (int, int, bool) {
	if len(region) == 0 || strings.TrimSpace(key) == "" {
		return 0, 0, false
	}
	needle := []byte(key + ":")
	for start := 0; start < len(region); {
		relative := bytes.Index(region[start:], needle)
		if relative < 0 {
			return 0, 0, false
		}
		index := start + relative
		if index == 0 || region[index-1] == ',' || region[index-1] == '{' {
			return index + len(needle), len(region), true
		}
		start = index + 1
	}
	return 0, 0, false
}

func mcpServersDirectorySerializedString(region []byte, key string) string {
	start, _, ok := mcpServersDirectorySerializedProperty(region, key)
	if !ok {
		return ""
	}
	for start < len(region) && (region[start] == ' ' || region[start] == '\t' || region[start] == '\r' || region[start] == '\n') {
		start++
	}
	if start >= len(region) || region[start] != '"' {
		return ""
	}
	value, _, ok := decodeSerializedJSString(region, start)
	if !ok {
		return ""
	}
	return value
}

func mcpServersDirectorySerializedBool(region []byte, key string) bool {
	start, _, ok := mcpServersDirectorySerializedProperty(region, key)
	if !ok {
		return false
	}
	for start < len(region) && (region[start] == ' ' || region[start] == '\t' || region[start] == '\r' || region[start] == '\n') {
		start++
	}
	value := region[start:]
	return bytes.HasPrefix(value, []byte("true")) || bytes.HasPrefix(value, []byte("!0"))
}

func mcpServersDirectorySerializedArrayStrings(region []byte, key string) []string {
	start, _, ok := mcpServersDirectorySerializedProperty(region, key)
	if !ok {
		return nil
	}
	for start < len(region) && (region[start] == ' ' || region[start] == '\t' || region[start] == '\r' || region[start] == '\n' || region[start] == '$') {
		// `$R[12]=` may precede an array value. Skip the assignment without
		// interpreting any executable content.
		if region[start] == '$' {
			if equal := bytes.IndexByte(region[start:], '='); equal >= 0 {
				start += equal + 1
				continue
			}
		}
		start++
	}
	if start >= len(region) || region[start] != '[' {
		return nil
	}
	end := serializedBracketEnd(region, start, '[', ']')
	if end <= start {
		return nil
	}
	array := region[start+1 : end]
	result := make([]string, 0, 8)
	for index := 0; index < len(array); {
		if array[index] != '"' {
			index++
			continue
		}
		value, next, valid := decodeSerializedJSString(array, index)
		if !valid {
			break
		}
		result = append(result, value)
		index = next
	}
	return result
}

func serializedBracketEnd(data []byte, start int, open, close byte) int {
	depth := 0
	quoted := false
	escaped := false
	for index := int(start); index < len(data); index++ {
		char := data[index]
		if quoted {
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				quoted = false
			}
			continue
		}
		if char == '"' {
			quoted = true
			continue
		}
		if char == open {
			depth++
		} else if char == close {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func isGitHubURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "github.com" || strings.HasSuffix(host, ".github.com")
}

// extractSerializedJSString reads one quoted property from the server's
// serialized state. It understands JSON/JavaScript escapes but never executes
// the script, which keeps untrusted documentation inert at the boundary.
func extractSerializedJSString(data []byte, key string) (string, bool) {
	needle := []byte(key + ":")
	for start := 0; ; {
		relative := bytes.Index(data[start:], needle)
		if relative < 0 {
			return "", false
		}
		index := start + relative + len(needle)
		for index < len(data) && (data[index] == ' ' || data[index] == '\t' || data[index] == '\r' || data[index] == '\n') {
			index++
		}
		if index >= len(data) || data[index] != '"' {
			start = index
			continue
		}
		value, _, ok := decodeSerializedJSString(data, index)
		if ok {
			return value, true
		}
		start = index + 1
	}
}

func decodeSerializedJSString(data []byte, start int) (string, int, bool) {
	if start >= len(data) || data[start] != '"' {
		return "", start, false
	}
	var builder strings.Builder
	for index := start + 1; index < len(data); index++ {
		char := data[index]
		if char == '"' {
			return builder.String(), index + 1, true
		}
		if char != '\\' {
			builder.WriteByte(char)
			continue
		}
		if index+1 >= len(data) {
			return "", index, false
		}
		index++
		switch data[index] {
		case 'n':
			builder.WriteByte('\n')
		case 'r':
			builder.WriteByte('\r')
		case 't':
			builder.WriteByte('\t')
		case 'b':
			builder.WriteByte('\b')
		case 'f':
			builder.WriteByte('\f')
		case '\\', '/', '"':
			builder.WriteByte(data[index])
		case 'u':
			if index+4 >= len(data) {
				return "", index, false
			}
			value, err := strconv.ParseUint(string(data[index+1:index+5]), 16, 16)
			if err != nil {
				return "", index, false
			}
			builder.WriteRune(rune(value))
			index += 4
		default:
			// JavaScript permits escaped line continuations. Preserve unknown
			// escapes as their literal character instead of rejecting all docs.
			builder.WriteByte(data[index])
		}
	}
	return "", len(data), false
}

func lastPathSegment(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func walkHTML(root *html.Node, visit func(*html.Node)) {
	if root == nil {
		return
	}
	visit(root)
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}

func firstHTMLNode(root *html.Node, predicate func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if predicate(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if result := firstHTMLNode(child, predicate); result != nil {
			return result
		}
	}
	return nil
}

func hasDescendantHTMLAttribute(root *html.Node, key, value string) bool {
	if root == nil {
		return false
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if hasHTMLAttribute(child, key, value) || hasDescendantHTMLAttribute(child, key, value) {
			return true
		}
	}
	return false
}

func hasHTMLAttribute(node *html.Node, key, value string) bool {
	return node != nil && strings.EqualFold(attribute(node, key), value)
}

func attribute(node *html.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func htmlText(root *html.Node) string {
	if root == nil {
		return ""
	}
	if root.Type == html.TextNode {
		return root.Data
	}
	if root.Type == html.ElementNode && (root.Data == "script" || root.Data == "style") {
		return ""
	}
	var builder strings.Builder
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(htmlText(child))
	}
	return builder.String()
}
