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

const (
	dshMacFeedURL = DSHDesktopFeedBase + "mac-arm64/nightly-mac.yml"
	dshWinFeedURL = DSHDesktopFeedBase + "win-x64/nightly.yml"
	dshMacZipURL  = "https://" + DSHDesktopDownloadHost + "/dsh-desk/bin/mac-arm64/deepseek-harness-0.1.7-rc.2-mac-arm64.zip"
	dshWinExeURL  = "https://" + DSHDesktopDownloadHost + "/dsh-desk/bin/win-x64/deepseek-harness-0.1.7-rc.2-win-x64.exe"

	// What codesign -dv reports for the shipped release, captured on this machine.
	dshCodesignIdentity = "Identifier=com.deepseek.dsh\nTeamIdentifier=NAN929V4UM\nAuthority=Developer ID Application: Hangzhou DeepSeek Artificial Intelligence Co., Ltd (NAN929V4UM)\nAuthority=Developer ID Certification Authority\nAuthority=Apple Root CA\n"
)

// dshFeedYAML mirrors the vendor's electron-updater manifest: one file entry
// with its digest, then the same file restated at the top level.
func dshFeedYAML(version, fileURL, digest string, size int64) []byte {
	return fmt.Appendf(nil, `version: %s
files:
  - url: >-
      %s
    sha512: >-
      %s
    size: %d
path: >-
  %s
sha512: >-
  %s
releaseDate: '2026-09-24T14:10:00.562Z'
`, version, fileURL, digest, size, fileURL, digest)
}

func dshDigest(payload []byte) string {
	sum := sha512.Sum512(payload)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// dshRunner replays scripted results and materializes the extracted .app on the
// ditto -x call, since the install walks the extraction directory for it.
type dshRunner struct {
	results []process.Result
	calls   [][]string
	started [][]string
	t       *testing.T
}

func (r *dshRunner) LookPath(string) (string, bool) { return "", false }

func (r *dshRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	if len(argv) >= 5 && argv[0] == "/usr/bin/ditto" && argv[1] == "-x" {
		if err := os.MkdirAll(filepath.Join(argv[4], dshDesktopAppName, "Contents"), 0o755); err != nil {
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

func (r *dshRunner) Start(argv []string, _ map[string]string) error {
	r.started = append(r.started, append([]string(nil), argv...))
	return nil
}

// The vendor publishes two targets. Intel macOS and Windows on ARM have no
// package, and the feed for them does not exist, so the lookup has to refuse
// rather than request a 404.
func TestDSHFeedURLCoversOnlyThePublishedTargets(t *testing.T) {
	for _, test := range []struct{ osID, arch, want string }{
		{"macos", "arm64", dshMacFeedURL},
		{"macos", "aarch64", dshMacFeedURL},
		{"windows", "x64", dshWinFeedURL},
		{"windows", "amd64", dshWinFeedURL},
	} {
		got, err := dshFeedURL(test.osID, test.arch)
		if err != nil || got != test.want {
			t.Errorf("dshFeedURL(%q, %q) = %q, %v; want %q", test.osID, test.arch, got, err, test.want)
		}
	}
	for _, test := range []struct{ osID, arch string }{
		{"macos", "amd64"}, {"macos", "x86_64"}, {"windows", "arm64"}, {"linux", "amd64"},
	} {
		if got, err := dshFeedURL(test.osID, test.arch); err == nil {
			t.Errorf("dshFeedURL(%q, %q) = %q, want an error", test.osID, test.arch, got)
		}
	}
}

func TestDSHArtifactRequiresTheDigestedFileFromTheVendorHost(t *testing.T) {
	feed := dshFeed{Files: []dshFeedFile{{URL: dshMacZipURL, SHA512: "emlw", Size: 1}}}
	artifact, err := dshArtifact(feed, "macos")
	if err != nil || artifact.URL != dshMacZipURL {
		t.Fatalf("dshArtifact() = %#v, %v", artifact, err)
	}
	// The feed's macOS entry is the zip; a manifest that only listed the exe
	// (or nothing) has no macOS package.
	if _, err := dshArtifact(dshFeed{Files: []dshFeedFile{{URL: dshWinExeURL, SHA512: "ZXhl"}}}, "macos"); err == nil || !strings.Contains(err.Error(), "no .zip") {
		t.Fatalf("dshArtifact() on a Windows-only feed = %v", err)
	}
	offHost := dshFeed{Files: []dshFeedFile{{URL: "https://github.com/anywhere-labs/x/releases/download/v2/DSH.Desktop.zip", SHA512: "emlw"}}}
	if _, err := dshArtifact(offHost, "macos"); err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("dshArtifact() accepted an off-host URL: %v", err)
	}
	noDigest := dshFeed{Files: []dshFeedFile{{URL: dshMacZipURL, Size: 1}}}
	if _, err := dshArtifact(noDigest, "macos"); err == nil || !strings.Contains(err.Error(), "no digest") {
		t.Fatalf("dshArtifact() accepted a file with no digest: %v", err)
	}
}

func TestVerifyDSHDigestRejectsTamperedAndTruncatedPackages(t *testing.T) {
	payload := []byte("DeepSeek Harness package bytes")
	path := filepath.Join(t.TempDir(), "DeepSeek Harness.zip")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDSHDigest(path, dshFeedFile{SHA512: dshDigest(payload), Size: int64(len(payload))}); err != nil {
		t.Fatalf("verifyDSHDigest() on matching bytes = %v", err)
	}
	if err := verifyDSHDigest(path, dshFeedFile{SHA512: dshDigest([]byte("other")), Size: int64(len(payload))}); err == nil || !strings.Contains(err.Error(), "SHA-512") {
		t.Fatalf("verifyDSHDigest() on tampered bytes = %v", err)
	}
	if err := verifyDSHDigest(path, dshFeedFile{SHA512: dshDigest(payload), Size: int64(len(payload)) + 10}); err == nil || !strings.Contains(err.Error(), "bytes, expected") {
		t.Fatalf("verifyDSHDigest() on truncated bytes = %v", err)
	}
}

// The full macOS path: feed, digest, bundle identifier, pinned Developer ID team,
// notarization, copy. Only the zip is fetched; the dmg the vendor publishes
// beside it carries no digest and is never requested.
func TestDSHMacOSInstallVerifiesDigestBundleIDAndSignature(t *testing.T) {
	payload := []byte("DeepSeek Harness macOS archive")
	feed := dshFeedYAML("0.1.7-rc.2", dshMacZipURL, dshDigest(payload), int64(len(payload)))
	downloader := &routeDownloader{routes: map[string][]byte{dshMacFeedURL: feed, dshMacZipURL: payload}}
	applications := t.TempDir()
	runner := &dshRunner{t: t, results: []process.Result{
		{ExitCode: 0}, // ditto -x -k
		{ExitCode: 0, Stdout: DSHDesktopBundleID + "\n"},         // plutil CFBundleIdentifier
		{ExitCode: 0, Stdout: "0.1.7-rc.2\n"},                    // plutil CFBundleShortVersionString
		{ExitCode: 0},                                            // codesign --verify
		{ExitCode: 0, Stdout: dshCodesignIdentity},               // codesign -dv
		{ExitCode: 0, Stdout: "source=Notarized Developer ID\n"}, // spctl
		{ExitCode: 0}, // ditto copy
	}}
	result, err := Install(context.Background(), DSHDesktopID, Options{
		Home: t.TempDir(), Platform: platform.For("macos", "arm64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()}, ApplicationDirs: []string{applications},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "installed" || result.App.Path != filepath.Join(applications, dshDesktopAppName) || result.App.Version == nil || *result.App.Version != "0.1.7-rc.2" {
		t.Fatalf("Install() = %#v", result)
	}
	if len(downloader.hits) != 2 || downloader.hits[0] != dshMacFeedURL || downloader.hits[1] != dshMacZipURL {
		t.Fatalf("downloads = %#v", downloader.hits)
	}
	// The feed is the rolling pointer, so it must be asked for fresh.
	var sawSpctl bool
	for _, call := range runner.calls {
		if call[0] == "/usr/sbin/spctl" {
			sawSpctl = true
		}
	}
	if !sawSpctl {
		t.Fatalf("notarization was not assessed: %#v", runner.calls)
	}
}

// A validly signed bundle from another team is exactly what the old
// codesign-only check let through.
func TestDSHMacOSInstallRejectsAnotherTeamsSignature(t *testing.T) {
	payload := []byte("DeepSeek Harness macOS archive")
	feed := dshFeedYAML("0.1.7-rc.2", dshMacZipURL, dshDigest(payload), int64(len(payload)))
	downloader := &routeDownloader{routes: map[string][]byte{dshMacFeedURL: feed, dshMacZipURL: payload}}
	runner := &dshRunner{t: t, results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0, Stdout: DSHDesktopBundleID + "\n"},
		{ExitCode: 0, Stdout: "0.1.7-rc.2\n"},
		{ExitCode: 0},
		{ExitCode: 0, Stdout: "Identifier=com.deepseek.dsh\nTeamIdentifier=2DC432GLL2\nAuthority=Developer ID Application: Someone Else (2DC432GLL2)\n"},
	}}
	_, err := Install(context.Background(), DSHDesktopID, Options{
		Home: t.TempDir(), Platform: platform.For("macos", "arm64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()}, ApplicationDirs: []string{t.TempDir()},
	})
	if err == nil || !strings.Contains(err.Error(), "TeamIdentifier="+DSHDesktopTeamID) {
		t.Fatalf("Install() error = %v", err)
	}
	for _, call := range runner.calls {
		if call[0] == "/usr/bin/ditto" && call[1] != "-x" {
			t.Fatalf("app was copied despite a foreign signature: %#v", runner.calls)
		}
	}
}

func TestDSHMacOSInstallStopsBeforeExtractingOnDigestMismatch(t *testing.T) {
	feed := dshFeedYAML("0.1.7-rc.2", dshMacZipURL, dshDigest([]byte("expected")), 8)
	downloader := &routeDownloader{routes: map[string][]byte{dshMacFeedURL: feed, dshMacZipURL: []byte("replaced")}}
	runner := &dshRunner{t: t}
	_, err := Install(context.Background(), DSHDesktopID, Options{
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

func TestDSHWindowsInstallVerifiesAuthenticodeBeforeStarting(t *testing.T) {
	payload := []byte("DeepSeek Harness Windows installer")
	feed := dshFeedYAML("0.1.7-rc.2", dshWinExeURL, dshDigest(payload), int64(len(payload)))
	downloader := &routeDownloader{routes: map[string][]byte{dshWinFeedURL: feed, dshWinExeURL: payload}}
	runner := &dshRunner{t: t, results: []process.Result{
		{ExitCode: 0, Stdout: `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Hangzhou DeepSeek Artificial Intelligence Co., Ltd","Organization":"Hangzhou DeepSeek Artificial Intelligence Co., Ltd","Subject":"CN=Hangzhou DeepSeek Artificial Intelligence Co., Ltd, O=Hangzhou DeepSeek Artificial Intelligence Co., Ltd, C=CN","Issuer":"CN=Some Code Signing CA"}`},
	}}
	result, err := Install(context.Background(), DSHDesktopID, Options{
		Home: t.TempDir(), Platform: platform.For("windows", "x64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "installer-started" || len(runner.started) != 1 || result.App.Version == nil || *result.App.Version != "0.1.7-rc.2" {
		t.Fatalf("Install() = %#v started=%#v", result, runner.started)
	}
	if err := os.Remove(runner.started[0][0]); err != nil {
		t.Fatal(err)
	}
}

// Any valid signature used to pass here. The digest proves the bytes are what the
// feed described; the publisher check is what proves who published them.
func TestDSHWindowsInstallRejectsUnexpectedPublisher(t *testing.T) {
	payload := []byte("DeepSeek Harness Windows installer")
	feed := dshFeedYAML("0.1.7-rc.2", dshWinExeURL, dshDigest(payload), int64(len(payload)))
	downloader := &routeDownloader{routes: map[string][]byte{dshWinFeedURL: feed, dshWinExeURL: payload}}
	runner := &dshRunner{t: t, results: []process.Result{
		{ExitCode: 0, Stdout: `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Someone Else","Organization":"Someone Else","Subject":"CN=Someone Else, O=Someone Else","Issuer":"CN=Trusted CA"}`},
	}}
	_, err := Install(context.Background(), DSHDesktopID, Options{
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

// Detection reads the bundle identifier, so a directory that merely carries the
// vendor's name is not reported as installed, and the version comes from the
// bundle on disk rather than anything BootAgent remembers -- the app updates
// itself.
func TestInspectDSHMacOSMatchesOnBundleIDAndReportsTheInstalledVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, dshDesktopAppName, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	stranger := &dshRunner{t: t, results: []process.Result{{ExitCode: 0, Stdout: "com.example.other\n"}}}
	status := Inspect(context.Background(), DSHDesktopID, Options{Platform: platform.For("macos", "arm64"), Runner: stranger, SearchRoots: []string{root}})
	if status.Installed {
		t.Fatalf("a foreign bundle named %s was reported installed: %#v", dshDesktopAppName, status)
	}
	genuine := &dshRunner{t: t, results: []process.Result{
		{ExitCode: 0, Stdout: DSHDesktopBundleID + "\n"},
		{ExitCode: 0, Stdout: "0.1.7-rc.2\n"},
	}}
	status = Inspect(context.Background(), DSHDesktopID, Options{Platform: platform.For("macos", "arm64"), Runner: genuine, SearchRoots: []string{root}})
	if !status.Installed || status.Path != filepath.Join(root, dshDesktopAppName) || status.Version == nil || *status.Version != "0.1.7-rc.2" || status.Source != SourceMacOSZIP {
		t.Fatalf("Inspect() = %#v", status)
	}
}

func TestInspectDSHWindowsLooksInThePerUserProgramsDirectory(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(home, "AppData", "Local", "Programs", "DeepSeek Harness", dshDesktopExeName)
	status := Inspect(context.Background(), DSHDesktopID, Options{Home: home, Platform: platform.For("windows", "x64")})
	if status.Installed || !status.Supported {
		t.Fatalf("Inspect() before install = %#v", status)
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	status = Inspect(context.Background(), DSHDesktopID, Options{Home: home, Platform: platform.For("windows", "x64")})
	if !status.Installed || status.Path != exe {
		t.Fatalf("Inspect() after install = %#v", status)
	}
}

func TestDSHIsNotOfferedOnIntelMacOrLinux(t *testing.T) {
	for _, info := range []platform.Info{platform.For("macos", "amd64"), platform.For("linux", "amd64")} {
		_, err := Install(context.Background(), DSHDesktopID, Options{Home: t.TempDir(), Platform: info, Runner: &dshRunner{t: t}, SearchRoots: []string{t.TempDir()}})
		if err == nil {
			t.Errorf("Install() on %s/%s succeeded; the vendor ships no package there", info.OS, info.Arch)
		}
	}
}
