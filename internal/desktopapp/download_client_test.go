package desktopapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A CDN's last hop can be a presigned URL whose query holds the asset file name
// with a raw space in it. Left alone, net/http writes that space into the request
// line and the CDN answers 400.
func TestDownloadFollowsRedirectWithUnencodedSpaceInQuery(t *testing.T) {
	var servedTarget string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/downloads/mac":
			// Written straight to the header so the space survives, exactly as
			// the real redirect delivers it.
			w.Header()["Location"] = []string{"/cdn/object?filename=Some App-2.0.1-universal.dmg&auth_key=abc"}
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
