package app

import (
	"context"
	"errors"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

// ChatGPTAppStatus is the public projection of the official desktop app. It is
// deliberately separate from AgentStatus: the app and Codex CLI share config,
// but they have different installation and version contracts.
type ChatGPTAppStatus struct {
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

// ChatGPTAppActionResult reports a local install or an external official
// installer launch. Windows Store installation continues outside OneAgent.
type ChatGPTAppActionResult struct {
	Status        string           `json:"status"`
	Message       string           `json:"message"`
	RefreshNeeded bool             `json:"refreshNeeded"`
	App           ChatGPTAppStatus `json:"app"`
}

func (u *UseCases) chatGPTAppStatus(ctx context.Context) ChatGPTAppStatus {
	return publicChatGPTStatus(desktopapp.Inspect(ctx, u.desktopAppOptions()))
}

// ChatGPTAppStatus returns the current app state without changing config files.
func (u *UseCases) ChatGPTAppStatus(ctx context.Context) (ChatGPTAppStatus, error) {
	if u == nil {
		return ChatGPTAppStatus{}, oneerrors.New(oneerrors.InternalError, "Desktop app service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app status request was cancelled"); err != nil {
		return ChatGPTAppStatus{}, err
	}
	return u.chatGPTAppStatus(ctx), nil
}

// InstallChatGPTApp installs the official app on macOS or opens the official
// Store bootstrapper on Windows. It never writes ~/.codex; configuration is a
// separate, explicit Codex action handled by the existing writer.
func (u *UseCases) InstallChatGPTApp(ctx context.Context) (ChatGPTAppActionResult, error) {
	if u == nil {
		return ChatGPTAppActionResult{}, oneerrors.New(oneerrors.InternalError, "Desktop app service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app installation request was cancelled"); err != nil {
		return ChatGPTAppActionResult{}, err
	}
	result, err := desktopapp.Install(ctx, u.desktopAppOptions())
	if err != nil {
		return ChatGPTAppActionResult{}, desktopAppInstallError(err)
	}
	return publicChatGPTAction(result), nil
}

// OpenChatGPTApp launches the already installed app and leaves its shared Codex
// configuration untouched.
func (u *UseCases) OpenChatGPTApp(ctx context.Context) error {
	if u == nil {
		return oneerrors.New(oneerrors.InternalError, "Desktop app service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app launch request was cancelled"); err != nil {
		return err
	}
	if err := desktopapp.Open(ctx, u.desktopAppOptions()); err != nil {
		return oneerrors.New(oneerrors.InternalError, "Cannot open ChatGPT Desktop", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

// OpenChatGPTInstaller opens the official distribution source. This is the
// update path because the vendor does not publish a stable latest-version API.
func (u *UseCases) OpenChatGPTInstaller(ctx context.Context) (ChatGPTAppActionResult, error) {
	if u == nil {
		return ChatGPTAppActionResult{}, oneerrors.New(oneerrors.InternalError, "Desktop app service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app installer request was cancelled"); err != nil {
		return ChatGPTAppActionResult{}, err
	}
	result, err := desktopapp.OpenInstaller(ctx, u.desktopAppOptions())
	if err != nil {
		return ChatGPTAppActionResult{}, desktopAppInstallError(err)
	}
	return publicChatGPTAction(result), nil
}

func (u *UseCases) desktopAppOptions() desktopapp.Options {
	return desktopapp.Options{
		Home:     u.status.Home,
		Platform: u.status.Platform,
		Runner:   u.runner,
	}
}

func publicChatGPTStatus(value desktopapp.Status) ChatGPTAppStatus {
	return ChatGPTAppStatus{
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

func publicChatGPTAction(value desktopapp.ActionResult) ChatGPTAppActionResult {
	return ChatGPTAppActionResult{
		Status:        value.Status,
		Message:       value.Message,
		RefreshNeeded: value.RefreshNeeded,
		App:           publicChatGPTStatus(value.App),
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
