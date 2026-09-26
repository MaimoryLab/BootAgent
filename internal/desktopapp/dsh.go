package desktopapp

import (
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"gopkg.in/yaml.v3"
)

// DeepSeek Harness is DeepSeek's own desktop shell around dsh. It replaced the
// third-party "DSH Desktop" build (anywhere-labs) this entry used to install: the
// ID is kept so existing Agent bindings and the Profile the CLI shares keep
// resolving, while everything the ID points at -- name, publisher, download
// origin, bundle, install paths -- is now the vendor's.
const (
	DSHDesktopID   = "dsh-desktop"
	DSHDesktopName = "DeepSeek Harness"
	DSHDesktopHome = "https://www.deepseek.com/harness/"

	// DSHDesktopBundleID and DSHDesktopTeamID are read off the signed release:
	// codesign reports Identifier=com.deepseek.dsh and TeamIdentifier=NAN929V4UM
	// under "Developer ID Application: Hangzhou DeepSeek Artificial Intelligence
	// Co., Ltd (NAN929V4UM)". The Team ID is what the signature check pins; the
	// authority's common name is left free so a certificate renewal that keeps
	// the team does not break installs.
	DSHDesktopBundleID = "com.deepseek.dsh"
	DSHDesktopTeamID   = "NAN929V4UM"

	// DSHDesktopWindowsPublisher is the legal entity behind the macOS Developer ID
	// team, which is also what the vendor's Windows signing derives publisherName
	// from (the certificate's O attribute). Read off the same release line; the
	// Windows certificate itself was not inspected on this machine, so a mismatch
	// here surfaces as a refused install rather than an accepted stranger.
	DSHDesktopWindowsPublisher = "Hangzhou DeepSeek Artificial Intelligence Co., Ltd"

	// DSHDesktopDownloadHost is the vendor's release origin. The desktop app's own
	// app-update.yml points electron-updater at dsh-desk/feeds/<target>/ under it,
	// and the installers live under dsh-desk/bin/<target>/.
	DSHDesktopDownloadHost = "download.deepseek.com"
	DSHDesktopFeedBase     = "https://" + DSHDesktopDownloadHost + "/dsh-desk/feeds/"

	dshDesktopAppName = "DeepSeek Harness.app"
	dshDesktopExeName = "DeepSeek Harness.exe"
)

// dshFeed is the electron-updater manifest the vendor publishes per target.
// Only what BootAgent acts on is decoded.
type dshFeed struct {
	Version string        `yaml:"version"`
	Files   []dshFeedFile `yaml:"files"`
}

type dshFeedFile struct {
	URL    string `yaml:"url"`
	SHA512 string `yaml:"sha512"`
	Size   int64  `yaml:"size"`
}

// dshTarget names the vendor's feed directory for a platform, or "" when the
// vendor publishes nothing BootAgent can install there. This is the one place
// that decides both what Status.Supported reports and what dshFeedURL fetches,
// so the UI cannot offer an install that the lookup then refuses.
//
// The vendor publishes mac-arm64 and win-x64 only. An Intel Mac gets nothing:
// there is no x64 build and Rosetta does not run arm64 binaries. Windows on ARM
// gets the x64 installer, which is the supported way to install x64-only
// products there (see platform.nativeArch) and what the vendor's own updater
// would serve on such a machine.
func dshTarget(osID, arch string) string {
	switch osID {
	case "macos":
		if arch == "arm64" {
			return "mac-arm64"
		}
	case "windows":
		return "win-x64"
	}
	return ""
}

// dshFeedURL names the manifest for one target. The channel is "nightly" in the
// shipped app-update.yml even for release-candidate builds, so that is the file
// name electron-updater asks for and the one that exists.
func dshFeedURL(osID, arch string) (string, error) {
	target := dshTarget(osID, arch)
	switch target {
	case "mac-arm64":
		return DSHDesktopFeedBase + target + "/nightly-mac.yml", nil
	case "win-x64":
		return DSHDesktopFeedBase + target + "/nightly.yml", nil
	}
	if osID == "macos" || osID == "windows" {
		return "", fmt.Errorf("%s has no package for %s/%s", DSHDesktopName, osID, arch)
	}
	return "", fmt.Errorf("%s is not supported on %s", DSHDesktopName, osID)
}

// fetchDSHFeed reads the manifest with no-cache: it is the rolling pointer to
// the current build, and the CDN in front of it would otherwise be free to pin
// BootAgent to a stale one.
func fetchDSHFeed(ctx context.Context, options Options) (dshFeed, error) {
	endpoint, err := dshFeedURL(options.Platform.OS, options.Platform.Arch)
	if err != nil {
		return dshFeed{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return dshFeed{}, err
	}
	request.Header.Set("Cache-Control", "no-cache")
	client := options.Downloader
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return dshFeed{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return dshFeed{}, fmt.Errorf("%s update request returned HTTP %d", DSHDesktopName, response.StatusCode)
	}
	var feed dshFeed
	if err := yaml.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&feed); err != nil {
		return dshFeed{}, fmt.Errorf("decode %s update response: %w", DSHDesktopName, err)
	}
	return feed, nil
}

// dshArtifact picks the file to download and returns it with its digest.
//
// On macOS this is the .zip, which is the file the feed's digest describes and
// the one electron-updater itself consumes. A .dmg is published beside it but
// carries no digest in the manifest, so preferring it would trade a verified
// download for an unverified one.
func dshArtifact(feed dshFeed, osID string) (dshFeedFile, error) {
	wanted := ".exe"
	if osID == "macos" {
		wanted = ".zip"
	}
	for _, file := range feed.Files {
		if !strings.EqualFold(filepath.Ext(mustParseURLPath(file.URL)), wanted) {
			continue
		}
		approved, err := approvedDownloadURL(file.URL, DSHDesktopDownloadHost)
		if err != nil {
			return dshFeedFile{}, fmt.Errorf("validate %s package URL: %w", DSHDesktopName, err)
		}
		if strings.TrimSpace(file.SHA512) == "" {
			return dshFeedFile{}, fmt.Errorf("%s update feed lists no digest for %s", DSHDesktopName, wanted)
		}
		file.URL = approved
		return file, nil
	}
	return dshFeedFile{}, fmt.Errorf("%s update feed lists no %s package", DSHDesktopName, wanted)
}

// dshPackage resolves what to download. DownloadURL is the test and self-hosting
// seam the other agents use; it still has to pass the host allowlist, and it
// carries no digest, so it verifies by signature alone.
func dshPackage(ctx context.Context, options Options) (dshFeed, dshFeedFile, error) {
	if raw := strings.TrimSpace(options.DownloadURL); raw != "" {
		approved, err := approvedDownloadURL(raw, DSHDesktopDownloadHost)
		return dshFeed{}, dshFeedFile{URL: approved}, err
	}
	feed, err := fetchDSHFeed(ctx, options)
	if err != nil {
		return dshFeed{}, dshFeedFile{}, err
	}
	artifact, err := dshArtifact(feed, options.Platform.OS)
	if err != nil {
		return dshFeed{}, dshFeedFile{}, err
	}
	return feed, artifact, nil
}

// verifyDSHDigest checks the downloaded bytes against the feed. The digest is
// what authenticates the archive: the host allowlist only says who was asked.
// Size first, so a truncated transfer reports the useful error.
func verifyDSHDigest(path string, expected dshFeedFile) error {
	want, err := base64.StdEncoding.DecodeString(strings.TrimSpace(expected.SHA512))
	if err != nil {
		return fmt.Errorf("decode expected %s digest: %w", DSHDesktopName, err)
	}
	if len(want) != sha512.Size {
		return fmt.Errorf("expected %s digest is not a SHA-512 value", DSHDesktopName)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if expected.Size > 0 && info.Size() != expected.Size {
		return fmt.Errorf("downloaded %s package is %d bytes, expected %d", DSHDesktopName, info.Size(), expected.Size)
	}
	digest := sha512.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(digest.Sum(nil), want) != 1 {
		return fmt.Errorf("downloaded %s package failed its SHA-512 check", DSHDesktopName)
	}
	return nil
}

// baseDSHStatus reports Supported only for a platform the vendor publishes a
// package for. The OS alone is not enough: an Intel Mac is macOS and gets no
// package, and a Supported=true there is an install button that always fails.
func baseDSHStatus(info platform.Info) Status {
	status := Status{ID: DSHDesktopID, Name: DSHDesktopName, Source: SourceUnknown}
	switch dshTarget(info.OS, info.Arch) {
	case "mac-arm64":
		status.Supported, status.Source = true, SourceMacOSZIP
	case "win-x64":
		status.Supported, status.Source = true, SourceWindowsInstaller
	}
	return status
}

func inspectDSH(ctx context.Context, options Options) Status {
	status := baseDSHStatus(options.Platform)
	if err := contextError(ctx); err != nil {
		status.InspectionUnavailable = nonEmptyPointer(err.Error())
		return status
	}
	switch options.Platform.OS {
	case "macos":
		found, err := inspectDSHMacOS(ctx, options)
		if err != nil {
			found.InspectionUnavailable = nonEmptyPointer(err.Error())
		}
		return found
	case "windows":
		for _, candidate := range dshWindowsCandidates(options) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				status.Installed, status.Path = true, candidate
				return status
			}
		}
	}
	return status
}

// inspectDSHMacOS reads the bundle identifier out of each candidate rather than
// trusting the directory name, and reports the installed version from the same
// plist: the app updates itself through electron-updater, so the version on
// disk is not the one BootAgent installed.
func inspectDSHMacOS(ctx context.Context, options Options) (Status, error) {
	status := baseDSHStatus(options.Platform)
	roots := options.SearchRoots
	if len(roots) == 0 {
		roots = []string{"/Applications"}
		if options.Home != "" {
			roots = append(roots, filepath.Join(options.Home, "Applications"))
		}
	}
	var lastErr error
	for _, root := range roots {
		candidate := root
		if !strings.EqualFold(filepath.Ext(root), ".app") {
			candidate = filepath.Join(root, dshDesktopAppName)
		}
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			lastErr = err
			continue
		}
		if !info.IsDir() {
			continue
		}
		metadata, err := readMacMetadata(ctx, options, candidate)
		if err != nil {
			lastErr = err
			continue
		}
		if metadata.bundleID != DSHDesktopBundleID {
			continue
		}
		status.Installed, status.Path, status.Version = true, candidate, metadata.version
		return status, nil
	}
	return status, lastErr
}

// dshWindowsCandidates lists where the vendor's NSIS installer places the app.
// It is configured perMachine: false with a fixed directory, which for
// electron-builder is %LOCALAPPDATA%\Programs\<productName>. Not verified on
// Windows: this machine is macOS, so the path follows electron-builder's
// documented default rather than an observed installation.
func dshWindowsCandidates(options Options) []string {
	if len(options.SearchRoots) > 0 {
		candidates := make([]string, 0, len(options.SearchRoots))
		for _, root := range options.SearchRoots {
			if strings.EqualFold(filepath.Ext(root), ".exe") {
				candidates = append(candidates, root)
				continue
			}
			candidates = append(candidates, filepath.Join(root, dshDesktopExeName))
		}
		return candidates
	}
	if options.Home == "" {
		return nil
	}
	return []string{filepath.Join(options.Home, "AppData", "Local", "Programs", "DeepSeek Harness", dshDesktopExeName)}
}

func installDSH(ctx context.Context, options Options) (ActionResult, error) {
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	status := inspectDSH(ctx, options)
	if status.Installed {
		return ActionResult{Status: "already-installed", Message: DSHDesktopName + " is already installed", App: status}, nil
	}
	switch options.Platform.OS {
	case "macos":
		return installDSHMacOS(ctx, options)
	case "windows":
		return installDSHWindows(ctx, options)
	}
	return ActionResult{}, fmt.Errorf("%s is not supported on %s", DSHDesktopName, options.Platform.OS)
}

func installDSHMacOS(ctx context.Context, options Options) (ActionResult, error) {
	feed, artifact, err := dshPackage(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	tempDir, err := os.MkdirTemp("", "bootagent-dsh-")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary %s installer directory: %w", DSHDesktopName, err)
	}
	defer os.RemoveAll(tempDir)
	archive := filepath.Join(tempDir, "DeepSeek Harness.zip")
	if err := downloadFile(ctx, options, artifact.URL, archive, DSHDesktopID); err != nil {
		return ActionResult{}, fmt.Errorf("download %s installer: %w", DSHDesktopName, err)
	}
	// Only when the feed supplied one. A DownloadURL override has no manifest to
	// compare against, and the signature check below is what gates it.
	if artifact.SHA512 != "" {
		if err := verifyDSHDigest(archive, artifact); err != nil {
			return ActionResult{}, err
		}
	}
	extracted := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extracted, 0o700); err != nil {
		return ActionResult{}, err
	}
	result, err := run(options, ctx, []string{"/usr/bin/ditto", "-x", "-k", archive, extracted}, installTimeout)
	if err != nil {
		return ActionResult{}, fmt.Errorf("extract %s installer: %w", DSHDesktopName, err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("extract "+DSHDesktopName+" installer", result)
	}
	appPath, err := findDSHApp(extracted)
	if err != nil {
		return ActionResult{}, err
	}
	metadata, err := readMacMetadata(ctx, options, appPath)
	if err != nil {
		return ActionResult{}, fmt.Errorf("inspect downloaded %s app: %w", DSHDesktopName, err)
	}
	if metadata.bundleID != DSHDesktopBundleID {
		return ActionResult{}, fmt.Errorf("downloaded app has unexpected bundle identifier %q", metadata.bundleID)
	}
	if err := verifyDSHMacOSApp(ctx, options, appPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded %s app: %w", DSHDesktopName, err)
	}
	var lastErr error
	for _, destination := range dshDestinations(options) {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			lastErr = err
			continue
		}
		if _, err := os.Stat(destination); err == nil {
			lastErr = fmt.Errorf("destination already exists: %s", destination)
			continue
		} else if !os.IsNotExist(err) {
			lastErr = err
			continue
		}
		copied, copyErr := run(options, ctx, []string{"/usr/bin/ditto", appPath, destination}, installTimeout)
		if copyErr != nil {
			lastErr = copyErr
			continue
		}
		if copied.ExitCode != 0 {
			lastErr = commandFailure("copy "+DSHDesktopName+" app", copied)
			continue
		}
		installed := baseDSHStatus(options.Platform)
		installed.Installed, installed.Path, installed.Version = true, destination, metadata.version
		if installed.Version == nil {
			installed.Version = nonEmptyPointer(feed.Version)
		}
		return ActionResult{Status: "installed", Message: DSHDesktopName + " was installed", RefreshNeeded: true, App: installed}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no writable macOS Applications directory")
	}
	return ActionResult{}, lastErr
}

// installDSHWindows starts the vendor's own installer after Authenticode passes.
// The .exe is an NSIS installer that owns its placement and shortcuts.
func installDSHWindows(ctx context.Context, options Options) (ActionResult, error) {
	feed, artifact, err := dshPackage(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	installer, err := os.CreateTemp("", "bootagent-dsh-*.exe")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary %s installer: %w", DSHDesktopName, err)
	}
	installerPath := installer.Name()
	if err := installer.Close(); err != nil {
		_ = os.Remove(installerPath)
		return ActionResult{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(installerPath)
		}
	}()
	if err := downloadFile(ctx, options, artifact.URL, installerPath, DSHDesktopID); err != nil {
		return ActionResult{}, fmt.Errorf("download %s installer: %w", DSHDesktopName, err)
	}
	if artifact.SHA512 != "" {
		if err := verifyDSHDigest(installerPath, artifact); err != nil {
			return ActionResult{}, err
		}
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	if err := verifyDSHWindowsInstaller(ctx, options, installerPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded %s installer with Authenticode: %w", DSHDesktopName, err)
	}
	if err := start(options, []string{installerPath}); err != nil {
		return ActionResult{}, fmt.Errorf("start %s installer: %w", DSHDesktopName, err)
	}
	keep = true
	status := baseDSHStatus(options.Platform)
	status.Version = nonEmptyPointer(feed.Version)
	return ActionResult{Status: "installer-started", Message: "The downloaded " + DSHDesktopName + " installer was started", RefreshNeeded: true, App: status}, nil
}

func dshDestinations(options Options) []string {
	dirs := options.ApplicationDirs
	if len(dirs) == 0 {
		dirs = []string{"/Applications"}
		if options.Home != "" {
			dirs = append(dirs, filepath.Join(options.Home, "Applications"))
		}
	}
	result := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		result = append(result, filepath.Join(dir, dshDesktopAppName))
	}
	return result
}

func findDSHApp(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.EqualFold(filepath.Base(path), dshDesktopAppName) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("downloaded %s archive contains no %s", DSHDesktopName, dshDesktopAppName)
	}
	return found, nil
}

func openDSH(ctx context.Context, options Options) error {
	status := inspectDSH(ctx, options)
	if !status.Installed {
		return errors.New(DSHDesktopName + " is not installed")
	}
	switch options.Platform.OS {
	case "macos":
		return start(options, []string{"/usr/bin/open", "-a", status.Path})
	case "windows":
		return start(options, []string{status.Path})
	}
	return fmt.Errorf("%s is not supported on %s", DSHDesktopName, options.Platform.OS)
}
