// Package desktopapp detects and manages the official ChatGPT Desktop app.
//
// ChatGPT Desktop and the Codex CLI are separate products at install time, even
// though the app currently uses the Codex bundle/package identity and shares the
// ~/.codex configuration target.
package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

const (
	ID                  = "chatgpt-desktop"
	Name                = "ChatGPT Desktop"
	CodexBundleID       = "com.openai.codex"
	WindowsPackageName  = "OpenAI.Codex_2p2nqsd0c76g0"
	WindowsAUMID        = WindowsPackageName + "!App"
	MacDownloadURL      = "https://persistent.oaistatic.com/codex-app-prod/ChatGPT.dmg"
	MacDownloadURLX64   = "https://persistent.oaistatic.com/codex-app-prod/ChatGPT-latest-x64.dmg"
	WindowsInstallerURL = "https://get.microsoft.com/installer/download/9PLM9XGG6VKS?cid=website_cta_psi"
)

const (
	SourceMacOSDMG     = "macos-dmg"
	SourceWindowsStore = "windows-store"
	SourceUnknown      = "unknown"
)

// Status is intentionally separate from app.AgentStatus. A desktop app has no
// package-manager command or CLI version probe.
type Status struct {
	ID                    string  `json:"id"`
	Name                  string  `json:"name"`
	Installed             bool    `json:"installed"`
	Supported             bool    `json:"supported"`
	Path                  string  `json:"path,omitempty"`
	Version               *string `json:"version"`
	Source                string  `json:"source"`
	PackageFamily         string  `json:"packageFamily,omitempty"`
	InspectionUnavailable *string `json:"inspectionUnavailable,omitempty"`
}

// ActionResult describes an install/open-installer action. Windows Store
// installation is external and asynchronous, so it is never reported as a
// completed local install.
type ActionResult struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	RefreshNeeded bool   `json:"refreshNeeded"`
	App           Status `json:"app"`
}

// Options keeps platform and process boundaries injectable. SearchRoots and
// ApplicationDirs are test seams; production callers leave them empty.
type Options struct {
	Home            string
	Platform        platform.Info
	Runner          process.Runner
	Output          process.OutputListener
	DownloadURL     string
	SearchRoots     []string
	ApplicationDirs []string
}

const (
	inspectTimeout = 10 * time.Second
	installTimeout = 20 * time.Minute
)

var packageVersionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}$`)

// Inspect never returns an operational error. A broken plist, unavailable
// PowerShell, or inaccessible AppX metadata is represented in the status so a
// single desktop-app probe cannot break the whole environment status call.
func Inspect(ctx context.Context, options Options) Status {
	if ctx == nil {
		ctx = context.Background()
	}
	status := baseStatus(options.Platform.OS)
	if err := ctx.Err(); err != nil {
		message := err.Error()
		status.InspectionUnavailable = &message
		return status
	}
	var inspected Status
	var err error
	switch options.Platform.OS {
	case "macos":
		inspected, err = inspectMacOS(ctx, options)
	case "windows":
		inspected, err = inspectWindows(ctx, options)
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

// Install installs the app when the platform permits a local install. On
// Windows it opens the official Store bootstrapper and returns immediately.
func Install(ctx context.Context, options Options) (ActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	status := Inspect(ctx, options)
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	if status.Installed && options.Platform.OS == "macos" {
		return ActionResult{Status: "already-installed", Message: "ChatGPT Desktop is already installed", App: status}, nil
	}
	switch options.Platform.OS {
	case "macos":
		return installMacOS(ctx, options)
	case "windows":
		return openWindowsInstaller(options, status)
	default:
		return ActionResult{}, fmt.Errorf("ChatGPT Desktop is not supported on %s", options.Platform.OS)
	}
}

// OpenInstaller opens the official distribution source. It is useful for
// updates because neither source exposes a stable public latest-version API.
func OpenInstaller(ctx context.Context, options Options) (ActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	status := Inspect(ctx, options)
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	switch options.Platform.OS {
	case "macos":
		url := macDownloadURL(options)
		if err := start(options, []string{"/usr/bin/open", url}); err != nil {
			return ActionResult{}, err
		}
		status.Source = SourceMacOSDMG
		return ActionResult{Status: "external-installer-opened", Message: "The official macOS installer was opened", RefreshNeeded: true, App: status}, nil
	case "windows":
		return openWindowsInstaller(options, status)
	default:
		return ActionResult{}, fmt.Errorf("ChatGPT Desktop is not supported on %s", options.Platform.OS)
	}
}

// Open launches an installed desktop app without touching its configuration.
func Open(ctx context.Context, options Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	status := Inspect(ctx, options)
	if err := contextError(ctx); err != nil {
		return err
	}
	if !status.Installed {
		if status.InspectionUnavailable != nil {
			return errors.New(*status.InspectionUnavailable)
		}
		return errors.New("ChatGPT Desktop is not installed")
	}
	switch options.Platform.OS {
	case "macos":
		if status.Path == "" {
			return errors.New("ChatGPT Desktop path is unavailable")
		}
		return start(options, []string{"/usr/bin/open", "-a", status.Path})
	case "windows":
		aumid := status.PackageFamily
		if aumid == "" {
			aumid = WindowsAUMID
		}
		return start(options, []string{"explorer.exe", "shell:AppsFolder\\" + normalizeAUMID(aumid)})
	default:
		return fmt.Errorf("ChatGPT Desktop is not supported on %s", options.Platform.OS)
	}
}

func baseStatus(osID string) Status {
	source := SourceUnknown
	supported := false
	switch osID {
	case "macos":
		source, supported = SourceMacOSDMG, true
	case "windows":
		source, supported = SourceWindowsStore, true
	}
	return Status{ID: ID, Name: Name, Supported: supported, Source: source}
}

func runnerFor(options Options) process.Runner {
	if options.Runner != nil {
		return options.Runner
	}
	return process.Current()
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func run(options Options, ctx context.Context, argv []string, timeout time.Duration) (process.Result, error) {
	if len(argv) == 0 {
		return process.Result{ExitCode: -1}, errors.New("desktop-app command is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Output != nil {
		options.Output(process.Output{Kind: "command", Args: append([]string(nil), argv...)})
	}
	return runnerFor(options).Run(ctx, argv, nil, timeout)
}

func start(options Options, argv []string) error {
	launcher, ok := process.AsLauncher(runnerFor(options))
	if !ok {
		return errors.New("this build cannot launch external desktop applications")
	}
	if options.Output != nil {
		options.Output(process.Output{Kind: "command", Args: append([]string(nil), argv...)})
	}
	return launcher.Start(argv, nil)
}

func inspectMacOS(ctx context.Context, options Options) (Status, error) {
	status := baseStatus("macos")
	roots := options.SearchRoots
	if len(roots) == 0 {
		roots = []string{"/Applications"}
		if options.Home != "" {
			roots = append(roots, filepath.Join(options.Home, "Applications"))
		}
	}
	candidates := make([]string, 0, len(roots)*4)
	for _, root := range roots {
		if strings.EqualFold(filepath.Ext(root), ".app") {
			candidates = append(candidates, root)
			continue
		}
		for _, name := range []string{"ChatGPT.app", "Codex.app", "OpenAI Codex.app", "OpenAI.Codex.app"} {
			candidates = append(candidates, filepath.Join(root, name))
		}
	}
	var lastErr error
	for _, candidate := range candidates {
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
		if metadata.bundleID != CodexBundleID {
			continue
		}
		status.Installed = true
		status.Path = candidate
		status.Version = metadata.version
		return status, nil
	}
	return status, lastErr
}

type macMetadata struct {
	bundleID string
	version  *string
}

func readMacMetadata(ctx context.Context, options Options, appPath string) (macMetadata, error) {
	plist := filepath.Join(appPath, "Contents", "Info.plist")
	bundleID, err := plutilValue(ctx, options, plist, "CFBundleIdentifier")
	if err != nil {
		return macMetadata{}, err
	}
	version, versionErr := plutilValue(ctx, options, plist, "CFBundleShortVersionString")
	if versionErr != nil || version == "" {
		version, _ = plutilValue(ctx, options, plist, "CFBundleVersion")
	}
	return macMetadata{bundleID: bundleID, version: nonEmptyPointer(version)}, nil
}

func plutilValue(ctx context.Context, options Options, plist, key string) (string, error) {
	result, err := run(options, ctx, []string{"/usr/bin/plutil", "-extract", key, "raw", "-o", "-", plist}, inspectTimeout)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = "plutil could not read " + key
		}
		return "", errors.New(message)
	}
	return strings.TrimSpace(result.Stdout), nil
}

func installMacOS(ctx context.Context, options Options) (ActionResult, error) {
	url := macDownloadURL(options)
	tempDir, err := os.MkdirTemp("", "oneagent-chatgpt-")
	if err != nil {
		return ActionResult{}, fmt.Errorf("create temporary installer directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	dmg := filepath.Join(tempDir, "ChatGPT.dmg")
	result, err := run(options, ctx, []string{"/usr/bin/curl", "-fL", "--retry", "3", "--retry-delay", "1", "-o", dmg, url}, installTimeout)
	if err != nil {
		return ActionResult{}, fmt.Errorf("download ChatGPT installer: %w", err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("download ChatGPT installer", result)
	}
	result, err = run(options, ctx, []string{"/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", dmg}, installTimeout)
	if err != nil {
		return ActionResult{}, fmt.Errorf("mount ChatGPT installer: %w", err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("mount ChatGPT installer", result)
	}
	mountPoint, err := parseMountPoint(result.Stdout)
	if err != nil {
		return ActionResult{}, err
	}
	defer func() {
		_, _ = run(options, context.Background(), []string{"/usr/bin/hdiutil", "detach", mountPoint}, installTimeout)
	}()
	appPath, err := findMountedApp(mountPoint)
	if err != nil {
		return ActionResult{}, err
	}
	metadata, err := readMacMetadata(ctx, options, appPath)
	if err != nil {
		return ActionResult{}, fmt.Errorf("inspect downloaded ChatGPT app: %w", err)
	}
	if metadata.bundleID != CodexBundleID {
		return ActionResult{}, fmt.Errorf("downloaded app has unexpected bundle identifier %q", metadata.bundleID)
	}
	dirs := options.ApplicationDirs
	if len(dirs) == 0 {
		dirs = []string{"/Applications"}
		if options.Home != "" {
			dirs = append(dirs, filepath.Join(options.Home, "Applications"))
		}
	}
	appName := filepath.Base(appPath)
	var lastErr error
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			lastErr = err
			continue
		}
		destination := filepath.Join(dir, appName)
		if _, err := os.Stat(destination); err == nil {
			lastErr = fmt.Errorf("destination already exists: %s", destination)
			continue
		} else if !os.IsNotExist(err) {
			lastErr = err
			continue
		}
		copyResult, copyErr := run(options, ctx, []string{"/usr/bin/ditto", appPath, destination}, installTimeout)
		if copyErr != nil {
			lastErr = copyErr
			continue
		}
		if copyResult.ExitCode != 0 {
			lastErr = commandFailure("copy ChatGPT app", copyResult)
			continue
		}
		installed := baseStatus("macos")
		installed.Installed = true
		installed.Path = destination
		installed.Version = metadata.version
		return ActionResult{Status: "installed", Message: "ChatGPT Desktop was installed", RefreshNeeded: true, App: installed}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no writable macOS Applications directory")
	}
	return ActionResult{}, lastErr
}

func macDownloadURL(options Options) string {
	if strings.TrimSpace(options.DownloadURL) != "" {
		return options.DownloadURL
	}
	if strings.EqualFold(options.Platform.Arch, "x64") || strings.EqualFold(options.Platform.Arch, "amd64") || strings.EqualFold(options.Platform.Arch, "x86_64") {
		return MacDownloadURLX64
	}
	return MacDownloadURL
}

func findMountedApp(mountPoint string) (string, error) {
	for _, name := range []string{"ChatGPT.app", "Codex.app", "OpenAI Codex.app", "OpenAI.Codex.app"} {
		candidate := filepath.Join(mountPoint, name)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	entries, err := os.ReadDir(mountPoint)
	if err != nil {
		return "", fmt.Errorf("read mounted ChatGPT installer: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".app") {
			return filepath.Join(mountPoint, entry.Name()), nil
		}
	}
	return "", errors.New("no .app bundle found in ChatGPT installer")
}

func parseMountPoint(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		if index := strings.Index(line, "/Volumes/"); index >= 0 {
			value := strings.TrimSpace(line[index:])
			if value != "" {
				return value, nil
			}
		}
	}
	return "", errors.New("could not determine the mounted ChatGPT installer volume")
}

func inspectWindows(ctx context.Context, options Options) (Status, error) {
	status := baseStatus("windows")
	result, err := run(options, ctx, windowsPackageQuery(), inspectTimeout)
	var packageQueryErr error
	if err == nil && result.ExitCode == 0 {
		if packageInfo, ok := parseWindowsPackage(result.Stdout); ok && isKnownPackage(packageInfo.Name, packageInfo.PackageFullName) {
			status.Installed = true
			status.Path = strings.TrimSpace(packageInfo.InstallLocation)
			status.Version = nonEmptyPointer(packageInfo.Version)
			status.PackageFamily = strings.TrimSpace(packageInfo.PackageFamilyName)
			if status.PackageFamily == "" {
				status.PackageFamily = packageFamilyFromFullName(packageInfo.PackageFullName)
			}
			if status.Version == nil {
				status.Version = nonEmptyPointer(versionFromPackageFullName(packageInfo.PackageFullName))
			}
			return status, nil
		}
		// An empty JSON result means no registered package. The StartApps query
		// below can still detect a registration that the package cmdlet cannot
		// inspect under a restricted account.
	} else if err != nil {
		packageQueryErr = err
		// Continue to the narrow StartApps fallback. It is a registered-app
		// query, not a package-directory scan.
	} else {
		packageQueryErr = commandFailure("query Windows AppX packages", result)
	}
	startApps, startErr := run(options, ctx, windowsStartAppsQuery(), inspectTimeout)
	if startErr != nil {
		return status, startErr
	}
	if startApps.ExitCode != 0 {
		return status, commandFailure("query Windows desktop apps", startApps)
	}
	for _, line := range strings.Split(startApps.Stdout, "\n") {
		appID := strings.TrimSpace(line)
		if !isKnownStartAppID(appID) {
			continue
		}
		status.Installed = true
		status.PackageFamily = appID[:strings.IndexByte(appID, '!')]
		message := "Windows registered the app, but AppX version metadata was unavailable"
		status.InspectionUnavailable = &message
		return status, nil
	}
	if packageQueryErr != nil {
		return status, packageQueryErr
	}
	return status, nil
}

type windowsPackage struct {
	Name              string `json:"Name"`
	PackageFullName   string `json:"PackageFullName"`
	PackageFamilyName string `json:"PackageFamilyName"`
	Version           string `json:"Version"`
	InstallLocation   string `json:"InstallLocation"`
}

func windowsPackageQuery() []string {
	script := `$items = @(
  Get-AppxPackage -Name 'OpenAI.Codex' -ErrorAction SilentlyContinue
  Get-AppxPackage -Name 'OpenAI.CodexBeta' -ErrorAction SilentlyContinue
  Get-AppxPackage -Name 'OpenAI.ChatGPT-Desktop' -ErrorAction SilentlyContinue
)
$items | Sort-Object Version -Descending | Select-Object -First 1 Name,PackageFullName,PackageFamilyName,@{Name='Version';Expression={[string]$_.Version}},InstallLocation | ConvertTo-Json -Compress`
	return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script}
}

func windowsStartAppsQuery() []string {
	script := `Get-StartApps | Where-Object { $_.AppID -like 'OpenAI.Codex_*!App' -or $_.AppID -like 'OpenAI.CodexBeta_*!App' -or $_.AppID -like 'OpenAI.ChatGPT-Desktop_*!App' } | Select-Object -First 1 -ExpandProperty AppID`
	return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script}
}

func parseWindowsPackage(output string) (windowsPackage, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(output), "\ufeff")
	if trimmed == "" {
		return windowsPackage{}, false
	}
	var item windowsPackage
	if json.Unmarshal([]byte(trimmed), &item) == nil && (item.Name != "" || item.PackageFullName != "") {
		return item, true
	}
	var items []windowsPackage
	if json.Unmarshal([]byte(trimmed), &items) != nil || len(items) == 0 {
		return windowsPackage{}, false
	}
	sort.SliceStable(items, func(i, j int) bool { return compareVersion(items[i].Version, items[j].Version) > 0 })
	return items[0], true
}

func isKnownPackage(name, fullName string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	fullName = strings.ToLower(strings.TrimSpace(fullName))
	for _, identity := range []string{"openai.codex", "openai.codexbeta", "openai.chatgpt-desktop"} {
		if name == identity || strings.HasPrefix(name, identity+"_") || strings.HasPrefix(fullName, identity+"_") {
			return true
		}
	}
	return false
}

func isKnownStartAppID(value string) bool {
	separator := strings.IndexByte(value, '!')
	if separator <= 0 || !strings.EqualFold(value[separator:], "!App") {
		return false
	}
	identity := strings.ToLower(value[:separator])
	for _, prefix := range []string{"openai.codex_", "openai.codexbeta_", "openai.chatgpt-desktop_"} {
		if strings.HasPrefix(identity, prefix) {
			return true
		}
	}
	return false
}

func versionFromPackageFullName(fullName string) string {
	for _, part := range strings.Split(fullName, "_") {
		if packageVersionPattern.MatchString(part) {
			return part
		}
	}
	return ""
}

func packageFamilyFromFullName(fullName string) string {
	parts := strings.Split(fullName, "_")
	if len(parts) < 2 {
		return ""
	}
	last := ""
	for index := len(parts) - 1; index > 0; index-- {
		if parts[index] != "" {
			last = parts[index]
			break
		}
	}
	if last == "" {
		return ""
	}
	return parts[0] + "_" + last
}

func normalizeAUMID(value string) string {
	if strings.Contains(value, "!") {
		return value
	}
	return value + "!App"
}

func compareVersion(left, right string) int {
	a := versionParts(left)
	b := versionParts(right)
	for len(a) < len(b) {
		a = append(a, 0)
	}
	for len(b) < len(a) {
		b = append(b, 0)
	}
	for index := range a {
		if a[index] < b[index] {
			return -1
		}
		if a[index] > b[index] {
			return 1
		}
	}
	return 0
}

func versionParts(value string) []int {
	parts := make([]int, 0, 4)
	for _, part := range strings.Split(value, ".") {
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		parts = append(parts, number)
	}
	return parts
}

func openWindowsInstaller(options Options, status Status) (ActionResult, error) {
	script := "Start-Process -FilePath '" + WindowsInstallerURL + "'"
	if err := start(options, []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script}); err != nil {
		return ActionResult{}, err
	}
	status.Source = SourceWindowsStore
	return ActionResult{Status: "external-installer-opened", Message: "The official Microsoft Store installer was opened", RefreshNeeded: true, App: status}, nil
}

func commandFailure(action string, result process.Result) error {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" {
		detail = fmt.Sprintf("exit code %d", result.ExitCode)
	}
	return fmt.Errorf("%s: %s", action, detail)
}

func nonEmptyPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
