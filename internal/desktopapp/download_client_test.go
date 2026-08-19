package desktopapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
)

// The mirror's last hop is a presigned CDN URL whose query holds the asset file
// name with a raw space in it. Left alone, net/http writes that space into the
// request line and the CDN answers 400, which is the failure users hit on every
// mirror install.
func TestDownloadFollowsRedirectWithUnencodedSpaceInQuery(t *testing.T) {
	var servedTarget string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/downloads/mac":
			// Written straight to the header so the space survives, exactly as
			// the real redirect delivers it.
			w.Header()["Location"] = []string{"/cdn/object?filename=DSH Desktop-2.0.1-universal.dmg&auth_key=abc"}
			w.WriteHeader(http.StatusFound)
		case "/cdn/object":
			servedTarget = r.RequestURI
			if r.URL.Query().Get("auth_key") != "abc" {
				http.Error(w, "signature lost", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte("installer bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "dsh.dmg")
	// Downloader stays nil so this exercises the client the production path uses.
	if err := downloadFile(context.Background(), Options{}, server.URL+"/api/downloads/mac", destination, DSHDesktopID); err != nil {
		t.Fatalf("download through a redirect with a raw space: %v", err)
	}
	if strings.Contains(servedTarget, " ") {
		t.Errorf("request target still carries a raw space: %q", servedTarget)
	}
	if !strings.Contains(servedTarget, "auth_key=abc") {
		t.Errorf("presigned query did not survive the repair: %q", servedTarget)
	}
	body, err := os.ReadFile(destination)
	if err != nil || string(body) != "installer bytes" {
		t.Fatalf("downloaded file = %q, %v", body, err)
	}
}

func TestEncodeQuerySpacesLeavesSignedQueriesAlone(t *testing.T) {
	// Byte-for-byte identity matters: a presigned URL is validated on the exact
	// query it was signed with, so anything beyond the space must not be touched.
	signed := "filename=plain.dmg&auth_key=1787118319-d6da049f-0-6a1a4258&tag=model"
	if got := encodeQuerySpaces(signed); got != signed {
		t.Errorf("encodeQuerySpaces rewrote a query with no spaces:\n got %q\nwant %q", got, signed)
	}
	if got, want := encodeQuerySpaces("filename=A B C.dmg&k=v"), "filename=A%20B%20C.dmg&k=v"; got != want {
		t.Errorf("encodeQuerySpaces = %q, want %q", got, want)
	}
}

// macOS ships one universal .dmg. Requiring "arm64" in the name matched no
// release that has ever been published, so the official GitHub route was dead on
// macOS and the mirror was the only way in.
func TestDSHAssetMatchesPublishedReleaseNames(t *testing.T) {
	for _, test := range []struct {
		name    string
		ext     string
		macOS   bool
		matches bool
	}{
		{"DSH.Desktop-2.0.1-universal.dmg", ".dmg", true, true},
		{"DSH-Desktop-2.0.1-arm64.dmg", ".dmg", true, true},
		{"DSH-Desktop-2.0.1-x64-Setup.exe", ".exe", false, true},
		{"DSH-Desktop-2.0.1-x64-Setup.exe", ".dmg", true, false},
		{"DSH.Desktop-2.0.1-universal.dmg", ".exe", false, false},
		// An Intel-only build is refused; dshURL's arch guard is what keeps an
		// Intel Mac from getting this far, but the name must not claim it either.
		{"DSH-Desktop-2.0.1-x64.dmg", ".dmg", true, false},
	} {
		if got := dshAssetMatches(test.name, test.ext, test.macOS); got != test.matches {
			t.Errorf("dshAssetMatches(%q, %q, macOS=%v) = %v, want %v", test.name, test.ext, test.macOS, got, test.matches)
		}
	}
}

// The official lookup has to resolve on macOS, since it is both the non-mirror
// route and the fallback a mirror failure depends on.
func TestDSHURLResolvesTheUniversalAssetFromGitHub(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"assets":[
			{"name":"DSH-Desktop-2.0.1-x64-Setup.exe","browser_download_url":"https://github.com/anywhere-labs/deepseek-harness-desktop/releases/download/v2.0.1/DSH-Desktop-2.0.1-x64-Setup.exe"},
			{"name":"DSH.Desktop-2.0.1-universal.dmg","browser_download_url":"https://github.com/anywhere-labs/deepseek-harness-desktop/releases/download/v2.0.1/DSH.Desktop-2.0.1-universal.dmg"}
		]}`))
	}))
	defer server.Close()

	options := Options{Platform: platform.For("macos", "arm64"), Downloader: releaseAPIClient{base: server.URL}}
	got, err := dshURL(context.Background(), options)
	if err != nil {
		t.Fatalf("official macOS dsh URL: %v", err)
	}
	if !strings.HasSuffix(got, "DSH.Desktop-2.0.1-universal.dmg") {
		t.Errorf("official macOS dsh URL = %q, want the universal .dmg", got)
	}
}

// releaseAPIClient answers the release API from a test server while leaving the
// host allowlist in approvedDownloadURL to judge the real asset URLs.
type releaseAPIClient struct{ base string }

func (c releaseAPIClient) Do(request *http.Request) (*http.Response, error) {
	if request.URL.String() == DSHDesktopReleaseAPI {
		redirected, err := http.NewRequestWithContext(request.Context(), request.Method, c.base, nil)
		if err != nil {
			return nil, err
		}
		return http.DefaultClient.Do(redirected)
	}
	return http.DefaultClient.Do(request)
}
