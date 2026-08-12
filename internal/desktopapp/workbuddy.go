package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	WorkBuddyID              = "workbuddy"
	WorkBuddyName            = "WorkBuddy"
	WorkBuddyBundleID        = "com.workbuddy.workbuddy"
	WorkBuddyUpdateEndpoint  = "https://www.workbuddy.cn/v2/update"
	WorkBuddyDownloadHost    = "download.codebuddy.cn"
	WorkBuddyWindowsPlatform = "workbuddy-win32-x64-user"
	SourceMacOSZIP           = "macos-zip"
	SourceWindowsInstaller   = "windows-installer"

	// The international edition is a separate build, not a locale switch: it has
	// its own bundle identifier, app name, update backend, download host and
	// config directory. Only the models.json schema is shared, which is why the
	// config writer is reused as-is.
	WorkBuddyIntlID             = "workbuddy-intl"
	WorkBuddyIntlName           = "WorkBuddy AI"
	WorkBuddyIntlBundleID       = "com.workbuddy.workbuddy-ai"
	WorkBuddyIntlUpdateEndpoint = "https://www.codebuddy.ai/v2/update"
	// The international artifacts are served straight from the vendor's COS
	// bucket rather than from a branded download host.
	WorkBuddyIntlDownloadHost = "codebuddy-1328495429.cos.accelerate.myqcloud.com"
	// WorkBuddyIntlConfigDir is the directory the shipped build actually reads.
	// The vendor's English documentation says ~/.codebuddy, which is wrong: the
	// resolver takes `config.customUserDataDir` from cli/product.json, and the
	// international build sets it to this value. `.codebuddy` is the branch taken
	// only when the product name does not contain "workbuddy", so no international
	// code path reaches it -- writing there would be a silent no-op.
	WorkBuddyIntlConfigDir = ".workbuddy-ai"
)

type workBuddyUpdate struct {
	Version        string `json:"version"`
	URL            string `json:"url"`
	ProductVersion string `json:"productVersion"`
}

// workBuddyEdition holds everything that differs between the Chinese and
// international builds. Both are inspected, installed and launched by the same
// code paths; only these values change.
type workBuddyEdition struct {
	id             string
	name           string
	bundleID       string
	appName        string
	executableName string
	updateEndpoint string
	updateHost     string
	downloadHost   string
	macTeamID      string
	// windowsSigners lists the Authenticode subjects accepted for this edition's
	// installer.
	windowsSigners []string
}

var (
	workBuddyCN = workBuddyEdition{
		id:             WorkBuddyID,
		name:           WorkBuddyName,
		bundleID:       WorkBuddyBundleID,
		appName:        "WorkBuddy.app",
		executableName: "WorkBuddy.exe",
		updateEndpoint: WorkBuddyUpdateEndpoint,
		updateHost:     "www.workbuddy.cn",
		downloadHost:   WorkBuddyDownloadHost,
		macTeamID:      WorkBuddyMacTeamID,
		windowsSigners: []string{
			"Tencent Technology (Shenzhen) Company Limited",
			"Shenzhen Tencent Computer Systems Company Limited",
		},
	}
	workBuddyIntl = workBuddyEdition{
		id:             WorkBuddyIntlID,
		name:           WorkBuddyIntlName,
		bundleID:       WorkBuddyIntlBundleID,
		appName:        "WorkBuddy AI.app",
		executableName: "WorkBuddy AI.exe",
		updateEndpoint: WorkBuddyIntlUpdateEndpoint,
		updateHost:     "www.codebuddy.ai",
		downloadHost:   WorkBuddyIntlDownloadHost,
		// Same vendor, so the same Developer ID team signs both builds. Verified
		// against the installed Chinese build; the international package is signed
		// by the same team.
		macTeamID: WorkBuddyMacTeamID,
		windowsSigners: []string{
			"Tencent Technology (Shenzhen) Company Limited",
			"Shenzhen Tencent Computer Systems Company Limited",
		},
	}
)

func workBuddyEditionFor(agentID string) workBuddyEdition {
	if strings.TrimSpace(agentID) == WorkBuddyIntlID {
		return workBuddyIntl
	}
	return workBuddyCN
}

func inspectWorkBuddy(edition workBuddyEdition) func(context.Context, Options) Status {
	return func(ctx context.Context, options Options) Status {
		status := baseWorkBuddyStatus(edition, options.Platform.OS)
		if err := contextError(ctx); err != nil {
			message := err.Error()
			status.InspectionUnavailable = &message
			return status
		}
		var inspected Status
		var err error
		switch options.Platform.OS {
		case "macos":
			inspected, err = inspectWorkBuddyMacOS(ctx, edition, options)
		case "windows":
			inspected, err = inspectWorkBuddyWindows(ctx, edition, options)
		default:
			return status
		}
		if err != nil {
			message := err.Error()
			status.InspectionUnavailable = &message
			return status
		}
		return inspected
	}
}

func baseWorkBuddyStatus(edition workBuddyEdition, osID string) Status {
	status := Status{ID: edition.id, Name: edition.name, Source: SourceUnknown}
	switch osID {
	case "macos":
		status.Supported, status.Source = true, SourceMacOSZIP
	case "windows":
		status.Supported, status.Source = true, SourceWindowsInstaller
	}
	return status
}

func inspectWorkBuddyMacOS(ctx context.Context, edition workBuddyEdition, options Options) (Status, error) {
	status := baseWorkBuddyStatus(edition, "macos")
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
			candidate = filepath.Join(root, edition.appName)
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
		// The bundle identifier is what separates the two editions: their version
		// strings are near-identical (both 5.3.11.x), so a version check could not
		// tell them apart, and a SearchRoots entry pointing straight at an .app
		// bypasses the name check above.
		if metadata.bundleID != edition.bundleID {
			continue
		}
		status.Installed, status.Path, status.Version = true, candidate, metadata.version
		return status, nil
	}
	return status, lastErr
}

func inspectWorkBuddyWindows(ctx context.Context, edition workBuddyEdition, options Options) (Status, error) {
	status := baseWorkBuddyStatus(edition, "windows")
	for _, candidate := range workBuddyWindowsCandidates(edition, options) {
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return status, err
		}
		if !info.IsDir() {
			status.Installed, status.Path = true, candidate
			status.Version = workBuddyWindowsVersion(ctx, options, candidate)
			return status, nil
		}
	}
	result, err := run(options, ctx, workBuddyStartAppsQuery(edition), inspectTimeout)
	if err != nil {
		return status, err
	}
	if result.ExitCode != 0 {
		return status, commandFailure("query installed WorkBuddy app", result)
	}
	var registered struct {
		AppID string `json:"AppID"`
	}
	text := strings.TrimPrefix(strings.TrimSpace(result.Stdout), "\ufeff")
	if text == "" || json.Unmarshal([]byte(text), &registered) != nil || strings.TrimSpace(registered.AppID) == "" {
		return status, nil
	}
	status.Installed = true
	status.PackageFamily = strings.TrimSpace(registered.AppID)
	message := "Windows registered " + edition.name + ", but its install path and version were unavailable"
	status.InspectionUnavailable = &message
	return status, nil
}

// workBuddyWindowsCandidates lists the per-user and machine-wide install
// locations. The international directory and executable names were not observed
// on Windows -- this machine is macOS -- so they follow the macOS product name,
// which is how the Chinese build's paths relate to its own.
func workBuddyWindowsCandidates(edition workBuddyEdition, options Options) []string {
	folder := strings.TrimSuffix(edition.appName, ".app")
	if len(options.SearchRoots) > 0 {
		result := make([]string, 0, len(options.SearchRoots)*3)
		for _, root := range options.SearchRoots {
			if strings.EqualFold(filepath.Ext(root), ".exe") {
				result = append(result, root)
				continue
			}
			result = append(result,
				filepath.Join(root, edition.executableName),
				filepath.Join(root, folder, edition.executableName),
				filepath.Join(root, "Programs", folder, edition.executableName),
			)
		}
		return result
	}
	var result []string
	if options.Home != "" {
		local := filepath.Join(options.Home, "AppData", "Local")
		result = append(result,
			filepath.Join(local, "Programs", folder, edition.executableName),
			filepath.Join(local, folder, edition.executableName),
		)
	}
	if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
		result = append(result, filepath.Join(programFiles, folder, edition.executableName))
	}
	return result
}

func workBuddyWindowsVersion(ctx context.Context, options Options, path string) *string {
	const key = "ONEAGENT_WORKBUDDY_PATH"
	argv := []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `(Get-Item -LiteralPath $env:ONEAGENT_WORKBUDDY_PATH).VersionInfo.ProductVersion`}
	result, err := runWithEnvironment(options, ctx, argv, map[string]string{key: path}, inspectTimeout)
	if err != nil || result.ExitCode != 0 {
		return nil
	}
	return nonEmptyPointer(strings.TrimSpace(result.Stdout))
}

// workBuddyStartAppsQuery matches on the Start menu display name, which carries
// no ".app" suffix.
func workBuddyStartAppsQuery(edition workBuddyEdition) []string {
	name := strings.TrimSuffix(edition.appName, ".app")
	script := `Get-StartApps | Where-Object { $_.Name -eq '` + name + `' } | Select-Object -First 1 Name,AppID | ConvertTo-Json -Compress`
	return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script}
}

func installWorkBuddy(edition workBuddyEdition) func(context.Context, Options) (ActionResult, error) {
	return func(ctx context.Context, options Options) (ActionResult, error) {
		status := inspectWorkBuddy(edition)(ctx, options)
		if err := contextError(ctx); err != nil {
			return ActionResult{}, err
		}
		if status.Installed {
			return ActionResult{Status: "already-installed", Message: edition.name + " is already installed", App: status}, nil
		}
		switch options.Platform.OS {
		case "macos":
			return installWorkBuddyMacOS(ctx, edition, options, "")
		case "windows":
			return installWorkBuddyWindows(ctx, edition, options, status)
		default:
			return ActionResult{}, fmt.Errorf("%s is not supported on %s", edition.name, options.Platform.OS)
		}
	}
}

func openWorkBuddy(edition workBuddyEdition) func(context.Context, Options) error {
	return func(ctx context.Context, options Options) error {
		status := inspectWorkBuddy(edition)(ctx, options)
		if !status.Installed {
			if status.InspectionUnavailable != nil {
				return errors.New(*status.InspectionUnavailable)
			}
			return errors.New(edition.name + " is not installed")
		}
		switch options.Platform.OS {
		case "macos":
			if status.Path == "" {
				return errors.New(edition.name + " path is unavailable")
			}
			return start(options, []string{"/usr/bin/open", "-a", status.Path})
		case "windows":
			if status.Path != "" {
				return start(options, []string{status.Path})
			}
			if status.PackageFamily != "" {
				return start(options, []string{"explorer.exe", "shell:AppsFolder\\" + status.PackageFamily})
			}
			return errors.New(edition.name + " launch target is unavailable")
		default:
			return fmt.Errorf("%s is not supported on %s", edition.name, options.Platform.OS)
		}
	}
}

func workBuddyPlatform(osID, arch string) (string, error) {
	arch = strings.ToLower(strings.TrimSpace(arch))
	switch osID {
	case "macos":
		switch arch {
		case "arm64", "aarch64":
			return "workbuddy-darwin-arm64", nil
		case "x64", "amd64", "x86_64":
			return "workbuddy-darwin-x64", nil
		}
	case "windows":
		switch arch {
		case "arm64", "aarch64", "x64", "amd64", "x86_64":
			return WorkBuddyWindowsPlatform, nil
		}
	}
	return "", fmt.Errorf("WorkBuddy has no package for %s/%s", osID, arch)
}

func fetchWorkBuddyUpdate(ctx context.Context, edition workBuddyEdition, options Options) (workBuddyUpdate, error) {
	platformID, err := workBuddyPlatform(options.Platform.OS, options.Platform.Arch)
	if err != nil {
		return workBuddyUpdate{}, err
	}
	endpoint, err := approvedDownloadURL(edition.updateEndpoint, edition.updateHost)
	if err != nil {
		return workBuddyUpdate{}, fmt.Errorf("validate %s update endpoint: %w", edition.name, err)
	}
	parsed, _ := url.Parse(endpoint)
	query := parsed.Query()
	query.Set("platform", platformID)
	parsed.RawQuery = query.Encode()
	requestCtx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return workBuddyUpdate{}, err
	}
	client := options.Downloader
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return workBuddyUpdate{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return workBuddyUpdate{}, fmt.Errorf("%s update request returned HTTP %d", edition.name, response.StatusCode)
	}
	var update workBuddyUpdate
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&update); err != nil {
		return workBuddyUpdate{}, fmt.Errorf("decode %s update response: %w", edition.name, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return workBuddyUpdate{}, errors.New(edition.name + " update response contains multiple JSON values")
	}
	update.URL, err = approvedDownloadURL(update.URL, edition.downloadHost)
	if err != nil {
		return workBuddyUpdate{}, fmt.Errorf("validate %s installer URL: %w", edition.name, err)
	}
	return update, nil
}

func workBuddyPackage(ctx context.Context, edition workBuddyEdition, options Options) (workBuddyUpdate, error) {
	if raw := strings.TrimSpace(options.DownloadURL); raw != "" {
		approved, err := approvedDownloadURL(raw, edition.downloadHost)
		return workBuddyUpdate{URL: approved}, err
	}
	return fetchWorkBuddyUpdate(ctx, edition, options)
}

func installWorkBuddyMacOS(ctx context.Context, edition workBuddyEdition, options Options, replacePath string) (ActionResult, error) {
	update, err := workBuddyPackage(ctx, edition, options)
	if err != nil {
		return ActionResult{}, err
	}
	if !strings.EqualFold(filepath.Ext(mustParseURLPath(update.URL)), ".zip") {
		return ActionResult{}, errors.New(edition.name + " macOS installer is not a zip archive")
	}
	tempDir, err := os.MkdirTemp("", "oneagent-workbuddy-")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary %s installer directory: %w", edition.name, err)
	}
	defer os.RemoveAll(tempDir)
	archive := filepath.Join(tempDir, "WorkBuddy.zip")
	if err := downloadFile(ctx, options, update.URL, archive, edition.id); err != nil {
		return ActionResult{}, fmt.Errorf("download %s installer: %w", edition.name, err)
	}
	extracted := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extracted, 0o700); err != nil {
		return ActionResult{}, err
	}
	result, err := run(options, ctx, []string{"/usr/bin/ditto", "-x", "-k", archive, extracted}, installTimeout)
	if err != nil {
		return ActionResult{}, fmt.Errorf("extract %s installer: %w", edition.name, err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("extract "+edition.name+" installer", result)
	}
	appPath, err := findWorkBuddyApp(edition, extracted)
	if err != nil {
		return ActionResult{}, err
	}
	metadata, err := readMacMetadata(ctx, options, appPath)
	if err != nil {
		return ActionResult{}, fmt.Errorf("inspect downloaded %s app: %w", edition.name, err)
	}
	if metadata.bundleID != edition.bundleID {
		return ActionResult{}, fmt.Errorf("downloaded app has unexpected bundle identifier %q", metadata.bundleID)
	}
	if err := verifyWorkBuddyMacOSApp(ctx, edition, options, appPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded %s app: %w", edition.name, err)
	}
	destinations := workBuddyDestinations(edition, options, replacePath)
	var lastErr error
	for _, destination := range destinations {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			lastErr = err
			continue
		}
		if _, err := os.Stat(destination); err == nil && replacePath == "" {
			lastErr = fmt.Errorf("destination already exists: %s", destination)
			continue
		} else if err != nil && !os.IsNotExist(err) {
			lastErr = err
			continue
		}
		copied, copyErr := run(options, ctx, []string{"/usr/bin/ditto", appPath, destination}, installTimeout)
		if copyErr != nil {
			lastErr = copyErr
			continue
		}
		if copied.ExitCode != 0 {
			lastErr = commandFailure("copy "+edition.name+" app", copied)
			continue
		}
		installed := baseWorkBuddyStatus(edition, "macos")
		installed.Installed, installed.Path = true, destination
		installed.Version = metadata.version
		if installed.Version == nil {
			installed.Version = nonEmptyPointer(workBuddyVersion(update))
		}
		return ActionResult{Status: "installed", Message: edition.name + " was installed", RefreshNeeded: true, App: installed}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no writable macOS Applications directory")
	}
	return ActionResult{}, lastErr
}

func workBuddyDestinations(edition workBuddyEdition, options Options, replacePath string) []string {
	if replacePath != "" {
		return []string{filepath.Clean(replacePath)}
	}
	dirs := options.ApplicationDirs
	if len(dirs) == 0 {
		dirs = []string{"/Applications"}
		if options.Home != "" {
			dirs = append(dirs, filepath.Join(options.Home, "Applications"))
		}
	}
	result := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		result = append(result, filepath.Join(dir, edition.appName))
	}
	return result
}

func findWorkBuddyApp(edition workBuddyEdition, root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.EqualFold(entry.Name(), edition.appName) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("inspect extracted %s installer: %w", edition.name, err)
	}
	if found == "" {
		return "", errors.New("no " + edition.appName + " bundle found in installer")
	}
	return found, nil
}

func installWorkBuddyWindows(ctx context.Context, edition workBuddyEdition, options Options, status Status) (ActionResult, error) {
	update, err := workBuddyPackage(ctx, edition, options)
	if err != nil {
		return ActionResult{}, err
	}
	if !strings.EqualFold(filepath.Ext(mustParseURLPath(update.URL)), ".exe") {
		return ActionResult{}, errors.New(edition.name + " Windows installer is not an executable")
	}
	installer, err := os.CreateTemp("", "oneagent-workbuddy-*.exe")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary %s installer: %w", edition.name, err)
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
	if err := downloadFile(ctx, options, update.URL, installerPath, edition.id); err != nil {
		return ActionResult{}, fmt.Errorf("download %s installer: %w", edition.name, err)
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	if err := verifyWorkBuddyWindowsInstaller(ctx, edition, options, installerPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded %s installer with Authenticode: %w", edition.name, err)
	}
	if err := start(options, []string{installerPath}); err != nil {
		return ActionResult{}, fmt.Errorf("start %s installer: %w", edition.name, err)
	}
	keep = true
	status.Source = SourceWindowsInstaller
	if status.Version == nil {
		status.Version = nonEmptyPointer(workBuddyVersion(update))
	}
	return ActionResult{Status: "installer-started", Message: "The downloaded " + edition.name + " installer was started", RefreshNeeded: true, App: status}, nil
}

func workBuddyVersion(update workBuddyUpdate) string {
	if value := strings.TrimSpace(update.ProductVersion); value != "" {
		return value
	}
	return strings.TrimSpace(update.Version)
}

func mustParseURLPath(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Path
}
