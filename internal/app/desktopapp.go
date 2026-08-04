package app

import (
	"context"
	"errors"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

// DesktopAgentStatus is the public projection of the current desktop agent. It is
// deliberately separate from AgentStatus: the app and Codex CLI share config,
// but they have different installation and version contracts.
type DesktopAgentStatus struct {
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

// DesktopAgentActionResult reports a local install or an external official
// installer launch. Windows Store installation continues outside OneAgent.
type DesktopAgentActionResult struct {
	Status        string             `json:"status"`
	Message       string             `json:"message"`
	RefreshNeeded bool               `json:"refreshNeeded"`
	App           DesktopAgentStatus `json:"app"`
}

func (u *UseCases) desktopAgentStatus(ctx context.Context) DesktopAgentStatus {
	return publicDesktopAgentStatus(desktopapp.Inspect(ctx, u.desktopAppOptions()))
}

// DesktopAgentStatus returns the current desktop agent state without changing config files.
func (u *UseCases) DesktopAgentStatus(ctx context.Context) (DesktopAgentStatus, error) {
	if u == nil {
		return DesktopAgentStatus{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app status request was cancelled"); err != nil {
		return DesktopAgentStatus{}, err
	}
	return u.desktopAgentStatus(ctx), nil
}

// InstallDesktopAgent installs the current desktop agent on macOS or opens the
// official Store bootstrapper on Windows. It never writes ~/.codex;
// configuration is a separate, explicit Codex action handled by the existing writer.
func (u *UseCases) InstallDesktopAgent(ctx context.Context) (DesktopAgentActionResult, error) {
	if u == nil {
		return DesktopAgentActionResult{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app installation request was cancelled"); err != nil {
		return DesktopAgentActionResult{}, err
	}
	result, err := desktopapp.Install(ctx, u.desktopAppOptions())
	if err != nil {
		return DesktopAgentActionResult{}, desktopAppInstallError(err)
	}
	return publicDesktopAgentAction(result), nil
}

// OpenDesktopAgent launches the already installed app and leaves its shared Codex
// configuration untouched.
func (u *UseCases) OpenDesktopAgent(ctx context.Context) error {
	if u == nil {
		return oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app launch request was cancelled"); err != nil {
		return err
	}
	if err := desktopapp.Open(ctx, u.desktopAppOptions()); err != nil {
		return oneerrors.New(oneerrors.InternalError, "Cannot open ChatGPT Desktop", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

// OpenDesktopAgentInstaller opens the official distribution source. This is the
// update path because the vendor does not publish a stable latest-version API.
func (u *UseCases) OpenDesktopAgentInstaller(ctx context.Context) (DesktopAgentActionResult, error) {
	if u == nil {
		return DesktopAgentActionResult{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app installer request was cancelled"); err != nil {
		return DesktopAgentActionResult{}, err
	}
	result, err := desktopapp.OpenInstaller(ctx, u.desktopAppOptions())
	if err != nil {
		return DesktopAgentActionResult{}, desktopAppInstallError(err)
	}
	return publicDesktopAgentAction(result), nil
}

func (u *UseCases) desktopAppOptions() desktopapp.Options {
	return desktopapp.Options{
		Home:     u.status.Home,
		Platform: u.status.Platform,
		Runner:   u.runner,
	}
}

func publicDesktopAgentStatus(value desktopapp.Status) DesktopAgentStatus {
	return DesktopAgentStatus{
		ID:                    value.ID,
		Name:                  value.Name,
		Installed:             value.Installed,
		Supported:             value.Supported,
		Path:                  value.Path,
		Version:               value.Version,
		Source:                value.Source,
		PackageFamily:         value.PackageFamily,
		InspectionUnavailable: value.InspectionUnavailable,
	}
}

func publicDesktopAgentAction(value desktopapp.ActionResult) DesktopAgentActionResult {
	return DesktopAgentActionResult{
		Status:        value.Status,
		Message:       value.Message,
		RefreshNeeded: value.RefreshNeeded,
		App:           publicDesktopAgentStatus(value.App),
	}
}

func desktopAppInstallError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return oneerrors.New(oneerrors.Timeout, "Desktop app installation was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "Cannot install ChatGPT Desktop"
	}
	return oneerrors.New(oneerrors.AgentInstallFailed, message, oneerrors.WithRetryable(true), oneerrors.WithCause(err))
}
