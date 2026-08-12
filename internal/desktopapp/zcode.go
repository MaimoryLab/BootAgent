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

	"gopkg.in/yaml.v3"
)

const (
	ZCodeID       = "zcode"
	ZCodeName     = "ZCode"
	ZCodeBundleID = "dev.zcode.app"
	// ZCodeMacTeamID is the Developer ID team behind the signed app on macOS,
	// read from the installed bundle rather than from documentation.
	ZCodeMacTeamID = "8A5X4JJ39T"
	ZCodeHome      = "https://zcode.z.ai/"
	// ZCodeUpdateHost serves both the update feed and the artifacts. The feed URL
	// is not guessed: it is what the installed app's own
	// Contents/Resources/app-update.yml points at, so the endpoint carries the
	// vendor's authority rather than ours.
	ZCodeUpdateHost = "cdn-zcode.z.ai"
	ZCodeFeedBase   = "https://" + ZCodeUpdateHost + "/zcode/electron/releases/update/"
)

// zcodeFeed is the electron-updater manifest. Only the fields OneAgent acts on
// are decoded; the feed also carries release notes in several locales.
//
// Files is what gets used, not Path: on macOS the feed lists both a .zip and a
// .dmg, and the .dmg's published digest is wrong (see zcodeArtifact).
type zcodeFeed struct {
	Version string          `yaml:"version"`
	Path    string          `yaml:"path"`
	SHA512  string          `yaml:"sha512"`
	Files   []zcodeFeedFile `yaml:"files"`
}

type zcodeFeedFile struct {
	URL    string `yaml:"url"`
	SHA512 string `yaml:"sha512"`
	Size   int64  `yaml:"size"`
}

// inspectZCode reports whether ZCode is installed.
func inspectZCode(ctx context.Context, options Options) Status {
	status := baseZCodeStatus(options.Platform.OS)
	if err := contextError(ctx); err != nil {
		message := err.Error()
		status.InspectionUnavailable = &message
		return status
	}
	switch options.Platform.OS {
	case "macos":
		found, err := inspectZCodeMacOS(ctx, options)
		if err != nil {
			message := err.Error()
			found.InspectionUnavailable = &message
		}
		return found
	case "windows":
		found, err := inspectZCodeWindows(options)
		if err != nil {
			message := err.Error()
			found.InspectionUnavailable = &message
		}
		return found
	}
	return status
}

// baseZCodeStatus marks the platforms ZCode ships for. The vendor's feed also
// carries linux builds, but OneAgent installs only where it can verify the
// package: macOS through codesign and Windows through Authenticode.
func baseZCodeStatus(osID string) Status {
	status := Status{ID: ZCodeID, Name: ZCodeName, Source: SourceUnknown}
	switch osID {
	case "macos":
		status.Supported, status.Source = true, SourceMacOSZIP
	case "windows":
		status.Supported, status.Source = true, SourceWindowsInstaller
	}
	return status
}

func inspectZCodeMacOS(ctx context.Context, options Options) (Status, error) {
	status := baseZCodeStatus("macos")
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
			candidate = filepath.Join(root, "ZCode.app")
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
		// The bundle identifier is read from the plist, so a renamed .app cannot
		// pass as ZCode.
		if metadata.bundleID != ZCodeBundleID {
			continue
		}
		status.Installed, status.Path, status.Version = true, candidate, metadata.version
		return status, nil
	}
	return status, lastErr
}

func inspectZCodeWindows(options Options) (Status, error) {
	status := baseZCodeStatus("windows")
	for _, candidate := range zcodeWindowsCandidates(options) {
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

// zcodeWindowsCandidates lists the per-user and machine-wide install locations an
// Electron app of this kind uses. Not verified on Windows: this machine is macOS,
// so the paths follow the same shape as the WorkBuddy lookup rather than an
// observed installation.
func zcodeWindowsCandidates(options Options) []string {
	if len(options.SearchRoots) > 0 {
		candidates := make([]string, 0, len(options.SearchRoots))
		for _, root := range options.SearchRoots {
			if strings.EqualFold(filepath.Ext(root), ".exe") {
				candidates = append(candidates, root)
				continue
			}
			candidates = append(candidates, filepath.Join(root, "ZCode.exe"))
		}
		return candidates
	}
	candidates := make([]string, 0, 3)
	if options.Home != "" {
		candidates = append(candidates,
			filepath.Join(options.Home, "AppData", "Local", "Programs", "ZCode", "ZCode.exe"),
			filepath.Join(options.Home, "AppData", "Local", "ZCode", "ZCode.exe"),
		)
	}
	return append(candidates, filepath.Join("C:\\", "Program Files", "ZCode", "ZCode.exe"))
}

// zcodeFeedURL names the electron-updater manifest for a platform and
// architecture. The layout is the vendor's: macOS keeps both architectures under
// latest-mac.yml, Windows under latest.yml.
func zcodeFeedURL(osID, arch string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "arm64", "aarch64":
		arch = "arm64"
	case "x64", "amd64", "x86_64":
		arch = "x64"
	default:
		return "", fmt.Errorf("%s has no package for %s/%s", ZCodeName, osID, arch)
	}
	switch osID {
	case "macos":
		return ZCodeFeedBase + "mac/" + arch + "/latest-mac.yml", nil
	case "windows":
		return ZCodeFeedBase + "win/" + arch + "/latest.yml", nil
	}
	return "", fmt.Errorf("%s has no package for %s/%s", ZCodeName, osID, arch)
}

// fetchZCodeFeed reads the update manifest. The feed is the rolling pointer to
// the current release, so it is requested with no-cache: the vendor serves it
// with a year-long max-age, which would otherwise let an intermediary pin
// OneAgent to a stale version.
func fetchZCodeFeed(ctx context.Context, options Options, osID, arch string) (zcodeFeed, error) {
	endpoint, err := zcodeFeedURL(osID, arch)
	if err != nil {
		return zcodeFeed{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return zcodeFeed{}, err
	}
	request.Header.Set("Cache-Control", "no-cache")
	client := options.Downloader
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return zcodeFeed{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return zcodeFeed{}, fmt.Errorf("%s update request returned HTTP %d", ZCodeName, response.StatusCode)
	}
	var feed zcodeFeed
	if err := yaml.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&feed); err != nil {
		return zcodeFeed{}, fmt.Errorf("decode %s update response: %w", ZCodeName, err)
	}
	return feed, nil
}

// zcodeArtifact picks the file to download and returns it with its expected
// digest.
//
// On macOS this deliberately requires the .zip. The feed lists a .dmg too, but
// its published size and sha512 describe the archive *before* the vendor staples
// the notarization ticket, so the served bytes are ~11.7 KB longer and the digest
// can never match. electron-updater itself consumes the zip -- the feed's own
// top-level `path` points at it -- which is why the discrepancy is unnoticed
// upstream. Falling back to the dmg would trade a verified install for an
// unverified one, so an absent zip is an error instead.
func zcodeArtifact(feed zcodeFeed, osID string) (zcodeFeedFile, error) {
	wanted := ".exe"
	if osID == "macos" {
		wanted = ".zip"
	}
	for _, file := range feed.Files {
		if !strings.EqualFold(filepath.Ext(mustParseURLPath(file.URL)), wanted) {
			continue
		}
		approved, err := approvedDownloadURL(file.URL, ZCodeUpdateHost)
		if err != nil {
			return zcodeFeedFile{}, fmt.Errorf("validate %s package URL: %w", ZCodeName, err)
		}
		if strings.TrimSpace(file.SHA512) == "" {
			return zcodeFeedFile{}, fmt.Errorf("%s update feed lists no digest for %s", ZCodeName, wanted)
		}
		file.URL = approved
		return file, nil
	}
	return zcodeFeedFile{}, fmt.Errorf("%s update feed lists no %s package", ZCodeName, wanted)
}

// verifyZCodeDigest checks the downloaded bytes against the feed. The digest is
// what authenticates the archive: the host allowlist only says who we asked.
// Size is checked first because it makes a truncated transfer report the useful
// error rather than an opaque digest mismatch.
func verifyZCodeDigest(path string, expected zcodeFeedFile) error {
	want, err := base64.StdEncoding.DecodeString(strings.TrimSpace(expected.SHA512))
	if err != nil {
		return fmt.Errorf("decode expected %s digest: %w", ZCodeName, err)
	}
	if len(want) != sha512.Size {
		return fmt.Errorf("expected %s digest is not a SHA-512 value", ZCodeName)
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
		return fmt.Errorf("downloaded %s package is %d bytes, expected %d", ZCodeName, info.Size(), expected.Size)
	}
	digest := sha512.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(digest.Sum(nil), want) != 1 {
		return fmt.Errorf("downloaded %s package failed its SHA-512 check", ZCodeName)
	}
	return nil
}

func installZCode(ctx context.Context, options Options) (ActionResult, error) {
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	status := inspectZCode(ctx, options)
	if status.Installed {
		return ActionResult{Status: "already-installed", Message: ZCodeName + " is already installed", App: status}, nil
	}
	switch options.Platform.OS {
	case "macos":
		return installZCodeMacOS(ctx, options)
	case "windows":
		return installZCodeWindows(ctx, options)
	}
	return ActionResult{}, fmt.Errorf("%s is not supported on %s", ZCodeName, options.Platform.OS)
}

// zcodePackage resolves what to download. DownloadURL is the test and
// self-hosting seam the other agents use; it still has to pass the host
// allowlist, and it carries no digest, so it verifies by signature alone.
func zcodePackage(ctx context.Context, options Options) (zcodeFeed, zcodeFeedFile, error) {
	if raw := strings.TrimSpace(options.DownloadURL); raw != "" {
		approved, err := approvedDownloadURL(raw, ZCodeUpdateHost)
		return zcodeFeed{}, zcodeFeedFile{URL: approved}, err
	}
	feed, err := fetchZCodeFeed(ctx, options, options.Platform.OS, options.Platform.Arch)
	if err != nil {
		return zcodeFeed{}, zcodeFeedFile{}, err
	}
	artifact, err := zcodeArtifact(feed, options.Platform.OS)
	if err != nil {
		return zcodeFeed{}, zcodeFeedFile{}, err
	}
	return feed, artifact, nil
}

func installZCodeMacOS(ctx context.Context, options Options) (ActionResult, error) {
	feed, artifact, err := zcodePackage(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	tempDir, err := os.MkdirTemp("", "oneagent-zcode-")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary %s installer directory: %w", ZCodeName, err)
	}
	defer os.RemoveAll(tempDir)
	archive := filepath.Join(tempDir, "ZCode.zip")
	if err := downloadFile(ctx, options, artifact.URL, archive, ZCodeID); err != nil {
		return ActionResult{}, fmt.Errorf("download %s installer: %w", ZCodeName, err)
	}
	// Only when the feed supplied one. A DownloadURL override has no manifest to
	// compare against, and signature verification below is what gates it.
	if artifact.SHA512 != "" {
		if err := verifyZCodeDigest(archive, artifact); err != nil {
			return ActionResult{}, err
		}
	}
	extracted := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extracted, 0o700); err != nil {
		return ActionResult{}, err
	}
	result, err := run(options, ctx, []string{"/usr/bin/ditto", "-x", "-k", archive, extracted}, installTimeout)
	if err != nil {
		return ActionResult{}, fmt.Errorf("extract %s installer: %w", ZCodeName, err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("extract "+ZCodeName+" installer", result)
	}
	appPath, err := findZCodeApp(extracted)
	if err != nil {
		return ActionResult{}, err
	}
	metadata, err := readMacMetadata(ctx, options, appPath)
	if err != nil {
		return ActionResult{}, fmt.Errorf("inspect downloaded %s app: %w", ZCodeName, err)
	}
	if metadata.bundleID != ZCodeBundleID {
		return ActionResult{}, fmt.Errorf("downloaded app has unexpected bundle identifier %q", metadata.bundleID)
	}
	if err := verifyZCodeMacOSApp(ctx, options, appPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded %s app: %w", ZCodeName, err)
	}
	destinations := zcodeDestinations(options)
	var lastErr error
	for _, destination := range destinations {
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
			lastErr = commandFailure("copy "+ZCodeName+" app", copied)
			continue
		}
		installed := baseZCodeStatus("macos")
		installed.Installed, installed.Path = true, destination
		installed.Version = metadata.version
		if installed.Version == nil {
			installed.Version = nonEmptyPointer(feed.Version)
		}
		return ActionResult{Status: "installed", Message: ZCodeName + " was installed", RefreshNeeded: true, App: installed}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no writable macOS Applications directory")
	}
	return ActionResult{}, lastErr
}

// installZCodeWindows starts the vendor's own installer. OneAgent verifies
// Authenticode first and then hands over, the same as WorkBuddy: the .exe is an
// NSIS installer that owns its own placement and shortcuts.
func installZCodeWindows(ctx context.Context, options Options) (ActionResult, error) {
	feed, artifact, err := zcodePackage(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	installer, err := os.CreateTemp("", "oneagent-zcode-*.exe")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary %s installer: %w", ZCodeName, err)
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
	if err := downloadFile(ctx, options, artifact.URL, installerPath, ZCodeID); err != nil {
		return ActionResult{}, fmt.Errorf("download %s installer: %w", ZCodeName, err)
	}
	if artifact.SHA512 != "" {
		if err := verifyZCodeDigest(installerPath, artifact); err != nil {
			return ActionResult{}, err
		}
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	if err := verifyZCodeWindowsInstaller(ctx, options, installerPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded %s installer with Authenticode: %w", ZCodeName, err)
	}
	if err := start(options, []string{installerPath}); err != nil {
		return ActionResult{}, fmt.Errorf("start %s installer: %w", ZCodeName, err)
	}
	keep = true
	status := baseZCodeStatus("windows")
	status.Version = nonEmptyPointer(feed.Version)
	return ActionResult{Status: "installer-started", Message: "The downloaded " + ZCodeName + " installer was started", RefreshNeeded: true, App: status}, nil
}

func zcodeDestinations(options Options) []string {
	dirs := options.ApplicationDirs
	if len(dirs) == 0 {
		dirs = []string{"/Applications"}
		if options.Home != "" {
			dirs = append(dirs, filepath.Join(options.Home, "Applications"))
		}
	}
	result := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		result = append(result, filepath.Join(dir, "ZCode.app"))
	}
	return result
}

func findZCodeApp(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.EqualFold(filepath.Base(path), "ZCode.app") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("downloaded %s archive contains no ZCode.app", ZCodeName)
	}
	return found, nil
}

func openZCode(ctx context.Context, options Options) error {
	status := inspectZCode(ctx, options)
	if !status.Installed {
		return errors.New(ZCodeName + " is not installed")
	}
	switch options.Platform.OS {
	case "macos":
		return start(options, []string{"/usr/bin/open", "-a", status.Path})
	case "windows":
		return start(options, []string{status.Path})
	}
	return fmt.Errorf("%s is not supported on %s", ZCodeName, options.Platform.OS)
}
