package desktopapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
)

// The two editions install side by side, so each must ignore the other. Their
// version strings are near-identical (both 5.3.11.x), which is why the bundle
// identifier is the discriminator rather than the version or the app name alone.
func TestWorkBuddyEditionsDoNotDetectEachOther(t *testing.T) {
	tests := []struct {
		agentID  string
		appName  string
		bundleID string
		otherID  string
	}{
		{WorkBuddyID, "WorkBuddy.app", WorkBuddyBundleID, WorkBuddyIntlID},
		{WorkBuddyIntlID, "WorkBuddy AI.app", WorkBuddyIntlBundleID, WorkBuddyID},
	}
	for _, test := range tests {
		root := t.TempDir()
		appPath := makeBundle(t, root, test.appName)
		if err := os.WriteFile(filepath.Join(appPath, "Contents", "Info.plist"), []byte("plist"), 0o600); err != nil {
			t.Fatal(err)
		}
		runner := &probeRunner{macValues: map[string]string{
			"CFBundleIdentifier": test.bundleID, "CFBundleShortVersionString": "5.3.11",
		}}
		status := Inspect(context.Background(), test.agentID, Options{
			Platform: platform.For("macos", "arm64"), SearchRoots: []string{root}, Runner: runner,
		})
		if !status.Installed || status.ID != test.agentID || status.Path != appPath {
			t.Fatalf("Inspect(%q) = %#v", test.agentID, status)
		}

		// The same directory must not satisfy the other edition. SearchRoots points
		// at a root here, so the app-name check runs first; the bundle ID check is
		// covered by the pinned-path case below.
		other := Inspect(context.Background(), test.otherID, Options{
			Platform: platform.For("macos", "arm64"), SearchRoots: []string{root},
			Runner: &probeRunner{macValues: map[string]string{"CFBundleIdentifier": test.bundleID}},
		})
		if other.Installed {
			t.Fatalf("Inspect(%q) matched the %s bundle: %#v", test.otherID, test.agentID, other)
		}
	}
}

// A SearchRoots entry pointing straight at an .app skips the filename check, so
// the bundle identifier has to be what rejects the wrong edition. Without it a
// user with only the international build installed would see the Chinese Agent
// reported as installed, and BootAgent would write to the wrong config file.
func TestWorkBuddyPinnedPathStillChecksBundleIdentifier(t *testing.T) {
	root := t.TempDir()
	appPath := makeBundle(t, root, "WorkBuddy AI.app")
	if err := os.WriteFile(filepath.Join(appPath, "Contents", "Info.plist"), []byte("plist"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := Inspect(context.Background(), WorkBuddyID, Options{
		Platform: platform.For("macos", "arm64"), SearchRoots: []string{appPath},
		Runner: &probeRunner{macValues: map[string]string{"CFBundleIdentifier": WorkBuddyIntlBundleID}},
	})
	if status.Installed {
		t.Fatalf("Chinese WorkBuddy accepted the international bundle: %#v", status)
	}
}

// Each edition has its own update backend and its own download host. Crossing
// them must fail closed: the Chinese host is not approved for the international
// feed's URLs and the reverse.
func TestWorkBuddyEditionsUseTheirOwnEndpointsAndRejectTheOtherHost(t *testing.T) {
	tests := []struct {
		edition    workBuddyEdition
		wantFeed   string
		crossHost  string
		wantSource string
	}{
		{workBuddyCN, WorkBuddyUpdateEndpoint + "?platform=workbuddy-darwin-arm64", WorkBuddyIntlDownloadHost, WorkBuddyDownloadHost},
		{workBuddyIntl, WorkBuddyIntlUpdateEndpoint + "?platform=workbuddy-darwin-arm64", WorkBuddyDownloadHost, WorkBuddyIntlDownloadHost},
	}
	for _, test := range tests {
		good := "https://" + test.wantSource + "/workbuddy/WorkBuddy.zip"
		downloader := &routeDownloader{routes: map[string][]byte{
			test.wantFeed: []byte(`{"version":"5.3.11","url":"` + good + `"}`),
		}}
		update, err := fetchWorkBuddyUpdate(context.Background(), test.edition, Options{
			Platform: platform.For("macos", "arm64"), Downloader: downloader,
		})
		if err != nil || update.URL != good {
			t.Fatalf("fetchWorkBuddyUpdate(%s) = %#v, %v", test.edition.id, update, err)
		}
		if len(downloader.hits) != 1 || downloader.hits[0] != test.wantFeed {
			t.Fatalf("%s requested %#v; want %q", test.edition.id, downloader.hits, test.wantFeed)
		}

		// The other edition's download host must not be accepted.
		crossed := &routeDownloader{routes: map[string][]byte{
			test.wantFeed: []byte(`{"version":"5.3.11","url":"https://` + test.crossHost + `/workbuddy/WorkBuddy.zip"}`),
		}}
		if _, err := fetchWorkBuddyUpdate(context.Background(), test.edition, Options{
			Platform: platform.For("macos", "arm64"), Downloader: crossed,
		}); err == nil || !strings.Contains(err.Error(), "not approved") {
			t.Fatalf("fetchWorkBuddyUpdate(%s) accepted host %q: %v", test.edition.id, test.crossHost, err)
		}
	}
}

// The config path is the one fact the vendor's own English documentation gets
// wrong: it says ~/.codebuddy, but the shipped build resolves the directory from
// config.customUserDataDir in its cli/product.json, which is ".workbuddy-ai".
// Writing to .codebuddy would be a silent no-op, so this pins the real value.
func TestWorkBuddyEditionConfigPathsAreDistinctAndNotCodebuddy(t *testing.T) {
	cn, ok := DefinitionFor(WorkBuddyID)
	if !ok {
		t.Fatal("WorkBuddy is not registered")
	}
	intl, ok := DefinitionFor(WorkBuddyIntlID)
	if !ok {
		t.Fatal("WorkBuddy International is not registered")
	}
	if cn.ConfigPath != ".workbuddy/models.json" {
		t.Fatalf("Chinese config path = %q", cn.ConfigPath)
	}
	if intl.ConfigPath != ".workbuddy-ai/models.json" {
		t.Fatalf("international config path = %q", intl.ConfigPath)
	}
	if strings.Contains(intl.ConfigPath, "codebuddy") {
		t.Fatalf("international config path uses the documented but unread .codebuddy directory: %q", intl.ConfigPath)
	}
	// Both editions read the same schema, so they share one writer.
	if cn.ConfigAdapter != intl.ConfigAdapter {
		t.Fatalf("adapters diverged: %q vs %q", cn.ConfigAdapter, intl.ConfigAdapter)
	}
	// The editions have to be labelled, since the UI shows them side by side.
	if cn.Edition != EditionChina || intl.Edition != EditionInternational {
		t.Fatalf("editions = %q, %q", cn.Edition, intl.Edition)
	}
	// Separate profile bindings: pointing both at one Agent ID would make
	// configuring one silently rebind the other.
	if cn.ProfileAgentID == intl.ProfileAgentID {
		t.Fatalf("both editions bind profiles as %q", cn.ProfileAgentID)
	}
}
