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
)

type workBuddyUpdate struct {
	Version        string `json:"version"`
	URL            string `json:"url"`
	ProductVersion string `json:"productVersion"`
}

func inspectWorkBuddy(ctx context.Context, options Options) Status {
	status := baseWorkBuddyStatus(options.Platform.OS)
	if err := contextError(ctx); err != nil {
		message := err.Error()
		status.InspectionUnavailable = &message
		return status
	}
	var inspected Status
	var err error
	switch options.Platform.OS {
	case "macos":
		inspected, err = inspectWorkBuddyMacOS(ctx, options)
	case "windows":
		inspected, err = inspectWorkBuddyWindows(ctx, options)
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

func baseWorkBuddyStatus(osID string) Status {
	status := Status{ID: WorkBuddyID, Name: WorkBuddyName, Source: SourceUnknown}
	switch osID {
	case "macos":
		status.Supported, status.Source = true, SourceMacOSZIP
	case "windows":
		status.Supported, status.Source = true, SourceWindowsInstaller
	}
	return status
}

func inspectWorkBuddyMacOS(ctx context.Context, options Options) (Status, error) {
	status := baseWorkBuddyStatus("macos")
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
			candidate = filepath.Join(root, "WorkBuddy.app")
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
		if metadata.bundleID != WorkBuddyBundleID {
			continue
		}
		status.Installed, status.Path, status.Version = true, candidate, metadata.version
		return status, nil
	}
	return status, lastErr
}

func inspectWorkBuddyWindows(ctx context.Context, options Options) (Status, error) {
	status := baseWorkBuddyStatus("windows")
	for _, candidate := range workBuddyWindowsCandidates(options) {
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
	result, err := run(options, ctx, workBuddyStartAppsQuery(), inspectTimeout)
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
	message := "Windows registered WorkBuddy, but its install path and version were unavailable"
	status.InspectionUnavailable = &message
	return status, nil
}

func workBuddyWindowsCandidates(options Options) []string {
	if len(options.SearchRoots) > 0 {
		result := make([]string, 0, len(options.SearchRoots)*3)
		for _, root := range options.SearchRoots {
			if strings.EqualFold(filepath.Ext(root), ".exe") {
				result = append(result, root)
				continue
			}
			result = append(result,
				filepath.Join(root, "WorkBuddy.exe"),
				filepath.Join(root, "WorkBuddy", "WorkBuddy.exe"),
				filepath.Join(root, "Programs", "WorkBuddy", "WorkBuddy.exe"),
			)
		}
		return result
	}
	var result []string
	if options.Home != "" {
		local := filepath.Join(options.Home, "AppData", "Local")
		result = append(result,
			filepath.Join(local, "Programs", "WorkBuddy", "WorkBuddy.exe"),
			filepath.Join(local, "WorkBuddy", "WorkBuddy.exe"),
		)
	}
	if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
		result = append(result, filepath.Join(programFiles, "WorkBuddy", "WorkBuddy.exe"))
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

func workBuddyStartAppsQuery() []string {
	script := `Get-StartApps | Where-Object { $_.Name -eq 'WorkBuddy' } | Select-Object -First 1 Name,AppID | ConvertTo-Json -Compress`
	return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script}
}

func installWorkBuddy(ctx context.Context, options Options, update bool) (ActionResult, error) {
	status := inspectWorkBuddy(ctx, options)
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	if status.Installed && !update {
		return ActionResult{Status: "already-installed", Message: "WorkBuddy is already installed", App: status}, nil
	}
	switch options.Platform.OS {
	case "macos":
		replacePath := ""
		if update {
			replacePath = status.Path
		}
		return installWorkBuddyMacOS(ctx, options, replacePath)
	case "windows":
		return installWorkBuddyWindows(ctx, options, status)
	default:
		return ActionResult{}, fmt.Errorf("WorkBuddy is not supported on %s", options.Platform.OS)
	}
}

func openWorkBuddy(ctx context.Context, options Options) error {
	status := inspectWorkBuddy(ctx, options)
	if !status.Installed {
		if status.InspectionUnavailable != nil {
			return errors.New(*status.InspectionUnavailable)
		}
		return errors.New("WorkBuddy is not installed")
	}
	switch options.Platform.OS {
	case "macos":
		if status.Path == "" {
			return errors.New("WorkBuddy path is unavailable")
		}
		return start(options, []string{"/usr/bin/open", "-a", status.Path})
	case "windows":
		if status.Path != "" {
			return start(options, []string{status.Path})
		}
		if status.PackageFamily != "" {
			return start(options, []string{"explorer.exe", "shell:AppsFolder\\" + status.PackageFamily})
		}
		return errors.New("WorkBuddy launch target is unavailable")
	default:
		return fmt.Errorf("WorkBuddy is not supported on %s", options.Platform.OS)
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

func fetchWorkBuddyUpdate(ctx context.Context, options Options) (workBuddyUpdate, error) {
	platformID, err := workBuddyPlatform(options.Platform.OS, options.Platform.Arch)
	if err != nil {
		return workBuddyUpdate{}, err
	}
	endpoint, err := approvedDownloadURL(WorkBuddyUpdateEndpoint, "www.workbuddy.cn")
	if err != nil {
		return workBuddyUpdate{}, fmt.Errorf("validate WorkBuddy update endpoint: %w", err)
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
		return workBuddyUpdate{}, fmt.Errorf("WorkBuddy update request returned HTTP %d", response.StatusCode)
	}
	var update workBuddyUpdate
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&update); err != nil {
		return workBuddyUpdate{}, fmt.Errorf("decode WorkBuddy update response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return workBuddyUpdate{}, errors.New("WorkBuddy update response contains multiple JSON values")
	}
	update.URL, err = approvedDownloadURL(update.URL, WorkBuddyDownloadHost)
	if err != nil {
		return workBuddyUpdate{}, fmt.Errorf("validate WorkBuddy installer URL: %w", err)
	}
	return update, nil
}

func workBuddyPackage(ctx context.Context, options Options) (workBuddyUpdate, error) {
	if raw := strings.TrimSpace(options.DownloadURL); raw != "" {
		approved, err := approvedDownloadURL(raw, WorkBuddyDownloadHost)
		return workBuddyUpdate{URL: approved}, err
	}
	return fetchWorkBuddyUpdate(ctx, options)
}

func installWorkBuddyMacOS(ctx context.Context, options Options, replacePath string) (ActionResult, error) {
	update, err := workBuddyPackage(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	if !strings.EqualFold(filepath.Ext(mustParseURLPath(update.URL)), ".zip") {
		return ActionResult{}, errors.New("WorkBuddy macOS installer is not a zip archive")
	}
	tempDir, err := os.MkdirTemp("", "oneagent-workbuddy-")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary WorkBuddy installer directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	archive := filepath.Join(tempDir, "WorkBuddy.zip")
	if err := downloadFile(ctx, options, update.URL, archive, WorkBuddyID); err != nil {
		return ActionResult{}, fmt.Errorf("download WorkBuddy installer: %w", err)
	}
	extracted := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extracted, 0o700); err != nil {
		return ActionResult{}, err
	}
	result, err := run(options, ctx, []string{"/usr/bin/ditto", "-x", "-k", archive, extracted}, installTimeout)
	if err != nil {
		return ActionResult{}, fmt.Errorf("extract WorkBuddy installer: %w", err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("extract WorkBuddy installer", result)
	}
	appPath, err := findWorkBuddyApp(extracted)
	if err != nil {
		return ActionResult{}, err
	}
	metadata, err := readMacMetadata(ctx, options, appPath)
	if err != nil {
		return ActionResult{}, fmt.Errorf("inspect downloaded WorkBuddy app: %w", err)
	}
	if metadata.bundleID != WorkBuddyBundleID {
		return ActionResult{}, fmt.Errorf("downloaded app has unexpected bundle identifier %q", metadata.bundleID)
	}
	if err := verifyWorkBuddyMacOSApp(ctx, options, appPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded WorkBuddy app: %w", err)
	}
	destinations := workBuddyDestinations(options, replacePath)
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
			lastErr = commandFailure("copy WorkBuddy app", copied)
			continue
		}
		installed := baseWorkBuddyStatus("macos")
		installed.Installed, installed.Path = true, destination
		installed.Version = metadata.version
		if installed.Version == nil {
			installed.Version = nonEmptyPointer(workBuddyVersion(update))
		}
		return ActionResult{Status: "installed", Message: "WorkBuddy was installed", RefreshNeeded: true, App: installed}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no writable macOS Applications directory")
	}
	return ActionResult{}, lastErr
}

func workBuddyDestinations(options Options, replacePath string) []string {
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
		result = append(result, filepath.Join(dir, "WorkBuddy.app"))
	}
	return result
}

func findWorkBuddyApp(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.EqualFold(entry.Name(), "WorkBuddy.app") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("inspect extracted WorkBuddy installer: %w", err)
	}
	if found == "" {
		return "", errors.New("no WorkBuddy.app bundle found in installer")
	}
	return found, nil
}

func installWorkBuddyWindows(ctx context.Context, options Options, status Status) (ActionResult, error) {
	update, err := workBuddyPackage(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	if !strings.EqualFold(filepath.Ext(mustParseURLPath(update.URL)), ".exe") {
		return ActionResult{}, errors.New("WorkBuddy Windows installer is not an executable")
	}
	installer, err := os.CreateTemp("", "oneagent-workbuddy-*.exe")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary WorkBuddy installer: %w", err)
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
	if err := downloadFile(ctx, options, update.URL, installerPath, WorkBuddyID); err != nil {
		return ActionResult{}, fmt.Errorf("download WorkBuddy installer: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	if err := verifyWorkBuddyWindowsInstaller(ctx, options, installerPath); err != nil {
		return ActionResult{}, fmt.Errorf("verify downloaded WorkBuddy installer with Authenticode: %w", err)
	}
	if err := start(options, []string{installerPath}); err != nil {
		return ActionResult{}, fmt.Errorf("start WorkBuddy installer: %w", err)
	}
	keep = true
	status.Source = SourceWindowsInstaller
	if status.Version == nil {
		status.Version = nonEmptyPointer(workBuddyVersion(update))
	}
	return ActionResult{Status: "installer-started", Message: "The downloaded WorkBuddy installer was started", RefreshNeeded: true, App: status}, nil
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
