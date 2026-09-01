package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

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
