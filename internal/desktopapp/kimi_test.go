package desktopapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// kimiCodeRunner replays scripted results and creates the extracted .app when
// it sees the unpacking ditto call, which the install then walks for.
type kimiCodeRunner struct {
	results []process.Result
	calls   [][]string
	started [][]string
	t       *testing.T
}

func (r *kimiCodeRunner) LookPath(string) (string, bool) { return "", false }

func (r *kimiCodeRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	if len(argv) >= 5 && argv[0] == "/usr/bin/ditto" && argv[1] == "-x" {
		if err := os.MkdirAll(filepath.Join(argv[4], kimiCodeDesktopAppName, "Contents"), 0o755); err != nil {
			r.t.Fatal(err)
		}
	}
	if len(r.results) == 0 {
		return process.Result{Args: argv, ExitCode: 0}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	result.Args = argv
	return result, nil
}

func (r *kimiCodeRunner) Start(argv []string, _ map[string]string) error {
	r.started = append(r.started, append([]string(nil), argv...))
	return nil
}

// kimiCodeMacFeed is shaped like the vendor's latest-mac.yml: relative URLs,
// both architectures, a zip and a dmg for each.
func kimiCodeMacFeed(armZipDigest string, armZipSize int64) []byte {
	return fmt.Appendf(nil, `version: 1.0.4
files:
  - url: binaries/1.0.4/KimiCode-1.0.4-mac-arm64.zip
    sha512: %s
    size: %d
  - url: binaries/1.0.4/KimiCode-1.0.4-mac-arm64.dmg
    sha512: ZG1n
    size: 3
  - url: binaries/1.0.4/KimiCode-1.0.4-mac-x64.zip
    sha512: eDY0
    size: 3
  - url: binaries/1.0.4/KimiCode-1.0.4-mac-x64.dmg
    sha512: ZG1n
    size: 3
path: binaries/1.0.4/KimiCode-1.0.4-mac-arm64.zip
`, armZipDigest, armZipSize)
}

func TestKimiCodeTargetAndFeedPerPlatform(t *testing.T) {
	tests := []struct {
		osID, arch, target, feed string
	}{
		{"macos", "arm64", "mac-arm64", KimiCodeDesktopFeedBase + "latest-mac.yml"},
		{"macos", "x86_64", "mac-x64", KimiCodeDesktopFeedBase + "latest-mac.yml"},
		{"windows", "x64", "win-x64", KimiCodeDesktopFeedBase + "latest.yml"},
		// The vendor ships no ARM Windows build; x64 is what such a machine runs.
		{"windows", "arm64", "win-x64", KimiCodeDesktopFeedBase + "latest.yml"},
	}
	for _, test := range tests {
		if got := kimiCodeTarget(test.osID, test.arch); got != test.target {
			t.Fatalf("kimiCodeTarget(%q, %q) = %q, want %q", test.osID, test.arch, got, test.target)
		}
		if got, err := kimiCodeFeedURL(test.osID, test.arch); err != nil || got != test.feed {
			t.Fatalf("kimiCodeFeedURL(%q, %q) = %q, %v", test.osID, test.arch, got, err)
		}
	}
	if _, err := kimiCodeFeedURL("linux", "x64"); err == nil {
		t.Fatal("kimiCodeFeedURL() accepted linux, which the vendor does not ship")
	}
	if status := Inspect(context.Background(), KimiCodeDesktopID, Options{Platform: platform.For("linux", "amd64")}); status.Supported {
		t.Fatalf("linux status = %#v, want unsupported", status)
	}
}

// The feed's URLs are relative. They resolve against the feed URL and must
// stay on the vendor's hosts; each architecture gets its own zip, never the
// dmg listed beside it.
func TestKimiCodeArtifactResolvesRelativeURLsPerArchitecture(t *testing.T) {
	feed := kimiCodeFeed{Files: []kimiCodeFeedFile{
		{URL: "binaries/1.0.4/KimiCode-1.0.4-mac-arm64.dmg", SHA512: "ZG1n"},
		{URL: "binaries/1.0.4/KimiCode-1.0.4-mac-arm64.zip", SHA512: "YXJt"},
		{URL: "binaries/1.0.4/KimiCode-1.0.4-mac-x64.zip", SHA512: "eDY0"},
	}}
	feedURL := KimiCodeDesktopFeedBase + "latest-mac.yml"
	arm, err := kimiCodeArtifact(feed, feedURL, "macos", "arm64")
	if err != nil || arm.URL != KimiCodeDesktopFeedBase+"binaries/1.0.4/KimiCode-1.0.4-mac-arm64.zip" || arm.SHA512 != "YXJt" {
		t.Fatalf("arm64 artifact = %#v, %v", arm, err)
	}
	intel, err := kimiCodeArtifact(feed, feedURL, "macos", "x64")
	if err != nil || !strings.HasSuffix(intel.URL, "-mac-x64.zip") {
		t.Fatalf("x64 artifact = %#v, %v", intel, err)
	}
	if _, err := kimiCodeArtifact(kimiCodeFeed{Files: feed.Files[:1]}, feedURL, "macos", "arm64"); err == nil {
		t.Fatal("kimiCodeArtifact() fell back to the dmg when no zip was listed")
	}
}

func TestKimiCodeArtifactRejectsOffHostAndMissingDigest(t *testing.T) {
	feedURL := KimiCodeDesktopFeedBase + "latest.yml"
	for _, url := range []string{"https://example.test/KimiCode-1.0.4-win-x64.exe", "//example.test/KimiCode-1.0.4-win-x64.exe", "http://code.kimi.com/KimiCode-1.0.4-win-x64.exe"} {
		feed := kimiCodeFeed{Files: []kimiCodeFeedFile{{URL: url, SHA512: "ZXhl"}}}
		if _, err := kimiCodeArtifact(feed, feedURL, "windows", "x64"); err == nil {
			t.Fatalf("kimiCodeArtifact() accepted %q", url)
		}
	}
	noDigest := kimiCodeFeed{Files: []kimiCodeFeedFile{{URL: "binaries/1.0.4/KimiCode-1.0.4-win-x64.exe"}}}
	if _, err := kimiCodeArtifact(noDigest, feedURL, "windows", "x64"); err == nil || !strings.Contains(err.Error(), "no digest") {
		t.Fatalf("kimiCodeArtifact() error = %v", err)
	}
}

func TestKimiCodeMacOSInstallVerifiesDigestBundleIDAndSignature(t *testing.T) {
	payload := []byte("Kimi Code macOS archive")
	feedURL := KimiCodeDesktopFeedBase + "latest-mac.yml"
	zipURL := KimiCodeDesktopFeedBase + "binaries/1.0.4/KimiCode-1.0.4-mac-arm64.zip"
	downloader := &routeDownloader{routes: map[string][]byte{
		feedURL: kimiCodeMacFeed(zcodeDigest(payload), int64(len(payload))),
		zipURL:  payload,
	}}
	applications := t.TempDir()
	runner := &kimiCodeRunner{t: t, results: []process.Result{
		{ExitCode: 0}, // ditto -x -k
		{ExitCode: 0, Stdout: "com.kimi.code.desktop\n"}, // plutil CFBundleIdentifier
		{ExitCode: 0, Stdout: "1.0.4\n"},                 // plutil CFBundleShortVersionString
		{ExitCode: 0},                                    // codesign --verify
		{ExitCode: 0, Stdout: "Identifier=com.kimi.code.desktop\nTeamIdentifier=2J9472RW75\nAuthority=Developer ID Application: Beijing Moonshot Technology Co., Ltd (2J9472RW75)\n"},
		{ExitCode: 0, Stdout: "source=Notarized Developer ID\n"}, // spctl
		{ExitCode: 0}, // ditto copy
	}}
	result, err := Install(context.Background(), KimiCodeDesktopID, Options{
		Home: t.TempDir(), Platform: platform.For("macos", "arm64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()}, ApplicationDirs: []string{applications},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "installed" || result.App.Path != filepath.Join(applications, kimiCodeDesktopAppName) || result.App.Version == nil || *result.App.Version != "1.0.4" {
		t.Fatalf("Install() = %#v", result)
	}
	if len(downloader.hits) != 2 || downloader.hits[0] != feedURL || downloader.hits[1] != zipURL {
		t.Fatalf("downloads = %#v", downloader.hits)
	}
}

// A different team must stop the install even though the digest and bundle
// identifier matched: the feed proves integrity, the signature proves origin.
func TestKimiCodeMacOSInstallRejectsAnotherTeam(t *testing.T) {
	payload := []byte("Kimi Code macOS archive")
	feedURL := KimiCodeDesktopFeedBase + "latest-mac.yml"
	zipURL := KimiCodeDesktopFeedBase + "binaries/1.0.4/KimiCode-1.0.4-mac-arm64.zip"
	downloader := &routeDownloader{routes: map[string][]byte{
		feedURL: kimiCodeMacFeed(zcodeDigest(payload), int64(len(payload))),
		zipURL:  payload,
	}}
	applications := t.TempDir()
	runner := &kimiCodeRunner{t: t, results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0, Stdout: "com.kimi.code.desktop\n"},
		{ExitCode: 0, Stdout: "1.0.4\n"},
		{ExitCode: 0},
		{ExitCode: 0, Stdout: "Identifier=com.kimi.code.desktop\nTeamIdentifier=ABCDE12345\nAuthority=Developer ID Application: Someone Else (ABCDE12345)\n"},
	}}
	_, err := Install(context.Background(), KimiCodeDesktopID, Options{
		Home: t.TempDir(), Platform: platform.For("macos", "arm64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()}, ApplicationDirs: []string{applications},
	})
	if err == nil || !strings.Contains(err.Error(), "TeamIdentifier=2J9472RW75") {
		t.Fatalf("Install() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(applications, kimiCodeDesktopAppName)); !os.IsNotExist(statErr) {
		t.Fatalf("an app from another team was copied into Applications: %v", statErr)
	}
}

func TestKimiCodeMacOSInstallStopsBeforeExtractingOnDigestMismatch(t *testing.T) {
	feedURL := KimiCodeDesktopFeedBase + "latest-mac.yml"
	zipURL := KimiCodeDesktopFeedBase + "binaries/1.0.4/KimiCode-1.0.4-mac-arm64.zip"
	downloader := &routeDownloader{routes: map[string][]byte{
		feedURL: kimiCodeMacFeed(zcodeDigest([]byte("expected")), 8),
		zipURL:  []byte("replaced"),
	}}
	runner := &kimiCodeRunner{t: t}
	_, err := Install(context.Background(), KimiCodeDesktopID, Options{
		Home: t.TempDir(), Platform: platform.For("macos", "arm64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()}, ApplicationDirs: []string{t.TempDir()},
	})
	if err == nil || !strings.Contains(err.Error(), "SHA-512") {
		t.Fatalf("Install() error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("install ran commands after a digest mismatch: %#v", runner.calls)
	}
}

func kimiCodeWindowsFeed(digest string, size int) []byte {
	return fmt.Appendf(nil, "version: 1.0.4\nfiles:\n  - url: binaries/1.0.4/KimiCode-1.0.4-win-x64.exe\n    sha512: %s\n    size: %d\npath: binaries/1.0.4/KimiCode-1.0.4-win-x64.exe\n", digest, size)
}

func TestKimiCodeWindowsInstallVerifiesAuthenticodeBeforeStarting(t *testing.T) {
	payload := []byte("Kimi Code Windows installer")
	feedURL := KimiCodeDesktopFeedBase + "latest.yml"
	exeURL := KimiCodeDesktopFeedBase + "binaries/1.0.4/KimiCode-1.0.4-win-x64.exe"
	downloader := &routeDownloader{routes: map[string][]byte{feedURL: kimiCodeWindowsFeed(zcodeDigest(payload), len(payload)), exeURL: payload}}
	runner := &kimiCodeRunner{t: t, results: []process.Result{
		{ExitCode: 0, Stdout: `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"NOVASCENT PRIVATE LIMITED","Subject":"CN=NOVASCENT PRIVATE LIMITED, O=NOVASCENT PRIVATE LIMITED, L=Singapore, C=SG","Issuer":"CN=Microsoft ID Verified CS EOC CA 04, O=Microsoft Corporation, C=US"}`},
	}}
	result, err := Install(context.Background(), KimiCodeDesktopID, Options{
		Home: t.TempDir(), Platform: platform.For("windows", "x64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "installer-started" || len(runner.started) != 1 || result.App.Version == nil || *result.App.Version != "1.0.4" {
		t.Fatalf("Install() = %#v started=%#v", result, runner.started)
	}
	// The install leaves the installer on disk for Windows to run.
	if err := os.Remove(runner.started[0][0]); err != nil {
		t.Fatal(err)
	}
}

// Moonshot's own name is not what signs the Windows build, so accepting it
// would be accepting a signer the vendor does not use; only the pinned
// publisher passes.
func TestKimiCodeWindowsInstallRejectsAnyOtherPublisher(t *testing.T) {
	payload := []byte("Kimi Code Windows installer")
	feedURL := KimiCodeDesktopFeedBase + "latest.yml"
	exeURL := KimiCodeDesktopFeedBase + "binaries/1.0.4/KimiCode-1.0.4-win-x64.exe"
	for _, publisher := range []string{"Beijing Moonshot Technology Co., Ltd", "Someone Else"} {
		downloader := &routeDownloader{routes: map[string][]byte{feedURL: kimiCodeWindowsFeed(zcodeDigest(payload), len(payload)), exeURL: payload}}
		runner := &kimiCodeRunner{t: t, results: []process.Result{
			{ExitCode: 0, Stdout: fmt.Sprintf(`{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":%q,"Subject":"CN=%s, O=%s","Issuer":"CN=Trusted CA"}`, publisher, publisher, publisher)},
		}}
		_, err := Install(context.Background(), KimiCodeDesktopID, Options{
			Home: t.TempDir(), Platform: platform.For("windows", "x64"), Runner: runner, Downloader: downloader,
			SearchRoots: []string{t.TempDir()},
		})
		if err == nil || !strings.Contains(err.Error(), "not approved") {
			t.Fatalf("publisher %q: Install() error = %v", publisher, err)
		}
		if len(runner.started) != 0 {
			t.Fatalf("publisher %q: installer started: %#v", publisher, runner.started)
		}
	}
}

func TestKimiCodeInspectRequiresTheBundleIdentifier(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, kimiCodeDesktopAppName, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	options := func(bundleID string) Options {
		return Options{Platform: platform.For("macos", "arm64"), SearchRoots: []string{root}, Runner: &kimiCodeRunner{t: t, results: []process.Result{
			{ExitCode: 0, Stdout: bundleID + "\n"},
			{ExitCode: 0, Stdout: "1.0.4\n"},
		}}}
	}
	if status := Inspect(context.Background(), KimiCodeDesktopID, options("com.example.renamed")); status.Installed {
		t.Fatalf("a renamed app passed as Kimi Code: %#v", status)
	}
	status := Inspect(context.Background(), KimiCodeDesktopID, options(KimiCodeDesktopBundleID))
	if !status.Installed || status.Version == nil || *status.Version != "1.0.4" || status.Path != filepath.Join(root, kimiCodeDesktopAppName) {
		t.Fatalf("Inspect() = %#v", status)
	}
}

func TestKimiCodeWindowsInspectAndOpen(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, kimiCodeDesktopExeName)
	if err := os.WriteFile(exe, []byte("exe"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &kimiCodeRunner{t: t}
	options := Options{Platform: platform.For("windows", "x64"), SearchRoots: []string{root}, Runner: runner}
	if status := Inspect(context.Background(), KimiCodeDesktopID, options); !status.Installed || status.Path != exe {
		t.Fatalf("Inspect() = %#v", status)
	}
	if err := Open(context.Background(), KimiCodeDesktopID, options); err != nil {
		t.Fatal(err)
	}
	if len(runner.started) != 1 || runner.started[0][0] != exe {
		t.Fatalf("started = %#v", runner.started)
	}
}
