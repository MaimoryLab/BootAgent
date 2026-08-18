package desktopapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ClaudeDesktopID       = "claude-desktop"
	ClaudeDesktopName     = "Claude Desktop"
	ClaudeDesktopBundleID = "com.anthropic.claudefordesktop"
	ClaudeDesktopHome     = "https://claude.com/download"
)

func inspectClaudeDesktop(ctx context.Context, options Options) Status {
	status := baseClaudeDesktopStatus(options.Platform.OS)
	if err := contextError(ctx); err != nil {
		status.InspectionUnavailable = nonEmptyPointer(err.Error())
		return status
	}
	var found Status
	var err error
	switch options.Platform.OS {
	case "macos":
		found, err = inspectClaudeDesktopMacOS(ctx, options)
	case "windows":
		found, err = inspectClaudeDesktopWindows(options)
	default:
		return status
	}
	if err != nil {
		status.InspectionUnavailable = nonEmptyPointer(err.Error())
		return status
	}
	return found
}

func baseClaudeDesktopStatus(osID string) Status {
	status := Status{ID: ClaudeDesktopID, Name: ClaudeDesktopName, Source: SourceUnknown}
	if osID == "macos" || osID == "windows" {
		status.Supported = true
		status.Source = osID + "-application"
	}
	return status
}

func inspectClaudeDesktopMacOS(ctx context.Context, options Options) (Status, error) {
	status := baseClaudeDesktopStatus("macos")
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
			candidate = filepath.Join(root, "Claude.app")
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
		if metadata.bundleID == ClaudeDesktopBundleID {
			status.Installed, status.Path, status.Version = true, candidate, metadata.version
			return status, nil
		}
	}
	return status, lastErr
}

func inspectClaudeDesktopWindows(options Options) (Status, error) {
	status := baseClaudeDesktopStatus("windows")
	for _, candidate := range claudeDesktopWindowsCandidates(options) {
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return status, err
		}
		if !info.IsDir() {
			status.Installed, status.Path = true, candidate
			return status, nil
		}
	}
	return status, nil
}

func claudeDesktopWindowsCandidates(options Options) []string {
	if len(options.SearchRoots) > 0 {
		candidates := make([]string, 0, len(options.SearchRoots))
		for _, root := range options.SearchRoots {
			if strings.EqualFold(filepath.Ext(root), ".exe") {
				candidates = append(candidates, root)
			} else {
				candidates = append(candidates, filepath.Join(root, "Claude.exe"))
			}
		}
		return candidates
	}
	local := filepath.Join(options.Home, "AppData", "Local")
	return []string{
		filepath.Join(local, "AnthropicClaude", "Claude.exe"),
		filepath.Join(local, "Programs", "Claude", "Claude.exe"),
		filepath.Join(local, "Claude", "Claude.exe"),
	}
}

func installClaudeDesktop(ctx context.Context, options Options) (ActionResult, error) {
	status := inspectClaudeDesktop(ctx, options)
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	if status.Installed {
		return ActionResult{Status: "already-installed", Message: "Claude Desktop is already installed", App: status}, nil
	}
	var argv []string
	switch options.Platform.OS {
	case "macos":
		argv = []string{"/usr/bin/open", ClaudeDesktopHome}
	case "windows":
		argv = []string{"explorer.exe", ClaudeDesktopHome}
	default:
		return ActionResult{}, fmt.Errorf("%s is not supported on %s", ClaudeDesktopName, options.Platform.OS)
	}
	if err := start(options, argv); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Status: "installer-opened", Message: "Claude Desktop download page opened", App: status}, nil
}

func openClaudeDesktop(ctx context.Context, options Options) error {
	status := inspectClaudeDesktop(ctx, options)
	if !status.Installed {
		return fmt.Errorf("%s is not installed", ClaudeDesktopName)
	}
	if options.Platform.OS == "macos" {
		return start(options, []string{"/usr/bin/open", "-a", status.Path})
	}
	return start(options, []string{status.Path})
}
