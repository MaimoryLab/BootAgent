package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/install"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// AgentUninstallResult reports the package removed without implying that user
// configuration, Profiles, Providers, or conversation data were deleted.
type AgentUninstallResult struct {
	Agent   string `json:"agent"`
	Package string `json:"package"`
	Command string `json:"command"`
}

type AgentUninstallOptions struct {
	AllowCrossEnvironment bool
	InstallationID        string
}

// UninstallAgent removes one npm-managed Agent executable. User-owned state is
// deliberately outside this operation: only the catalog package is passed to
// npm in the managed runtime environment.
func (u *UseCases) UninstallAgent(ctx context.Context, agentID string, listeners ...process.OutputListener) (AgentUninstallResult, error) {
	return u.UninstallAgentWithOptions(ctx, agentID, AgentUninstallOptions{}, listeners...)
}

func (u *UseCases) UninstallAgentWithOptions(ctx context.Context, agentID string, options AgentUninstallOptions, listeners ...process.OutputListener) (AgentUninstallResult, error) {
	if u == nil {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Agent uninstall request was cancelled"); err != nil {
		return AgentUninstallResult{}, err
	}
	agentID = strings.TrimSpace(agentID)
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return AgentUninstallResult{}, err
	}
	agent, ok := manifest.Agents[agentID]
	if !ok || agent.Package == nil {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InvalidRequest, "Agent has no managed installation source: "+agentID)
	}
	manager := agent.Package.Manager
	if manager == "official-script" {
		if strings.HasPrefix(options.InstallationID, "npm:") {
			if alternative, found := findAlternativePackage(agent, options.InstallationID); found {
				agent.Package = &alternative
				manager = alternative.Manager
			}
		}
		if alternative, found := u.detectAlternativePackage(ctx, agent); found {
			agent.Package = &alternative
			manager = alternative.Manager
		}
	}
	if manager == "uv" {
		return u.uninstallUVAgent(ctx, agentID, agent, listeners...)
	}
	if manager == "official-script" {
		return u.uninstallOfficialScriptAgent(ctx, agentID, agent)
	}
	if manager != "npm" {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InvalidRequest, "Unsupported Agent installation source: "+manager)
	}

	unlockTask := u.lockTask("agent-task:" + agentID)
	defer unlockTask()
	if err := contextError(ctx, "Agent uninstall request was cancelled"); err != nil {
		return AgentUninstallResult{}, err
	}
	var output process.OutputListener
	if len(listeners) > 0 && listeners[0] != nil {
		base := listeners[0]
		output = func(event process.Output) { event.Agent = agentID; base(event) }
	}
	runtime := u.installRuntime(output)
	npm, present := runtime.Runner.LookPath("npm")
	if !present || npm == "" {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.PrerequisiteMissing, "npm is required to uninstall "+agent.Name)
	}
	installRecord, hasRecord := u.loadAgentInstallRecord(agentID)
	if agent.Command != "" {
		if executable, installed := runtime.Runner.LookPath(agent.Command); !installed || executable == "" {
			if !hasRecord {
				return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentPackageMissing, agent.Name+" is not installed")
			}
		}
	}

	selectedPrefix := ""
	if strings.HasPrefix(options.InstallationID, "npm:") {
		selectedPrefix = strings.TrimSpace(strings.TrimPrefix(options.InstallationID, "npm:"))
	}
	environment := install.NPMEnvironment(runtime, npm, "")
	if selectedPrefix != "" {
		environment["npm_config_prefix"] = selectedPrefix
	}
	checkArgs := []string{npm, "list", "-g", "--depth=0", "--json", agent.Package.Name}
	check, err := runtime.Run(ctx, checkArgs, environment, install.DefaultCommandTimeout)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || isPermissionFailure(err.Error()) {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMPermission, "Permission denied while checking the npm installation for "+agent.Name, oneerrors.WithStatus(403), oneerrors.WithRetryable(false), oneerrors.WithCause(err))
		}
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMFailed, "npm failed while checking the installation for "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if check.ExitCode != 0 {
		diagnostic := strings.TrimSpace(check.Stdout + "\n" + check.Stderr)
		if isPermissionFailure(diagnostic) {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMPermission, "Permission denied while checking the npm installation for "+agent.Name, oneerrors.WithStatus(403), oneerrors.WithRetryable(false))
		}
		if npmListLooksMissing(diagnostic) && agent.Command != "" && hasRecord && cleanInstallPath(installRecord.NPMPath) != cleanInstallPath(npm) {
			if !options.AllowCrossEnvironment {
				return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMMismatch, agent.Name+" is managed by another Node/npm environment; confirm cross-environment uninstall to continue", oneerrors.WithRetryable(false))
			}
			npm = installRecord.NPMPath
			environment = install.NPMEnvironment(runtime, npm, "")
			if installRecord.Prefix != "" {
				environment["npm_config_prefix"] = installRecord.Prefix
			}
			checkArgs[0] = npm
			check, err = runtime.Run(ctx, checkArgs, environment, install.DefaultCommandTimeout)
			if err != nil || check.ExitCode != 0 {
				return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMFailed, "The recorded npm environment could not verify "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true))
			}
		} else if npmListLooksMissing(diagnostic) && agent.Command != "" {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMMismatch, agent.Name+" is installed by a different Node/npm environment; activate its original npm before uninstalling", oneerrors.WithRetryable(false))
		} else {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMFailed, "npm failed while checking the installation for "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true))
		}
	}

	// npm 6 and earlier run package-controlled uninstall lifecycle scripts. They
	// are unnecessary for removing a CLI package and could mutate user-owned
	// configuration that this operation promises to preserve.
	args := []string{npm, "uninstall", "-g", "--ignore-scripts", agent.Package.Name}
	result, err := runtime.Run(ctx, args, environment, install.DefaultCommandTimeout)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || isPermissionFailure(err.Error()) {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMPermission, "Permission denied while uninstalling "+agent.Name, oneerrors.WithStatus(403), oneerrors.WithRetryable(false), oneerrors.WithCause(err))
		}
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMFailed, "npm failed while uninstalling "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if result.ExitCode != 0 {
		if isPermissionFailure(result.Stdout + "\n" + result.Stderr) {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMPermission, "Permission denied while uninstalling "+agent.Name, oneerrors.WithStatus(403), oneerrors.WithRetryable(false))
		}
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMFailed, fmt.Sprintf("npm failed while uninstalling %s: command exited with code %d", agent.Name, result.ExitCode), oneerrors.WithStatus(500), oneerrors.WithRetryable(true))
	}
	return AgentUninstallResult{Agent: agentID, Package: agent.Package.Name, Command: strings.Join(args, " ")}, nil
}

func findAlternativePackage(agent catalog.Agent, installationID string) (catalog.Package, bool) {
	for _, alternative := range agent.Package.Alternatives {
		if alternative.Manager == "npm" && installationID == "npm:"+strings.TrimSpace(installationID[len("npm:"):]) {
			return alternative, true
		}
	}
	return catalog.Package{}, false
}

func (u *UseCases) detectAlternativePackage(ctx context.Context, agent catalog.Agent) (catalog.Package, bool) {
	runtime := u.installRuntime(nil)
	for _, alternative := range agent.Package.Alternatives {
		if alternative.Manager != "npm" || alternative.Name == "" {
			continue
		}
		npm, present := runtime.Runner.LookPath("npm")
		if !present || npm == "" {
			continue
		}
		result, err := runtime.Run(ctx, []string{npm, "list", "-g", "--depth=0", "--json", alternative.Name}, nil, install.DefaultCommandTimeout)
		if err == nil && result.ExitCode == 0 && npmPackageListed(result.Stdout, alternative.Name) {
			return alternative, true
		}
	}
	return catalog.Package{}, false
}

func npmPackageListed(output, packageName string) bool {
	var tree struct {
		Dependencies map[string]json.RawMessage `json:"dependencies"`
	}
	if json.Unmarshal([]byte(output), &tree) == nil {
		if _, ok := tree.Dependencies[packageName]; ok {
			return true
		}
	}
	return false
}

func (u *UseCases) uninstallUVAgent(ctx context.Context, agentID string, agent catalog.Agent, listeners ...process.OutputListener) (AgentUninstallResult, error) {
	unlockTask := u.lockTask("agent-task:" + agentID)
	defer unlockTask()
	var output process.OutputListener
	if len(listeners) > 0 && listeners[0] != nil {
		base := listeners[0]
		output = func(event process.Output) { event.Agent = agentID; base(event) }
	}
	runtime := u.installRuntime(output)
	uv, present := runtime.Runner.LookPath("uv")
	if !present || uv == "" {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.PrerequisiteMissing, "uv is required to uninstall "+agent.Name)
	}
	list, err := runtime.Run(ctx, []string{uv, "tool", "list"}, nil, install.DefaultCommandTimeout)
	if err != nil {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentInstallFailed, "uv failed while checking the installation for "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if list.ExitCode != 0 || !strings.Contains(list.Stdout+"\n"+list.Stderr, agent.Package.Name) {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentPackageMissing, agent.Name+" is not installed by uv")
	}
	args := []string{uv, "tool", "uninstall", agent.Package.Name}
	result, err := runtime.Run(ctx, args, nil, install.DefaultCommandTimeout)
	if err != nil {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentInstallFailed, "uv failed while uninstalling "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if result.ExitCode != 0 {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentInstallFailed, fmt.Sprintf("uv failed while uninstalling %s: command exited with code %d", agent.Name, result.ExitCode), oneerrors.WithStatus(500), oneerrors.WithRetryable(true))
	}
	return AgentUninstallResult{Agent: agentID, Package: agent.Package.Name, Command: strings.Join(args, " ")}, nil
}

// uninstallOfficialScriptAgent removes only files owned by the known user-level
// installer layout. It never removes the Agent's config, credentials, sessions,
// or arbitrary PATH entries. Unknown/custom installer locations remain manual.
func (u *UseCases) uninstallOfficialScriptAgent(ctx context.Context, agentID string, agent catalog.Agent) (AgentUninstallResult, error) {
	if err := contextError(ctx, "Agent uninstall request was cancelled"); err != nil {
		return AgentUninstallResult{}, err
	}
	executable, present := u.installRuntime(nil).Runner.LookPath(agent.Command)
	if !present || executable == "" {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentPackageMissing, agent.Name+" is not installed")
	}
	home := u.status.Home
	var root string
	switch agentID {
	case "kimi-code":
		root = filepath.Join(home, ".kimi-code")
	case "hermes":
		root = filepath.Join(home, ".hermes", "hermes-agent")
	default:
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InvalidRequest, agent.Name+" does not declare a safe official uninstall layout")
	}
	root, _ = filepath.Abs(root)
	executable, _ = filepath.Abs(executable)
	if !pathWithin(root, executable) {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InvalidRequest, agent.Name+" was found outside its known official install directory; refusing to remove it")
	}
	if !officialInstallMarkerPresent(agentID, root) {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InvalidRequest, agent.Name+" installation ownership could not be verified; refusing to remove it")
	}
	if agentID == "kimi-code" {
		for _, path := range []string{executable, filepath.Join(root, "updates")} {
			if err := os.RemoveAll(path); err != nil {
				return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentInstallFailed, "Unable to remove official files for "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
			}
		}
	} else if err := os.RemoveAll(root); err != nil {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentInstallFailed, "Unable to remove official files for "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return AgentUninstallResult{Agent: agentID, Package: agent.Package.Name, Command: "remove official installer files"}, nil
}

func officialInstallMarkerPresent(agentID, root string) bool {
	marker := filepath.Join(root, ".bootagent-install-marker")
	if _, err := os.Stat(marker); err == nil {
		return true
	}
	if agentID == "kimi-code" {
		markerPath := filepath.Join(root, "updates", "install.json")
		data, err := os.ReadFile(markerPath)
		if err != nil {
			return false
		}
		var marker struct {
			SHA256 string `json:"sha256"`
		}
		if json.Unmarshal(data, &marker) != nil {
			return false
		}
		if marker.SHA256 == "" {
			return true
		}
		binaryPath := filepath.Join(root, "kimi")
		contents, readErr := os.ReadFile(binaryPath)
		if readErr != nil {
			return false
		}
		return fmt.Sprintf("%x", sha256.Sum256(contents)) == strings.ToLower(marker.SHA256)
	}
	if agentID == "hermes" {
		_, err := os.Stat(filepath.Join(root, ".git"))
		return err == nil
	}
	return false
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isPermissionFailure(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "permission denied") || strings.Contains(value, "eacces") || strings.Contains(value, "access is denied")
}

func npmListLooksMissing(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "missing:") || strings.Contains(value, "not found") || strings.Contains(value, "cannot find module")
}
