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
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	KimiCodeDesktopID   = "kimi-code-desktop"
	KimiCodeDesktopName = "Kimi Code Desktop"
	KimiCodeDesktopHome = "https://www.kimi.com/code"
	// KimiCodeAgentID is the CLI Agent whose ~/.kimi-code/config.toml the
	// desktop app shares: the vendor documents the two as sharing accounts,
	// models and providers under ~/.kimi-code.
	KimiCodeAgentID = "kimi-code"

	// KimiCodeDesktopBundleID and KimiCodeDesktopTeamID are read off the signed
	// 1.0.4 release: codesign reports Identifier=com.kimi.code.desktop and
	// TeamIdentifier=2J9472RW75 under "Developer ID Application: Beijing Moonshot
	// Technology Co., Ltd (2J9472RW75)", and spctl accepts it as notarized. The
	// team is pinned; the authority's common name is left free, as for ZCode.
	KimiCodeDesktopBundleID = "com.kimi.code.desktop"
	KimiCodeDesktopTeamID   = "2J9472RW75"

	// KimiCodeDesktopWindowsPublisher is the O and CN of the certificate that
	// signs KimiCode-1.0.4-win-x64.exe (issuer Microsoft ID Verified CS EOC CA
	// 04). It is not the Moonshot entity behind the macOS team: it was read out
	// of the vendor's own published installer, and pinning it was a deliberate
	// decision rather than an assumption that the two are the same company. If
	// the vendor changes signer, installs stop with a publisher error instead of
	// trusting the new one.
	KimiCodeDesktopWindowsPublisher = "NOVASCENT PRIVATE LIMITED"

	// KimiCodeDesktopFeedBase is what the installed app's own
	// Contents/Resources/app-update.yml points electron-updater at (generic
	// provider, channel latest). The feed lists its files relative to it, and
	// code.kimi.com answers both with a redirect to cdn.kimi.com.
	KimiCodeDesktopFeedHost = "code.kimi.com"
	KimiCodeDesktopCDNHost  = "cdn.kimi.com"
	KimiCodeDesktopFeedBase = "https://" + KimiCodeDesktopFeedHost + "/kimi-code/desktop/"

	kimiCodeDesktopAppName = "Kimi Code.app"
	kimiCodeDesktopExeName = "Kimi Code.exe"
)

// kimiCodeFeed is the electron-updater manifest. Only what BootAgent acts on is
// decoded.
type kimiCodeFeed struct {
	Version string             `yaml:"version"`
	Files   []kimiCodeFeedFile `yaml:"files"`
}

type kimiCodeFeedFile struct {
	URL    string `yaml:"url"`
	SHA512 string `yaml:"sha512"`
	Size   int64  `yaml:"size"`
}

// kimiCodeTarget names the build for a platform, or "" when the vendor
// publishes none BootAgent can install. It decides both what Status.Supported
// reports and which file the install picks, so the UI cannot offer an install
// the lookup then refuses. Windows on ARM gets the x64 installer, the vendor's
// only Windows build.
func kimiCodeTarget(osID, arch string) string {
	switch osID {
	case "macos":
		switch strings.ToLower(strings.TrimSpace(arch)) {
		case "arm64", "aarch64":
			return "mac-arm64"
		case "x64", "amd64", "x86_64":
			return "mac-x64"
		}
	case "windows":
		return "win-x64"
	}
	return ""
}

func kimiCodeFeedURL(osID, arch string) (string, error) {
	switch target := kimiCodeTarget(osID, arch); {
	case strings.HasPrefix(target, "mac-"):
		return KimiCodeDesktopFeedBase + "latest-mac.yml", nil
	case target == "win-x64":
		return KimiCodeDesktopFeedBase + "latest.yml", nil
	}
	if osID == "macos" || osID == "windows" {
		return "", fmt.Errorf("%s has no package for %s/%s", KimiCodeDesktopName, osID, arch)
	}
	return "", fmt.Errorf("%s is not supported on %s", KimiCodeDesktopName, osID)
}

func baseKimiCodeStatus(osID, arch string) Status {
	status := Status{ID: KimiCodeDesktopID, Name: KimiCodeDesktopName, Source: SourceUnknown}
	if kimiCodeTarget(osID, arch) == "" {
		return status
	}
	switch osID {
	case "macos":
		status.Supported, status.Source = true, SourceMacOSZIP
	case "windows":
		status.Supported, status.Source = true, SourceWindowsInstaller
	}
	return status
}

func inspectKimiCode(ctx context.Context, options Options) Status {
	status := baseKimiCodeStatus(options.Platform.OS, options.Platform.Arch)
	if err := contextError(ctx); err != nil {
		status.InspectionUnavailable = nonEmptyPointer(err.Error())
		return status
	}
	if !status.Supported {
		return status
	}
	var found Status
	var err error
	switch options.Platform.OS {
	case "macos":
		found, err = inspectKimiCodeMacOS(ctx, options)
	case "windows":
		found, err = inspectKimiCodeWindows(options)
	}
	if err != nil {
		found.InspectionUnavailable = nonEmptyPointer(err.Error())
	}
	return found
}

func inspectKimiCodeMacOS(ctx context.Context, options Options) (Status, error) {
	status := baseKimiCodeStatus("macos", options.Platform.Arch)
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
			candidate = filepath.Join(root, kimiCodeDesktopAppName)
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
		// Read from the plist, so a renamed .app cannot pass as Kimi Code.
		if metadata.bundleID != KimiCodeDesktopBundleID {
			continue
		}
		status.Installed, status.Path, status.Version = true, candidate, metadata.version
		return status, nil
	}
	return status, lastErr
}

func inspectKimiCodeWindows(options Options) (Status, error) {
	status := baseKimiCodeStatus("windows", options.Platform.Arch)
	for _, candidate := range kimiCodeWindowsCandidates(options) {
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return status, err
		}
		if info.IsDir() {
			continue
		}
		status.Installed, status.Path = true, candidate
		return status, nil
	}
	return status, nil
}

// kimiCodeWindowsCandidates are where an electron-builder NSIS installer puts
// the app: per-user under %LOCALAPPDATA%\Programs by default, machine-wide
// under Program Files when the user picks that. The executable name is
// verified -- the installer's app-64.7z carries "Kimi Code.exe" -- but the
// directory is electron-builder's default for that product name, not an
// observed installation: the NSIS script is compressed and this machine is
// macOS.
func kimiCodeWindowsCandidates(options Options) []string {
	if len(options.SearchRoots) > 0 {
		candidates := make([]string, 0, len(options.SearchRoots))
		for _, root := range options.SearchRoots {
			if strings.EqualFold(filepath.Ext(root), ".exe") {
				candidates = append(candidates, root)
				continue
			}
			candidates = append(candidates, filepath.Join(root, kimiCodeDesktopExeName))
		}
		return candidates
	}
	candidates := make([]string, 0, 2)
	if options.Home != "" {
		candidates = append(candidates, filepath.Join(options.Home, "AppData", "Local", "Programs", "Kimi Code", kimiCodeDesktopExeName))
	}
	return append(candidates, filepath.Join("C:\\", "Program Files", "Kimi Code", kimiCodeDesktopExeName))
}

// fetchKimiCodeFeed reads the manifest with no-cache: it is the rolling pointer
// to the current release, and a CDN in front of it would otherwise be free to
// pin BootAgent to a stale one.
func fetchKimiCodeFeed(ctx context.Context, options Options) (kimiCodeFeed, string, error) {
	endpoint, err := kimiCodeFeedURL(options.Platform.OS, options.Platform.Arch)
	if err != nil {
		return kimiCodeFeed{}, "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return kimiCodeFeed{}, "", err
	}
	request.Header.Set("Cache-Control", "no-cache")
	client := options.Downloader
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return kimiCodeFeed{}, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return kimiCodeFeed{}, "", fmt.Errorf("%s update request returned HTTP %d", KimiCodeDesktopName, response.StatusCode)
	}
	var feed kimiCodeFeed
	if err := yaml.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&feed); err != nil {
		return kimiCodeFeed{}, "", fmt.Errorf("decode %s update response: %w", KimiCodeDesktopName, err)
	}
	return feed, endpoint, nil
}

// kimiCodeArtifact picks this platform's file from the feed and resolves its
// URL. The feed lists files relative to itself, as electron-updater's generic
// provider allows; they are resolved against the feed URL BootAgent asked for,
// never against wherever a redirect landed, and the result still has to be on
// the vendor's hosts.
//
// On macOS this is the .zip, the file electron-updater itself consumes and the
// one the feed's top-level path names.
func kimiCodeArtifact(feed kimiCodeFeed, feedURL, osID, arch string) (kimiCodeFeedFile, error) {
	target := kimiCodeTarget(osID, arch)
	if target == "" {
		return kimiCodeFeedFile{}, fmt.Errorf("%s has no package for %s/%s", KimiCodeDesktopName, osID, arch)
	}
	wanted := ".exe"
	if osID == "macos" {
		wanted = ".zip"
	}
	base, err := url.Parse(feedURL)
	if err != nil {
		return kimiCodeFeedFile{}, fmt.Errorf("parse %s feed URL: %w", KimiCodeDesktopName, err)
	}
	for _, file := range feed.Files {
		reference, err := url.Parse(strings.TrimSpace(file.URL))
		if err != nil {
			continue
		}
		name := strings.ToLower(filepath.Base(reference.Path))
		if !strings.HasSuffix(name, "-"+target+wanted) {
			continue
		}
		approved, err := approvedDownloadURL(base.ResolveReference(reference).String(), KimiCodeDesktopFeedHost, KimiCodeDesktopCDNHost)
		if err != nil {
			return kimiCodeFeedFile{}, fmt.Errorf("validate %s package URL: %w", KimiCodeDesktopName, err)
		}
		if strings.TrimSpace(file.SHA512) == "" {
			return kimiCodeFeedFile{}, fmt.Errorf("%s update feed lists no digest for %s", KimiCodeDesktopName, target+wanted)
		}
		file.URL = approved
		return file, nil
	}
	return kimiCodeFeedFile{}, fmt.Errorf("%s update feed lists no %s package", KimiCodeDesktopName, target+wanted)
}

// kimiCodePackage resolves what to download. DownloadURL is the test and
// self-hosting seam the other agents use; it still has to pass the host
// allowlist, and it carries no digest, so it verifies by signature alone.
func kimiCodePackage(ctx context.Context, options Options) (kimiCodeFeed, kimiCodeFeedFile, error) {
	if raw := strings.TrimSpace(options.DownloadURL); raw != "" {
		approved, err := approvedDownloadURL(raw, KimiCodeDesktopFeedHost, KimiCodeDesktopCDNHost)
		return kimiCodeFeed{}, kimiCodeFeedFile{URL: approved}, err
	}
	feed, feedURL, err := fetchKimiCodeFeed(ctx, options)
	if err != nil {
		return kimiCodeFeed{}, kimiCodeFeedFile{}, err
	}
	artifact, err := kimiCodeArtifact(feed, feedURL, options.Platform.OS, options.Platform.Arch)
	if err != nil {
		return kimiCodeFeed{}, kimiCodeFeedFile{}, err
	}
	return feed, artifact, nil
}

// verifyKimiCodeDigest checks the downloaded bytes against the feed. The digest
// is what authenticates the archive: the host allowlist only says who was
// asked. Size first, so a truncated transfer reports the useful error.
func verifyKimiCodeDigest(path string, expected kimiCodeFeedFile) error {
	want, err := base64.StdEncoding.DecodeString(strings.TrimSpace(expected.SHA512))
	if err != nil {
		return fmt.Errorf("decode expected %s digest: %w", KimiCodeDesktopName, err)
	}
	if len(want) != sha512.Size {
		return fmt.Errorf("expected %s digest is not a SHA-512 value", KimiCodeDesktopName)
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
		return fmt.Errorf("downloaded %s package is %d bytes, expected %d", KimiCodeDesktopName, info.Size(), expected.Size)
	}
	digest := sha512.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(digest.Sum(nil), want) != 1 {
		return fmt.Errorf("downloaded %s package failed its SHA-512 check", KimiCodeDesktopName)
	}
	return nil
}

func installKimiCode(ctx context.Context, options Options) (ActionResult, error) {
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	status := inspectKimiCode(ctx, options)
	if status.Installed {
		return ActionResult{Status: "already-installed", Message: KimiCodeDesktopName + " is already installed", App: status}, nil
	}
	if !status.Supported {
		return ActionResult{}, fmt.Errorf("%s is not supported on %s/%s", KimiCodeDesktopName, options.Platform.OS, options.Platform.Arch)
	}
	switch options.Platform.OS {
	case "macos":
		return installKimiCodeMacOS(ctx, options)
	case "windows":
		return installKimiCodeWindows(ctx, options)
	}
	return ActionResult{}, fmt.Errorf("%s is not supported on %s", KimiCodeDesktopName, options.Platform.OS)
}

func installKimiCodeMacOS(ctx context.Context, options Options) (ActionResult, error) {
	feed, artifact, err := kimiCodePackage(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	tempDir, err := os.MkdirTemp("", "bootagent-kimi-code-")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary %s installer directory: %w", KimiCodeDesktopName, err)
	}
	defer os.RemoveAll(tempDir)
	archive := filepath.Join(tempDir, "KimiCode.zip")
	if err := downloadFile(ctx, options, artifact.URL, archive, KimiCodeDesktopID); err != nil {
		return ActionResult{}, fmt.Errorf("download %s installer: %w", KimiCodeDesktopName, err)
	}
	if artifact.SHA512 != "" {
		if err := verifyKimiCodeDigest(archive, artifact); err != nil {
			return ActionResult{}, err
		}
	}
	extracted := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extracted, 0o700); err != nil {
		return ActionResult{}, err
	}
	result, err := run(options, ctx, []string{"/usr/bin/ditto", "-x", "-k", archive, extracted}, installTimeout)
	if err != nil {
		return ActionResult{}, fmt.Errorf("extract %s installer: %w", KimiCodeDesktopName, err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("extract "+KimiCodeDesktopName+" installer", result)
	}
	appPath, err := findKimiCodeApp(extracted)
	if err != nil {
		return ActionResult{}, err
	}
	metadata, err := readMacMetadata(ctx, options, appPath)
	if err != nil {
		return ActionResult{}, fmt.Errorf("inspect downloaded %s app: %w", KimiCodeDesktopName, err)
	}
	if metadata.bundleID != KimiCodeDesktopBundleID {
		return ActionResult{}, fmt.Errorf("downloaded app has unexpected bundle identifier %q", metadata.bundleID)
	}
	if err := verifyKimiCodeMacOSApp(ctx, options, appPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded %s app: %w", KimiCodeDesktopName, err)
	}
	var lastErr error
	for _, destination := range kimiCodeDestinations(options) {
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
			lastErr = commandFailure("copy "+KimiCodeDesktopName+" app", copied)
			continue
		}
		installed := baseKimiCodeStatus("macos", options.Platform.Arch)
		installed.Installed, installed.Path = true, destination
		installed.Version = metadata.version
		if installed.Version == nil {
			installed.Version = nonEmptyPointer(feed.Version)
		}
		return ActionResult{Status: "installed", Message: KimiCodeDesktopName + " was installed", RefreshNeeded: true, App: installed}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no writable macOS Applications directory")
	}
	return ActionResult{}, lastErr
}

// installKimiCodeWindows verifies the digest and Authenticode, then starts the
// vendor's NSIS installer, which owns its own placement and shortcuts.
func installKimiCodeWindows(ctx context.Context, options Options) (ActionResult, error) {
	feed, artifact, err := kimiCodePackage(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	installer, err := os.CreateTemp("", "bootagent-kimi-code-*.exe")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary %s installer: %w", KimiCodeDesktopName, err)
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
	if err := downloadFile(ctx, options, artifact.URL, installerPath, KimiCodeDesktopID); err != nil {
		return ActionResult{}, fmt.Errorf("download %s installer: %w", KimiCodeDesktopName, err)
	}
	if artifact.SHA512 != "" {
		if err := verifyKimiCodeDigest(installerPath, artifact); err != nil {
			return ActionResult{}, err
		}
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	if err := verifyKimiCodeWindowsInstaller(ctx, options, installerPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded %s installer with Authenticode: %w", KimiCodeDesktopName, err)
	}
	if err := start(options, []string{installerPath}); err != nil {
		return ActionResult{}, fmt.Errorf("start %s installer: %w", KimiCodeDesktopName, err)
	}
	keep = true
	status := baseKimiCodeStatus("windows", options.Platform.Arch)
	status.Version = nonEmptyPointer(feed.Version)
	return ActionResult{Status: "installer-started", Message: "The downloaded " + KimiCodeDesktopName + " installer was started", RefreshNeeded: true, App: status}, nil
}

func kimiCodeDestinations(options Options) []string {
	dirs := options.ApplicationDirs
	if len(dirs) == 0 {
		dirs = []string{"/Applications"}
		if options.Home != "" {
			dirs = append(dirs, filepath.Join(options.Home, "Applications"))
		}
	}
	result := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		result = append(result, filepath.Join(dir, kimiCodeDesktopAppName))
	}
	return result
}

func findKimiCodeApp(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.EqualFold(filepath.Base(path), kimiCodeDesktopAppName) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("downloaded %s archive contains no %s", KimiCodeDesktopName, kimiCodeDesktopAppName)
	}
	return found, nil
}

func openKimiCode(ctx context.Context, options Options) error {
	status := inspectKimiCode(ctx, options)
	if !status.Installed {
		if status.InspectionUnavailable != nil {
			return errors.New(*status.InspectionUnavailable)
		}
		return errors.New(KimiCodeDesktopName + " is not installed")
	}
	switch options.Platform.OS {
	case "macos":
		return start(options, []string{"/usr/bin/open", "-a", status.Path})
	case "windows":
		return start(options, []string{status.Path})
	}
	return fmt.Errorf("%s is not supported on %s", KimiCodeDesktopName, options.Platform.OS)
}
