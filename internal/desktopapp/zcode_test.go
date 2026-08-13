package desktopapp

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// zcodeFeedYAML builds a manifest shaped like the vendor's, which lists the zip
// first and the dmg second.
func zcodeFeedYAML(version, zipURL, zipDigest string, zipSize int64, dmgURL, dmgDigest string, dmgSize int64) []byte {
	return fmt.Appendf(nil, `version: %s
files:
  - url: %s
    sha512: %s
    size: %d
  - url: %s
    sha512: %s
    size: %d
path: %s
sha512: %s
`, version, zipURL, zipDigest, zipSize, dmgURL, dmgDigest, dmgSize, zipURL, zipDigest)
}

func zcodeDigest(payload []byte) string {
	sum := sha512.Sum512(payload)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// zcodeRunner replays scripted results like scriptedRunner, but also creates the
// extracted .app when it sees the ditto call. The install walks the extraction
// directory for ZCode.app, so a runner that only records the command would leave
// nothing to find.
type zcodeRunner struct {
	results []process.Result
	calls   [][]string
	started [][]string
	t       *testing.T
}

func (r *zcodeRunner) LookPath(string) (string, bool) { return "", false }

func (r *zcodeRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	if len(argv) >= 5 && argv[0] == "/usr/bin/ditto" && argv[1] == "-x" {
		if err := os.MkdirAll(filepath.Join(argv[4], "ZCode.app", "Contents"), 0o755); err != nil {
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

func (r *zcodeRunner) Start(argv []string, _ map[string]string) error {
	r.started = append(r.started, append([]string(nil), argv...))
	return nil
}

func TestZCodeFeedURLPerPlatformAndArch(t *testing.T) {
	tests := []struct {
		osID, arch, want string
	}{
		{"macos", "arm64", ZCodeFeedBase + "mac/arm64/latest-mac.yml"},
		{"macos", "x86_64", ZCodeFeedBase + "mac/x64/latest-mac.yml"},
		{"windows", "x64", ZCodeFeedBase + "win/x64/latest.yml"},
		{"windows", "aarch64", ZCodeFeedBase + "win/arm64/latest.yml"},
	}
	for _, test := range tests {
		got, err := zcodeFeedURL(test.osID, test.arch)
		if err != nil || got != test.want {
			t.Fatalf("zcodeFeedURL(%q, %q) = %q, %v; want %q", test.osID, test.arch, got, err, test.want)
		}
	}
	if _, err := zcodeFeedURL("linux", "x64"); err == nil {
		t.Fatal("zcodeFeedURL() accepted linux, which BootAgent cannot verify")
	}
}

// The dmg's published digest describes the archive before the vendor staples its
// notarization ticket, so the served bytes never match it. Picking the dmg would
// therefore either fail every install or force the digest check to be skipped;
// the zip is the artifact electron-updater itself consumes.
func TestZCodeArtifactPrefersTheZipOverTheStaleDMG(t *testing.T) {
	feed := zcodeFeed{Files: []zcodeFeedFile{
		{URL: "https://" + ZCodeUpdateHost + "/a/ZCode.dmg", SHA512: "ZG1n", Size: 2},
		{URL: "https://" + ZCodeUpdateHost + "/a/ZCode.zip", SHA512: "emlw", Size: 1},
	}}
	artifact, err := zcodeArtifact(feed, "macos")
	if err != nil || !strings.HasSuffix(artifact.URL, ".zip") {
		t.Fatalf("zcodeArtifact() = %#v, %v", artifact, err)
	}
	if _, err := zcodeArtifact(zcodeFeed{Files: feed.Files[:1]}, "macos"); err == nil {
		t.Fatal("zcodeArtifact() fell back to the dmg when no zip was listed")
	}
}

func TestZCodeArtifactRejectsUnapprovedHostAndMissingDigest(t *testing.T) {
	offHost := zcodeFeed{Files: []zcodeFeedFile{{URL: "https://example.test/ZCode.zip", SHA512: "emlw", Size: 1}}}
	if _, err := zcodeArtifact(offHost, "macos"); err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("zcodeArtifact() error = %v", err)
	}
	noDigest := zcodeFeed{Files: []zcodeFeedFile{{URL: "https://" + ZCodeUpdateHost + "/a/ZCode.zip", Size: 1}}}
	if _, err := zcodeArtifact(noDigest, "macos"); err == nil || !strings.Contains(err.Error(), "no digest") {
		t.Fatalf("zcodeArtifact() error = %v", err)
	}
}

func TestVerifyZCodeDigestRejectsTamperedAndTruncatedPackages(t *testing.T) {
	payload := []byte("ZCode package bytes")
	path := filepath.Join(t.TempDir(), "ZCode.zip")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	good := zcodeFeedFile{SHA512: zcodeDigest(payload), Size: int64(len(payload))}
	if err := verifyZCodeDigest(path, good); err != nil {
		t.Fatalf("verifyZCodeDigest() on matching bytes = %v", err)
	}
	// A digest for different content: the archive was replaced in transit.
	tampered := zcodeFeedFile{SHA512: zcodeDigest([]byte("other")), Size: int64(len(payload))}
	if err := verifyZCodeDigest(path, tampered); err == nil || !strings.Contains(err.Error(), "SHA-512") {
		t.Fatalf("verifyZCodeDigest() on tampered bytes = %v", err)
	}
	// Size is reported on its own so a short transfer does not surface as an
	// opaque digest mismatch.
	short := zcodeFeedFile{SHA512: zcodeDigest(payload), Size: int64(len(payload)) + 10}
	if err := verifyZCodeDigest(path, short); err == nil || !strings.Contains(err.Error(), "bytes, expected") {
		t.Fatalf("verifyZCodeDigest() on truncated bytes = %v", err)
	}
	if err := verifyZCodeDigest(path, zcodeFeedFile{SHA512: base64.StdEncoding.EncodeToString([]byte("short"))}); err == nil {
		t.Fatal("verifyZCodeDigest() accepted a digest that is not SHA-512 sized")
	}
}

func TestZCodeMacOSInstallVerifiesDigestBundleIDAndSignature(t *testing.T) {
	payload := []byte("ZCode macOS archive")
	feedURL := ZCodeFeedBase + "mac/arm64/latest-mac.yml"
	zipURL := "https://" + ZCodeUpdateHost + "/zcode/electron/releases/3.7.5/macos-arm64/ZCode-3.7.5-mac-arm64.zip"
	dmgURL := "https://" + ZCodeUpdateHost + "/zcode/electron/releases/3.7.5/macos-arm64/ZCode-3.7.5-mac-arm64.dmg"
	feed := zcodeFeedYAML("3.7.5", zipURL, zcodeDigest(payload), int64(len(payload)), dmgURL, zcodeDigest([]byte("dmg")), 99)
	downloader := &routeDownloader{routes: map[string][]byte{feedURL: feed, zipURL: payload}}
	home := t.TempDir()
	applications := t.TempDir()
	runner := &zcodeRunner{t: t, results: []process.Result{
		{ExitCode: 0},                            // ditto -x -k
		{ExitCode: 0, Stdout: "dev.zcode.app\n"}, // plutil CFBundleIdentifier
		{ExitCode: 0, Stdout: "3.7.5\n"},         // plutil CFBundleShortVersionString
		{ExitCode: 0},                            // codesign --verify
		{ExitCode: 0, Stdout: "Identifier=dev.zcode.app\nTeamIdentifier=8A5X4JJ39T\nAuthority=Developer ID Application: Beijing Knowledge Atlas Technology Joint Stock Company Limited (8A5X4JJ39T)\n"},
		{ExitCode: 0, Stdout: "source=Notarized Developer ID\n"}, // spctl
		{ExitCode: 0}, // ditto copy
	}}
	result, err := Install(context.Background(), ZCodeID, Options{
		Home: home, Platform: platform.For("macos", "arm64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()}, ApplicationDirs: []string{applications},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "installed" || result.App.Path != applications+"/ZCode.app" {
		t.Fatalf("Install() = %#v", result)
	}
	// The dmg must not be requested: the feed is read, then only the zip.
	if len(downloader.hits) != 2 || downloader.hits[0] != feedURL || downloader.hits[1] != zipURL {
		t.Fatalf("downloads = %#v", downloader.hits)
	}
}

// A digest mismatch has to stop the install before the archive is unpacked or
// copied, otherwise the check is decorative.
func TestZCodeMacOSInstallStopsBeforeExtractingOnDigestMismatch(t *testing.T) {
	feedURL := ZCodeFeedBase + "mac/arm64/latest-mac.yml"
	zipURL := "https://" + ZCodeUpdateHost + "/a/ZCode.zip"
	feed := zcodeFeedYAML("3.7.5", zipURL, zcodeDigest([]byte("expected")), 8, zipURL+".dmg", "ZG1n", 3)
	downloader := &routeDownloader{routes: map[string][]byte{feedURL: feed, zipURL: []byte("replaced")}}
	runner := &zcodeRunner{t: t}
	_, err := Install(context.Background(), ZCodeID, Options{
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

func TestZCodeWindowsInstallVerifiesAuthenticodeBeforeStarting(t *testing.T) {
	payload := []byte("ZCode Windows installer")
	feedURL := ZCodeFeedBase + "win/x64/latest.yml"
	exeURL := "https://" + ZCodeUpdateHost + "/zcode/electron/releases/3.7.5/windows-x64/ZCode-3.7.5-win-x64.exe"
	feed := fmt.Appendf(nil, "version: 3.7.5\nfiles:\n  - url: %s\n    sha512: %s\n    size: %d\npath: %s\n",
		exeURL, zcodeDigest(payload), len(payload), exeURL)
	downloader := &routeDownloader{routes: map[string][]byte{feedURL: feed, exeURL: payload}}
	runner := &zcodeRunner{t: t, results: []process.Result{
		{ExitCode: 0, Stdout: `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"北京智谱华章科技股份有限公司","Organization":"北京智谱华章科技股份有限公司","Subject":"CN=北京智谱华章科技股份有限公司, O=北京智谱华章科技股份有限公司","Issuer":"CN=DigiCert Trusted G4 Code Signing RSA4096 SHA384 2021 CA1"}`},
	}}
	result, err := Install(context.Background(), ZCodeID, Options{
		Home: t.TempDir(), Platform: platform.For("windows", "x64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "installer-started" || len(runner.started) != 1 {
		t.Fatalf("Install() = %#v started=%#v", result, runner.started)
	}
	// The install deliberately leaves the installer on disk for Windows to run.
	if err := os.Remove(runner.started[0][0]); err != nil {
		t.Fatal(err)
	}
}

// An unexpected signer must block the install even though the digest matched:
// the feed proves integrity, the signature proves origin.
func TestZCodeWindowsInstallRejectsUnexpectedPublisher(t *testing.T) {
	payload := []byte("ZCode Windows installer")
	feedURL := ZCodeFeedBase + "win/x64/latest.yml"
	exeURL := "https://" + ZCodeUpdateHost + "/a/ZCode.exe"
	feed := fmt.Appendf(nil, "version: 3.7.5\nfiles:\n  - url: %s\n    sha512: %s\n    size: %d\n", exeURL, zcodeDigest(payload), len(payload))
	downloader := &routeDownloader{routes: map[string][]byte{feedURL: feed, exeURL: payload}}
	runner := &zcodeRunner{t: t, results: []process.Result{
		{ExitCode: 0, Stdout: `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Someone Else","Organization":"Someone Else","Subject":"CN=Someone Else, O=Someone Else","Issuer":"CN=Trusted CA"}`},
	}}
	_, err := Install(context.Background(), ZCodeID, Options{
		Home: t.TempDir(), Platform: platform.For("windows", "x64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()},
	})
	if err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("Install() error = %v", err)
	}
	if len(runner.started) != 0 {
		t.Fatalf("installer started despite an unapproved publisher: %#v", runner.started)
	}
}
