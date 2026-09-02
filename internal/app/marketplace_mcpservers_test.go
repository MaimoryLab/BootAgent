package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestParseMCPServersDirectoryPageExtractsCardsAndTotal(t *testing.T) {
	data := []byte(`<!doctype html><html><body>
<p>Showing 1-2 of 11762 servers</p>
<a href="/servers/acme/demo"><div data-slot="card">
  <div><img src="/api/logo?url=https%3A%2F%2Fgithub.com%2Facme%2Fdemo&amp;size=64">
    <div data-slot="card-title">Demo Server</div><div>Development</div>
  </div><div data-slot="card-description">A useful demo server.</div>
  <span title="42 GitHub stars">42</span><span>official</span>
</div></a>
<a href="/servers/acme/other"><div data-slot="card">
  <div><img src="/api/logo?url=https%3A%2F%2Fexample.com%2Fother&amp;size=64">
    <div data-slot="card-title">Other Server</div><div>Database</div>
  </div><div data-slot="card-description">Another server.</div>
</div></a>
</body></html>`)

	items, total, err := parseMCPServersDirectoryPage(data)
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	if total != 11762 || len(items) != 2 {
		t.Fatalf("items=%d total=%d", len(items), total)
	}
	if items[0].ID != "mcp-acme--demo" || items[0].Name != "Demo Server" || items[0].Description != "A useful demo server." {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
	if items[0].RepositoryURL != "https://github.com/acme/demo" || items[0].IconURL == "" || items[0].Stars != 42 || items[0].TrustLevel != "official" {
		t.Fatalf("unexpected first metadata: %+v", items[0])
	}
	if items[1].ID != "mcp-acme--other" || items[1].Tags[1] != "Database" {
		t.Fatalf("unexpected second item: %+v", items[1])
	}
}

func TestParseMCPServersDirectoryDetailUsesSerializedServerAndMarkdown(t *testing.T) {
	data := []byte(`<!doctype html><html><head>
<meta name="description" content="fallback description">
</head><body>
<header><img src="/icon.png" alt=""></header>
<main><h1>Wrong fallback title</h1>
<div><img src="/api/logo?url=https%3A%2F%2Fgithub.com%2Facme%2Fdemo&amp;size=64" alt="Demo logo"></div>
<a data-slot="button" href="https://github.com/acme/demo">GitHub repository</a>
<span>official</span><a href="/category/development">Development</a>
</main>
<script>server:$R[1]={id:1,slug:"acme/demo",name:"Demo Server",description:"Serialized description",url:"https://github.com/acme/demo",websiteUrl:null,category:"development",tags:$R[2]=["one","two"],official:!0,repoPushedAt:$R[3]=new Date("2026-01-02T03:04:05.000Z"),githubStars:42},remoteServer:null,markdownContent:"# Demo Server\n\n[Docs](./docs)"</script>
</body></html>`)

	item, markdown, err := parseMCPServersDirectoryDetail(data, "acme/demo")
	if err != nil {
		t.Fatalf("parse detail: %v", err)
	}
	if item.Name != "Demo Server" || item.Description != "Serialized description" || item.RepositoryURL != "https://github.com/acme/demo" {
		t.Fatalf("unexpected detail item: %+v", item)
	}
	if item.ExternalURL != item.RepositoryURL || item.IconURL == "https://mcpservers.org/icon.png" {
		t.Fatalf("unexpected external/icon URLs: external=%q icon=%q", item.ExternalURL, item.IconURL)
	}
	if item.Stars != 42 || item.GitHubUpdatedAt != "2026-01-02T03:04:05Z" || item.TrustLevel != "official" {
		t.Fatalf("unexpected serialized metadata: %+v", item)
	}
	if !containsString(item.Tags, "one") || !containsString(item.Tags, "development") || !strings.Contains(markdown, "# Demo Server\n\n[Docs](./docs)") {
		t.Fatalf("tags=%v markdown=%q", item.Tags, markdown)
	}
}

func TestNormalizeMCPServersDirectoryPathAcceptsCanonicalAndLocaleRoutes(t *testing.T) {
	tests := map[string]string{
		"acme/demo":          "acme/demo",
		"/servers/acme/demo": "acme/demo",
		"https://mcpservers.org/servers/acme/demo":       "acme/demo",
		"https://mcpservers.org/zh-CN/servers/acme/demo": "acme/demo",
	}
	for input, expected := range tests {
		got, err := normalizeMCPServersDirectoryPath(input)
		if err != nil || got != expected {
			t.Errorf("normalize(%q)=%q err=%v", input, got, err)
		}
	}
	for _, input := range []string{
		"http://mcpservers.org/servers/acme/demo",
		"https://evil.example/servers/acme/demo",
		"https://user:pass@mcpservers.org/servers/acme/demo",
		"/evil/servers/acme/demo",
		"https://mcpservers.org/servers/acme%2Fdemo",
		"/servers/../secret",
		"/servers/acme/demo/too/many/segments/here",
	} {
		if _, err := normalizeMCPServersDirectoryPath(input); err == nil {
			t.Errorf("unsafe directory path accepted: %q", input)
		}
	}
}

func TestFetchMCPPageCombinesSkillHubSupplementAndDirectoryCursor(t *testing.T) {
	var requested []string
	core := marketplaceCore(t, func(request *http.Request) (*http.Response, error) {
		requested = append(requested, request.URL.String())
		if request.URL.Host == "api.skillhub.cn" {
			return marketplaceResponse(http.StatusOK, `{"total":1,"items":[{"slug":"supplement","name":"Supplement MCP","summary":"supplement"}]}`), nil
		}
		page := request.URL.Query().Get("page")
		body := `<!doctype html><html><body><p>Showing 1-30 of 60 servers</p>`
		for index := 0; index < 30; index++ {
			id := fmt.Sprintf("%s-%02d", page, index)
			body += `<a href="/servers/acme/` + id + `"><div data-slot="card"><div data-slot="card-title">` + id + `</div><div data-slot="card-description">desc</div></div></a>`
		}
		body += `</body></html>`
		return marketplaceResponse(http.StatusOK, body), nil
	})

	result, err := core.DiscoverMarketplaceSources(context.Background(), MarketplaceDiscoverOptions{Source: "mcpservers", Limit: 30, Offset: 30})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(result.Items) != 30 || result.Total != 61 || result.NextOffset != 60 || !result.HasMore {
		t.Fatalf("unexpected result: items=%d total=%d next=%d more=%v", len(result.Items), result.Total, result.NextOffset, result.HasMore)
	}
	if !strings.Contains(strings.Join(requested, "\n"), "mcpservers.org/all?page=2") {
		t.Fatalf("directory page 2 was not requested: %v", requested)
	}
}

func TestMarketplaceHTTPClientBlocksUntrustedRedirects(t *testing.T) {
	client := marketplaceHTTPClient()
	if err := client.CheckRedirect(&http.Request{URL: mustMarketplaceURL("https://mcpservers.org/all?page=2")}, nil); err != nil {
		t.Fatalf("same-host redirect blocked: %v", err)
	}
	if err := client.CheckRedirect(&http.Request{URL: mustMarketplaceURL("http://mcpservers.org/all")}, nil); err == nil {
		t.Fatal("http redirect accepted")
	}
	if err := client.CheckRedirect(&http.Request{URL: mustMarketplaceURL("https://127.0.0.1/private")}, nil); err == nil {
		t.Fatal("private redirect accepted")
	}
	if marketplaceAllowedHTTPURL(mustMarketplaceURL("https://user:pass@mcpservers.org/all")) {
		t.Fatal("redirect with credentials accepted")
	}
}

func mustMarketplaceURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}
