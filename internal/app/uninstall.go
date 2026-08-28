package app

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// UninstallAgent removes one npm-managed Agent executable. User-owned state is
// deliberately outside this operation: only the catalog package is passed to
// npm in the managed runtime environment.
func (u *UseCases) UninstallAgent(ctx context.Context, agentID string, listeners ...process.OutputListener) (AgentUninstallResult, error) {
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
	if !ok || agent.Package == nil || agent.Package.Manager != "npm" {
		return AgentUninstallResult{}, oneerrors.New(oneerrors.InvalidRequest, "Agent is not npm-managed: "+agentID)
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
	if agent.Command != "" {
		if executable, installed := runtime.Runner.LookPath(agent.Command); !installed || executable == "" {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentPackageMissing, agent.Name+" is not installed")
		}
	}

	environment := install.NPMEnvironment(runtime, npm, "")
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
		if npmListLooksMissing(diagnostic) && agent.Command != "" {
			return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMMismatch, agent.Name+" is installed by a different Node/npm environment; activate its original npm before uninstalling", oneerrors.WithRetryable(false))
		}
		return AgentUninstallResult{}, oneerrors.New(oneerrors.AgentNPMFailed, "npm failed while checking the installation for "+agent.Name, oneerrors.WithStatus(500), oneerrors.WithRetryable(true))
	}

	// npm 6 and earlier run package-controlled uninstall lifecycle scripts. They
	// are unnecessary for removing a CLI package and could mutate user-owned
	// configuration that this operation promises to preserve.
	args := []string{npm, "uninstall", "-g", "--ignore-scripts", agent.Package.Name}
	result, err := runtime.Run(ctx, args, environment, install.DefaultCommandTimeout)
	if err != nil {
		if isPermissionFailure(err.Error()) {
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

func isPermissionFailure(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "permission denied") || strings.Contains(value, "eacces") || strings.Contains(value, "access is denied")
}

func npmListLooksMissing(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "missing:") || strings.Contains(value, "not found") || strings.Contains(value, "cannot find module")
}
