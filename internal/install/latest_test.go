package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
)

type latestDoer func(*http.Request) (*http.Response, error)

func (fn latestDoer) Do(request *http.Request) (*http.Response, error) { return fn(request) }

func latestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func npmAgent(name string) catalog.Agent {
	return catalog.Agent{Name: "Test", Package: &catalog.Package{Manager: "npm", Name: name}}
}

func TestLatestVersionReadsDistTagAndAsksForTheAbbreviatedDocument(t *testing.T) {
	var seen *http.Request
	client := latestDoer(func(request *http.Request) (*http.Response, error) {
		seen = request
		return latestResponse(http.StatusOK, `{"dist-tags":{"latest":"1.2.3","next":"2.0.0-beta.1"}}`), nil
	})
	if got := LatestVersion(context.Background(), client, npmAgent("@openai/codex"), "https://registry.npmjs.org/"); got != "1.2.3" {
		t.Fatalf("LatestVersion() = %q, want 1.2.3", got)
	}
	// The full packument carries every version's metadata, which is megabytes for
	// the larger Agents; the abbreviated document answers the same question.
	if got := seen.Header.Get("Accept"); got != "application/vnd.npm.install-v1+json" {
		t.Fatalf("Accept = %q", got)
	}
	// A scoped name keeps its slash unescaped, which is what registries serve.
	if got := seen.URL.String(); got != "https://registry.npmjs.org/@openai/codex" {
		t.Fatalf("URL = %q", got)
	}
}

func TestLatestVersionIsSilentOnEveryFailureTheUserDidNotAskAbout(t *testing.T) {
	transport := errors.New("no route to host")
	for name, client := range map[string]Doer{
		"transport error": latestDoer(func(*http.Request) (*http.Response, error) { return nil, transport }),
		"rate limited": latestDoer(func(*http.Request) (*http.Response, error) {
			return latestResponse(http.StatusTooManyRequests, ""), nil
		}),
		"not found":       latestResponder(http.StatusNotFound, `{"error":"Not found"}`),
		"captive portal":  latestResponder(http.StatusOK, "<html>sign in to continue</html>"),
		"no dist-tags":    latestResponder(http.StatusOK, `{"name":"x"}`),
		"empty latest":    latestResponder(http.StatusOK, `{"dist-tags":{"latest":""}}`),
		"range not exact": latestResponder(http.StatusOK, `{"dist-tags":{"latest":"^1.2.3"}}`),
		"shell injection": latestResponder(http.StatusOK, `{"dist-tags":{"latest":"1.2.3; rm -rf /"}}`),
		"wrong type":      latestResponder(http.StatusOK, `{"dist-tags":{"latest":123}}`),
		"nil client":      nil,
	} {
		if got := LatestVersion(context.Background(), client, npmAgent("pkg"), ""); got != "" {
			t.Errorf("%s: LatestVersion() = %q, want \"\"", name, got)
		}
	}
}

func TestLatestVersionSkipsWhatIsNotAnNPMPackage(t *testing.T) {
	client := latestResponder(http.StatusOK, `{"dist-tags":{"latest":"9.9.9"}}`)
	// Aider comes from PyPI through uv, whose metadata shape is different, and a
	// guide-only entry has no package at all. Neither may borrow the npm answer.
	for name, agent := range map[string]catalog.Agent{
		"uv managed": {Name: "Aider", Package: &catalog.Package{Manager: "uv", Name: "aider-chat"}},
		"no package": {Name: "Cursor"},
		"no manager": {Name: "Odd", Package: &catalog.Package{Name: "x"}},
	} {
		if got := LatestVersion(context.Background(), client, agent, ""); got != "" {
			t.Errorf("%s: LatestVersion() = %q, want \"\"", name, got)
		}
	}
}

func TestLatestVersionRefusesToLeakInstalledAgentsOverPlainHTTP(t *testing.T) {
	called := false
	client := latestDoer(func(*http.Request) (*http.Response, error) {
		called = true
		return latestResponse(http.StatusOK, `{"dist-tags":{"latest":"1.0.0"}}`), nil
	})
	// Which Agents a user has installed is not something to put on the wire in
	// the clear for the sake of a decoration.
	for _, registry := range []string{"http://registry.example.com/", "ftp://registry.example.com/", "://broken"} {
		if got := LatestVersion(context.Background(), client, npmAgent("pkg"), registry); got != "" {
			t.Errorf("registry %q returned %q", registry, got)
		}
	}
	if called {
		t.Fatal("a non-https registry still reached the network")
	}
}

func TestLatestVersionDefaultsToTheOfficialRegistryAndRejectsTraversal(t *testing.T) {
	var seen string
	client := latestDoer(func(request *http.Request) (*http.Response, error) {
		seen = request.URL.String()
		return latestResponse(http.StatusOK, `{"dist-tags":{"latest":"1.0.0"}}`), nil
	})
	if got := LatestVersion(context.Background(), client, npmAgent("pkg"), ""); got != "1.0.0" {
		t.Fatalf("LatestVersion() with empty registry = %q", got)
	}
	if !strings.HasPrefix(seen, officialRegistry()) {
		t.Fatalf("empty registry did not fall back to the official one: %q", seen)
	}
	seen = ""
	if got := LatestVersion(context.Background(), client, npmAgent("../../etc/passwd"), ""); got != "" || seen != "" {
		t.Fatalf("traversal in a package name reached %q and returned %q", seen, got)
	}
}

func TestLatestVersionReadsADocumentTooLargeToHoldInMemory(t *testing.T) {
	// The real reason this streams. opencode-ai's abbreviated document is 13 MB
	// because it carries an entry per published version, and dist-tags sits in
	// the first 24 bytes. Decoding the whole object into a struct forced a choice
	// between a size cap that truncated the JSON -- unexpected EOF with the answer
	// already read -- and no cap, letting a registry decide the memory bill.
	var body strings.Builder
	body.WriteString(`{"dist-tags":{"latest":"1.18.14"},"versions":{`)
	for index := range 60000 {
		if index > 0 {
			body.WriteString(",")
		}
		fmt.Fprintf(&body, `"0.0.%d":{"name":"opencode-ai","dist":{"tarball":"https://registry.example/%d.tgz"}}`, index, index)
	}
	body.WriteString("}}")
	if body.Len() < 4<<20 {
		t.Fatalf("fixture is only %d bytes; it must exceed a naive read cap to be the case in question", body.Len())
	}
	client := latestResponder(http.StatusOK, body.String())
	if got := LatestVersion(context.Background(), client, npmAgent("opencode-ai"), ""); got != "1.18.14" {
		t.Fatalf("LatestVersion() = %q, want 1.18.14", got)
	}
}

func TestLatestVersionFindsTheTagAfterKeysItDoesNotCareAbout(t *testing.T) {
	// dist-tags is near the front in practice but nothing guarantees ordering, so
	// preceding values of every shape have to be skipped rather than parsed.
	body := `{"_id":"pkg","nested":{"a":{"b":[1,2,{"c":null}]}},"list":[[],{},"x"],"n":12.5,"t":true,` +
		`"dist-tags":{"beta":"2.0.0-rc.1","latest":"3.1.4"}}`
	if got := LatestVersion(context.Background(), latestResponder(http.StatusOK, body), npmAgent("pkg"), ""); got != "3.1.4" {
		t.Fatalf("LatestVersion() = %q, want 3.1.4", got)
	}
}

func TestLatestVersionStopsReadingAnEndlessBody(t *testing.T) {
	// A registry that never stops sending must not be able to exhaust memory.
	client := latestDoer(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(endlessReader{})}, nil
	})
	if got := LatestVersion(context.Background(), client, npmAgent("pkg"), ""); got != "" {
		t.Fatalf("LatestVersion() = %q, want \"\"", got)
	}
}

type endlessReader struct{}

func (endlessReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = ' '
	}
	return len(buffer), nil
}

func latestResponder(status int, body string) Doer {
	return latestDoer(func(*http.Request) (*http.Response, error) { return latestResponse(status, body), nil })
}
