package desktopapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ZCodeID       = "zcode"
	ZCodeName     = "ZCode"
	ZCodeBundleID = "dev.zcode.app"
	// ZCodeMacTeamID is the Developer ID team behind the signed app on macOS,
	// read from the installed bundle rather than from documentation.
	ZCodeMacTeamID = "8A5X4JJ39T"
	ZCodeHome      = "https://zcode.z.ai/"
)

// inspectZCode reports whether ZCode is installed. There is no install path: no
// official download endpoint for the app was found, so OneAgent detects and
// configures an existing installation and points the user at the vendor's site
// otherwise. Adding a downloader on a guessed URL would put an unverifiable
// binary behind an install button.
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

// baseZCodeStatus marks the platforms ZCode ships for. Unlike the agents with an
// installer, "supported" here means OneAgent can detect and configure it.
func baseZCodeStatus(osID string) Status {
	supported := osID == "macos" || osID == "windows"
	return Status{ID: ZCodeID, Name: ZCodeName, Supported: supported, Source: SourceUnknown}
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

func installZCode(ctx context.Context, options Options) (ActionResult, error) {
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	status := inspectZCode(ctx, options)
	if status.Installed {
		return ActionResult{Status: "already-installed", Message: ZCodeName + " is already installed", App: status}, nil
	}
	// Deliberately not a download. Returning the vendor's page is the honest
	// answer when no verifiable installer URL is known, and it keeps OneAgent from
	// fetching an executable from an address it cannot vouch for.
	return ActionResult{}, fmt.Errorf("%s has no automated installer; download it from %s and run this step again", ZCodeName, ZCodeHome)
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
