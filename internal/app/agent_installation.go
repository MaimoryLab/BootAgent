package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/install"
)

// AgentInstallation is one independently managed installation of an Agent.
// Paths are diagnostic metadata; the backend re-discovers and validates them
// before any destructive operation.
type AgentInstallation struct {
	ID           string `json:"id"`
	Manager      string `json:"manager"`
	Package      string `json:"package"`
	Prefix       string `json:"prefix,omitempty"`
	Executable   string `json:"executable"`
	Version      string `json:"version,omitempty"`
	CanUninstall bool   `json:"canUninstall"`
	Reason       string `json:"reason,omitempty"`
}

func (u *UseCases) discoverAgentInstallations(ctx context.Context, agentID string, agent catalog.Agent) []AgentInstallation {
	if agent.Command == "" || agent.Package == nil {
		return []AgentInstallation{}
	}
	paths := allCommandPaths(agent.Command, u.environment, u.status.Platform.OS)
	paths = append(paths, managerCommandPaths(ctx, u, agent.Command)...)
	if u.status.Platform.OS == "windows" {
		paths = append(paths, windowsManagerCommandPaths(ctx, u, agent.Command)...)
	}
	result := make([]AgentInstallation, 0, len(paths))
	seen := make(map[string]bool)
	for _, executable := range paths {
		if seen[executable] {
			continue
		}
		seen[executable] = true
		installation := AgentInstallation{Executable: executable, Manager: agent.Package.Manager, Package: agent.Package.Name}
		if strings.Contains(filepath.ToSlash(executable), "/.local/share/mise/") || strings.Contains(filepath.ToSlash(executable), "/mise/installs/") {
			installation.Manager = "mise"
		}
		if strings.Contains(filepath.ToSlash(executable), "/Cellar/") || strings.Contains(filepath.ToSlash(executable), "/homebrew/") {
			installation.Manager = "homebrew"
		}
		if prefix, packageName, ok := npmInstallationForPath(executable, agent, u.status.Platform.OS); ok {
			installation.Manager = "npm"
			installation.Package = packageName
			installation.Prefix = prefix
			installation.ID = "npm:" + prefix
			installation.CanUninstall = true
		} else if agent.Package.Manager == "uv" {
			installation.ID = "uv:" + agent.Package.Name
			installation.CanUninstall = uvToolListed(ctx, u, agent.Package.Name)
			if !installation.CanUninstall {
				installation.Reason = "未在 uv 工具列表中确认"
			}
		} else if agent.Package.Manager == "official-script" {
			installation.ID = "official:" + executable
			installation.CanUninstall = officialInstallationOwned(agentID, u.status.Home, executable)
			if !installation.CanUninstall {
				installation.Reason = "无法确认官方安装目录归属"
			}
		}
		if installation.ID == "" {
			installation.ID = installation.Manager + ":" + executable
			installation.Reason = "未识别安装来源"
		}
		result = append(result, installation)
	}
	return result
}

func windowsManagerCommandPaths(ctx context.Context, u *UseCases, command string) []string {
	runner := u.installRuntime(nil).Runner
	probes := [][]string{{"winget", "list", "--id", command}, {"scoop", "which", command}, {"choco", "list", "--local-only", command}}
	paths := []string{}
	for _, probe := range probes {
		binary, ok := runner.LookPath(probe[0])
		if !ok || binary == "" {
			continue
		}
		result, err := runner.Run(ctx, append([]string{binary}, probe[1:]...), nil, install.DefaultCommandTimeout)
		if err != nil || result.ExitCode != 0 {
			continue
		}
		for _, line := range strings.Split(result.Stdout, "\n") {
			candidate := strings.TrimSpace(line)
			if filepath.IsAbs(candidate) {
				if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
					paths = append(paths, candidate)
				}
			}
		}
	}
	return paths
}

// managerCommandPaths asks version managers for their resolved executable. This
// catches shims and prefixes that are not represented by the process PATH.
func managerCommandPaths(ctx context.Context, u *UseCases, command string) []string {
	runner := u.installRuntime(nil).Runner
	paths := []string{}
	for _, probe := range [][]string{{"mise", "which", command}, {"brew", "--prefix", command}} {
		binary, ok := runner.LookPath(probe[0])
		if !ok || binary == "" {
			continue
		}
		result, err := runner.Run(ctx, append([]string{binary}, probe[1:]...), nil, install.DefaultCommandTimeout)
		if err != nil || result.ExitCode != 0 {
			continue
		}
		for _, line := range strings.Split(result.Stdout, "\n") {
			candidate := strings.TrimSpace(line)
			if candidate == "" {
				continue
			}
			if probe[0] == "brew" {
				candidate = filepath.Join(candidate, "bin", command)
			}
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				absolute, absErr := filepath.Abs(candidate)
				if absErr == nil {
					paths = append(paths, filepath.Clean(absolute))
				}
			}
		}
	}
	return paths
}

func allCommandPaths(command string, environment map[string]string, osID string) []string {
	pathValue := environment["PATH"]
	if pathValue == "" {
		pathValue = os.Getenv("PATH")
	}
	extensions := []string{""}
	if osID == "windows" || runtime.GOOS == "windows" {
		extensions = []string{"", ".exe", ".cmd", ".bat"}
	}
	result := []string{}
	for _, dir := range filepath.SplitList(pathValue) {
		for _, ext := range extensions {
			candidate := filepath.Join(dir, command+ext)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				absolute, err := filepath.Abs(candidate)
				if err == nil {
					result = append(result, filepath.Clean(absolute))
				}
			}
		}
	}
	return result
}

func npmInstallationForPath(executable string, agent catalog.Agent, osID string) (string, string, bool) {
	name := agent.Package.Name
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", "", false
	}
	if osID == "windows" {
		launcherDirectory, err := filepath.EvalSymlinks(filepath.Dir(executable))
		if err != nil {
			return "", "", false
		}
		packageRoot := filepath.Join(launcherDirectory, "node_modules", filepath.FromSlash(name))
		if prefix, ok := npmGlobalPrefixForPackage(packageRoot, name, osID); ok && npmPackageNameMatches(packageRoot, name) {
			return prefix, name, true
		}
		return "", "", false
	}
	for dir := filepath.Dir(resolved); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if prefix, ok := npmGlobalPrefixForPackage(dir, name, osID); ok && npmPackageNameMatches(dir, name) {
			return prefix, name, true
		}
	}
	return "", "", false
}

func npmGlobalPrefixForPackage(packageRoot, name, osID string) (string, bool) {
	modules := filepath.Dir(packageRoot)
	if strings.HasPrefix(name, "@") {
		modules = filepath.Dir(modules)
	}
	if filepath.Base(modules) != "node_modules" || filepath.Join(modules, filepath.FromSlash(name)) != packageRoot {
		return "", false
	}
	prefix := filepath.Dir(modules)
	if osID != "windows" {
		if filepath.Base(prefix) != "lib" {
			return "", false
		}
		prefix = filepath.Dir(prefix)
	}
	for _, component := range strings.Split(filepath.ToSlash(prefix), "/") {
		if strings.EqualFold(component, "node_modules") || strings.EqualFold(component, ".pnpm") {
			return "", false
		}
	}
	return prefix, true
}

func npmPackageNameMatches(packageRoot, name string) bool {
	data, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		return false
	}
	var metadata struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(data, &metadata) == nil && metadata.Name == name
}

func uvToolListed(ctx context.Context, u *UseCases, packageName string) bool {
	uv, ok := u.installRuntime(nil).Runner.LookPath("uv")
	if !ok || uv == "" {
		return false
	}
	result, err := u.installRuntime(nil).Run(ctx, []string{uv, "tool", "list"}, nil, install.DefaultCommandTimeout)
	if err != nil || result.ExitCode != 0 {
		return false
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.TrimSuffix(fields[0], "*") == packageName {
			return true
		}
	}
	return false
}

func officialInstallationOwned(agentID, home, executable string) bool {
	root := ""
	if agentID == "kimi-code" {
		root = filepath.Join(home, ".kimi-code")
	} else if agentID == "hermes" {
		root = filepath.Join(home, ".hermes", "hermes-agent")
	}
	if root == "" {
		return false
	}
	root, _ = filepath.Abs(root)
	executable, _ = filepath.Abs(executable)
	if !pathWithin(root, executable) || !officialInstallMarkerPresent(agentID, root) {
		return false
	}
	return true
}
