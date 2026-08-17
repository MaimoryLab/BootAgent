package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	DSHDesktopID           = "dsh-desktop"
	DSHDesktopName         = "DSH Desktop"
	DSHDesktopHome         = "https://dshdesktop.cn"
	DSHDesktopReleaseAPI   = "https://api.github.com/repos/anywhere-labs/deepseek-harness-desktop/releases/latest"
	DSHDesktopMacMirrorURL = "https://www.dshdesktop.cn/api/downloads/mac"
	DSHDesktopWinMirrorURL = "https://www.dshdesktop.cn/api/downloads/windows"
)

func dshURL(ctx context.Context, options Options) (string, error) {
	if strings.TrimSpace(options.DownloadURL) != "" {
		return approvedDownloadURL(options.DownloadURL, "github.com", "www.dshdesktop.cn")
	}
	if options.Platform.OS == "macos" && options.Platform.Arch != "arm64" && options.Platform.Arch != "aarch64" {
		return "", fmt.Errorf("%s has no package for %s/%s", DSHDesktopName, options.Platform.OS, options.Platform.Arch)
	}
	if options.Platform.OS != "macos" && options.Platform.OS != "windows" {
		return "", fmt.Errorf("%s is not supported on %s", DSHDesktopName, options.Platform.OS)
	}
	if options.PreferMirror {
		url := DSHDesktopMacMirrorURL
		if options.Platform.OS == "windows" {
			url = DSHDesktopWinMirrorURL
		}
		return approvedDownloadURL(url, "www.dshdesktop.cn")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, DSHDesktopReleaseAPI, nil)
	if err != nil {
		return "", err
	}
	client := options.Downloader
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub release request returned HTTP %d", response.StatusCode)
	}
	var release struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		return "", err
	}
	ext := ".dmg"
	if options.Platform.OS == "windows" {
		ext = ".exe"
	}
	for _, asset := range release.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ext) && strings.Contains(strings.ToLower(asset.Name), "arm64") == (options.Platform.OS == "macos") {
			return approvedDownloadURL(asset.URL, "github.com")
		}
	}
	return "", fmt.Errorf("GitHub release has no %s %s asset", DSHDesktopName, ext)
}

func inspectDSH(ctx context.Context, options Options) Status {
	status := Status{ID: DSHDesktopID, Name: DSHDesktopName, Supported: options.Platform.OS == "macos" || options.Platform.OS == "windows", Source: SourceUnknown}
	if options.Platform.OS == "macos" {
		status.Source = SourceMacOSDMG
		roots := options.SearchRoots
		if len(roots) == 0 {
			roots = []string{"/Applications"}
		}
		for _, root := range roots {
			path := root
			if !strings.HasSuffix(strings.ToLower(path), ".app") {
				path = filepath.Join(root, "DSH Desktop.app")
			}
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				status.Installed, status.Path = true, path
				return status
			}
		}
	} else if options.Platform.OS == "windows" {
		status.Source = SourceWindowsInstaller
		for _, root := range dshWindowsCandidates(options) {
			if info, err := os.Stat(root); err == nil && !info.IsDir() {
				status.Installed, status.Path = true, root
				return status
			}
		}
	}
	if err := contextError(ctx); err != nil {
		status.InspectionUnavailable = nonEmptyPointer(err.Error())
	}
	return status
}

func dshWindowsCandidates(options Options) []string {
	if len(options.SearchRoots) > 0 {
		return options.SearchRoots
	}
	if options.Home == "" {
		return nil
	}
	return []string{filepath.Join(options.Home, "AppData", "Local", "Programs", "DSH Desktop", "DSH Desktop.exe")}
}

func installDSH(ctx context.Context, options Options) (ActionResult, error) {
	status := inspectDSH(ctx, options)
	if status.Installed {
		return ActionResult{Status: "already-installed", Message: DSHDesktopName + " is already installed", App: status}, nil
	}
	url, err := dshURL(ctx, options)
	if err != nil {
		return ActionResult{}, err
	}
	if options.Platform.OS == "windows" {
		path, err := os.CreateTemp("", "bootagent-dsh-*.exe")
		if err != nil {
			return ActionResult{}, err
		}
		name := path.Name()
		_ = path.Close()
		defer os.Remove(name)
		if err := downloadFile(ctx, options, url, name, DSHDesktopID); err != nil {
			return ActionResult{}, err
		}
		if err := verifyDSHWindowsInstaller(ctx, options, name); err != nil {
			return ActionResult{}, err
		}
		if err := start(options, []string{name}); err != nil {
			return ActionResult{}, err
		}
		status.Source = SourceWindowsInstaller
		return ActionResult{Status: "installer-started", Message: "The downloaded " + DSHDesktopName + " installer was started", RefreshNeeded: true, App: status}, nil
	}
	tmp, err := os.CreateTemp("", "bootagent-dsh-*.dmg")
	if err != nil {
		return ActionResult{}, err
	}
	name := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(name)
	if err := downloadFile(ctx, options, url, name, DSHDesktopID); err != nil {
		return ActionResult{}, err
	}
	mount := filepath.Dir(name) + "/mount"
	if err := os.MkdirAll(mount, 0o700); err != nil {
		return ActionResult{}, err
	}
	defer os.RemoveAll(mount)
	result, err := run(options, ctx, []string{"/usr/bin/hdiutil", "attach", name, "-nobrowse", "-readonly", "-mountpoint", mount}, installTimeout)
	if err != nil {
		return ActionResult{}, fmt.Errorf("mount %s installer: %w", DSHDesktopName, err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("mount "+DSHDesktopName+" installer", result)
	}
	app := filepath.Join(mount, "DSH Desktop.app")
	if _, err := os.Stat(app); err != nil {
		return ActionResult{}, errors.New("DSH Desktop.app not found in installer")
	}
	if err := runDSHMacSignatureCheck(ctx, options, app); err != nil {
		return ActionResult{}, err
	}
	dest := "/Applications/DSH Desktop.app"
	if len(options.ApplicationDirs) > 0 {
		dest = filepath.Join(options.ApplicationDirs[0], "DSH Desktop.app")
	}
	if result, err = run(options, ctx, []string{"/usr/bin/ditto", app, dest}, installTimeout); err != nil {
		return ActionResult{}, fmt.Errorf("install %s: %w", DSHDesktopName, err)
	}
	if result.ExitCode != 0 {
		return ActionResult{}, commandFailure("install "+DSHDesktopName, result)
	}
	status.Installed, status.Path, status.Source = true, dest, SourceMacOSDMG
	return ActionResult{Status: "installed", Message: DSHDesktopName + " was installed", RefreshNeeded: true, App: status}, nil
}

func runDSHMacSignatureCheck(ctx context.Context, options Options, app string) error {
	result, err := run(options, ctx, []string{"/usr/bin/codesign", "--verify", "--deep", "--strict", app}, installTimeout)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return commandFailure("verify DSH Desktop macOS code signature", result)
	}
	return nil
}

func openDSH(ctx context.Context, options Options) error {
	status := inspectDSH(ctx, options)
	if !status.Installed {
		return errors.New(DSHDesktopName + " is not installed")
	}
	if options.Platform.OS == "macos" {
		return start(options, []string{"/usr/bin/open", "-a", status.Path})
	}
	return start(options, []string{status.Path})
}
