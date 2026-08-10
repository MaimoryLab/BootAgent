package binding

import (
	"context"
	"errors"
	"strings"
	"testing"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// releaseAssets is the asset list published for v0.5.0, in the order the
// GitHub API returns it (alphabetical). The .dmg preceding the .zip is what
// made the default matcher pick a disk image.
func releaseAssets() []github.ReleaseAsset {
	names := []string{
		"OneAgent-darwin-amd64.dmg",
		"OneAgent-darwin-amd64.zip",
		"OneAgent-darwin-arm64.dmg",
		"OneAgent-darwin-arm64.zip",
		"OneAgent-windows-amd64-installer.exe",
		"OneAgent-windows-amd64.zip",
		"OneAgent-windows-arm64-installer.exe",
		"OneAgent-windows-arm64.zip",
		"SHA256SUMS",
	}
	assets := make([]github.ReleaseAsset, len(names))
	for i, name := range names {
		assets[i] = github.ReleaseAsset{Name: name}
	}
	return assets
}

func TestExtractableAssetMatcherPicksTheArchiveForEveryPlatform(t *testing.T) {
	assets := releaseAssets()
	for _, test := range []struct {
		platform string
		arch     string
		want     string
	}{
		{"darwin", "arm64", "OneAgent-darwin-arm64.zip"},
		{"darwin", "amd64", "OneAgent-darwin-amd64.zip"},
		{"windows", "amd64", "OneAgent-windows-amd64.zip"},
		{"windows", "arm64", "OneAgent-windows-arm64.zip"},
	} {
		t.Run(test.platform+"/"+test.arch, func(t *testing.T) {
			request := updater.CheckRequest{CurrentVersion: "0.4.0", Platform: test.platform, Arch: test.arch}

			index := ExtractableAssetMatcher(request, assets)
			if index < 0 {
				t.Fatalf("no asset matched %s/%s", test.platform, test.arch)
			}
			if got := assets[index].Name; got != test.want {
				t.Fatalf("matched %q, want %q", got, test.want)
			}
		})
	}
}

// The bug this guards: the upstream matcher takes the first name containing
// both platform and arch, and ".dmg" sorts before ".zip".
func TestDefaultAssetMatcherPicksTheDiskImage(t *testing.T) {
	assets := releaseAssets()
	request := updater.CheckRequest{CurrentVersion: "0.4.0", Platform: "darwin", Arch: "arm64"}

	index := github.DefaultAssetMatcher(request, assets)
	if index < 0 {
		t.Fatal("upstream matcher found nothing; the asset list no longer reproduces the bug")
	}
	if got := assets[index].Name; got != "OneAgent-darwin-arm64.dmg" {
		t.Skipf("upstream matcher now picks %q; ExtractableAssetMatcher may be redundant", got)
	}
}

func TestExtractableAssetMatcherReportsNoCandidate(t *testing.T) {
	request := updater.CheckRequest{CurrentVersion: "0.4.0", Platform: "linux", Arch: "arm64"}
	if index := ExtractableAssetMatcher(request, releaseAssets()); index != -1 {
		t.Fatalf("index = %d, want -1 for a platform with no asset", index)
	}

	onlyContainers := []github.ReleaseAsset{
		{Name: "OneAgent-darwin-arm64.dmg"},
		{Name: "OneAgent-darwin-arm64.pkg"},
	}
	request = updater.CheckRequest{CurrentVersion: "0.4.0", Platform: "darwin", Arch: "arm64"}
	if index := ExtractableAssetMatcher(request, onlyContainers); index != -1 {
		t.Fatalf("index = %d, want -1 when nothing is extractable", index)
	}
}

// Sidecars and installers are the upstream matcher's job; filtering must not
// reorder assets in a way that reintroduces them.
func TestExtractableAssetMatcherKeepsUpstreamExclusions(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "SHA256SUMS"},
		{Name: "OneAgent-windows-amd64-installer.exe"},
		{Name: "OneAgent-windows-amd64.zip.sig"},
		{Name: "OneAgent-windows-amd64.zip"},
	}
	request := updater.CheckRequest{CurrentVersion: "0.4.0", Platform: "windows", Arch: "amd64"}

	index := ExtractableAssetMatcher(request, assets)
	if index < 0 || assets[index].Name != "OneAgent-windows-amd64.zip" {
		t.Fatalf("index = %d, want the .zip", index)
	}
}

func TestUpdateServiceRejectsAnUninstallableArtifact(t *testing.T) {
	for _, staged := range []string{
		"/tmp/wails-update-1/OneAgent-darwin-arm64.dmg",
		"/tmp/wails-update-1/OneAgent-darwin-arm64.zip",
		"/tmp/wails-update-1/OneAgent-windows-amd64-installer.msi",
		"/tmp/wails-update-1/OneAgent-linux-amd64.AppImage",
	} {
		t.Run(staged, func(t *testing.T) {
			service := NewUpdateService(&updateBackendFake{
				downloadAndInstall: func(ctx context.Context) error { return nil },
				downloadedPath:     func() string { return staged },
			})

			err := service.DownloadAndInstall(context.Background())
			got := oneerrors.As(err)
			if got.Code != oneerrors.UpdateNotInstallable || got.Status != 500 {
				t.Fatalf("error = %#v", got)
			}
			// Not retryable: retrying downloads the same asset again.
			if got.Retryable {
				t.Fatal("error is retryable; a repeat download stages the same artifact")
			}
			if !strings.Contains(got.Message, "not installable") {
				t.Fatalf("message = %q", got.Message)
			}
			// The private cause names the artifact; the public message does not.
			cause := errors.Unwrap(err)
			if cause == nil || !strings.Contains(cause.Error(), "OneAgent-") {
				t.Fatalf("cause = %v", cause)
			}
		})
	}
}

func TestUpdateServiceAcceptsAnInstalledBundle(t *testing.T) {
	for name, staged := range map[string]string{
		"macOS bundle": "/tmp/wails-update-1/OneAgent.app",
		"bare binary":  "/tmp/wails-update-1/OneAgent.exe",
		"unreported":   "",
	} {
		t.Run(name, func(t *testing.T) {
			service := NewUpdateService(&updateBackendFake{
				downloadAndInstall: func(ctx context.Context) error { return nil },
				downloadedPath:     func() string { return staged },
			})

			if err := service.DownloadAndInstall(context.Background()); err != nil {
				t.Fatalf("DownloadAndInstall() = %v", err)
			}
		})
	}
}
