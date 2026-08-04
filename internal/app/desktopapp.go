package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/process"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
)

// DesktopAgentStatus is the public projection of the current desktop agent. It is
// deliberately separate from AgentStatus: desktop and command-line agents may
// share config, but they have different installation and version contracts.
type DesktopAgentStatus struct {
	ID                    string  `json:"id"`
	Name                  string  `json:"name"`
	Installed             bool    `json:"installed"`
	Supported             bool    `json:"supported"`
	Path                  string  `json:"path,omitempty"`
	Version               *string `json:"version"`
	Source                string  `json:"source"`
	ConfigPath            string  `json:"configPath,omitempty"`
	ConfigSharedWith      string  `json:"configSharedWith,omitempty"`
	ProfileAgentID        string  `json:"profileAgentId"`
	ProfileID             *string `json:"profileId"`
	PackageFamily         string  `json:"packageFamily,omitempty"`
	InspectionUnavailable *string `json:"inspectionUnavailable,omitempty"`
}

// DesktopAgentProfileResult is the non-secret result of applying a saved
// profile to a desktop Agent. ChatGPT Desktop returns the Codex config result;
// other desktop Agents only need their own profile membership recorded until
// their vendor-specific config writer is added.
type DesktopAgentProfileResult struct {
	AgentID        string `json:"agent"`
	ProfileID      string `json:"profileId"`
	ProfileAgentID string `json:"profileAgentId"`
	Config         string `json:"config,omitempty"`
	Restart        string `json:"restart,omitempty"`
	Message        string `json:"message"`
}

// DesktopAgentActionResult reports a local install or a downloaded installer
// launch. Windows Store installation continues after its bootstrapper starts.
type DesktopAgentActionResult struct {
	Status        string             `json:"status"`
	Message       string             `json:"message"`
	RefreshNeeded bool               `json:"refreshNeeded"`
	App           DesktopAgentStatus `json:"app"`
}

func (u *UseCases) desktopAgentStatus(ctx context.Context) DesktopAgentStatus {
	return u.publicDesktopAgentStatus(desktopapp.Inspect(ctx, u.desktopAppOptions(nil)))
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

// InstallDesktopAgent downloads and installs the current desktop agent on
// macOS or starts its downloaded official bootstrapper on Windows. It never
// writes shared Agent configuration; configuration remains a separate action.
func (u *UseCases) InstallDesktopAgent(ctx context.Context, output process.OutputListener) (DesktopAgentActionResult, error) {
	if u == nil {
		return DesktopAgentActionResult{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app installation request was cancelled"); err != nil {
		return DesktopAgentActionResult{}, err
	}
	result, err := desktopapp.Install(ctx, u.desktopAppOptions(output))
	if err != nil {
		return DesktopAgentActionResult{}, desktopAppInstallError(err)
	}
	return u.publicDesktopAgentAction(result), nil
}

// OpenDesktopAgent launches the already installed app and leaves shared Agent
// configuration untouched.
func (u *UseCases) OpenDesktopAgent(ctx context.Context) error {
	if u == nil {
		return oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app launch request was cancelled"); err != nil {
		return err
	}
	if err := desktopapp.Open(ctx, u.desktopAppOptions(nil)); err != nil {
		return oneerrors.New(oneerrors.InternalError, "Cannot open desktop agent", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

// OpenDesktopAgentInstaller downloads the official package and runs the update
// path without opening its URL in a browser.
func (u *UseCases) OpenDesktopAgentInstaller(ctx context.Context, output process.OutputListener) (DesktopAgentActionResult, error) {
	if u == nil {
		return DesktopAgentActionResult{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app installer request was cancelled"); err != nil {
		return DesktopAgentActionResult{}, err
	}
	result, err := desktopapp.OpenInstaller(ctx, u.desktopAppOptions(output))
	if err != nil {
		return DesktopAgentActionResult{}, desktopAppInstallError(err)
	}
	return u.publicDesktopAgentAction(result), nil
}

// ConfigureDesktopAgent applies a saved profile without accepting a secret
// from the desktop-specific UI. ChatGPT Desktop shares Codex's config writer;
// all other desktop IDs are recorded as owners of their own profile.
func (u *UseCases) ConfigureDesktopAgent(ctx context.Context, agentID, profileID string) (DesktopAgentProfileResult, error) {
	if u == nil {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop agent configuration request was cancelled"); err != nil {
		return DesktopAgentProfileResult{}, err
	}
	agentID = strings.TrimSpace(agentID)
	profileID = strings.TrimSpace(profileID)
	if agentID == "" || profileID == "" {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, "desktop agent and profile are required")
	}
	var selected profileStore.Profile
	for _, candidate := range u.profiles.List() {
		if candidate.ID == profileID {
			selected = candidate
			break
		}
	}
	if selected.ID == "" {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, "Profile not found: "+profileID)
	}
	profileAgentID := desktopapp.ProfileAgentID(agentID)
	assigned := slices.Contains(selected.AgentIDs, profileAgentID)
	if !assigned && len(selected.AgentIDs) == 0 {
		// Older profiles can omit AgentIDs while their per-Agent binding still
		// identifies the active profile. Treat that binding as authoritative so
		// the desktop and profile pages agree on what can be selected.
		binding, bindingErr := u.profiles.ReadAgentBinding(profileAgentID)
		assigned = bindingErr == nil && binding != nil && binding.ProfileRef == profileID
	}
	if !assigned {
		// A profile selected for a desktop Agent must explicitly own that Agent.
		// ChatGPT is the exception only in its *ID mapping*: its profile still
		// belongs to Codex, never to a synthetic desktop ID.
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, "Profile is not assigned to this desktop agent")
	}
	if desktopapp.SharesProfile(agentID) {
		result, err := u.ActivateAgent(ctx, ActivateAgentOptions{
			AgentID:    profileAgentID,
			Provider:   selected.Provider,
			APIBaseURL: stringPointerValue(selected.BaseURL),
			Model:      stringPointerValue(selected.Model),
			ProfileID:  profileID,
		})
		if err != nil {
			return DesktopAgentProfileResult{}, err
		}
		return DesktopAgentProfileResult{
			AgentID: agentID, ProfileID: profileID, ProfileAgentID: profileAgentID,
			Config: result.Config, Restart: result.Restart,
			Message: "Shared Codex profile applied",
		}, nil
	}
	// Desktop vendors without a OneAgent config adapter still get a durable
	// per-Agent selection. Their profile is ready for a future vendor writer,
	// and the overview can report the exact selected profile instead of relying
	// on directory order.
	target, err := u.providers.Resolve(selected.Provider, stringPointerValue(selected.BaseURL))
	if err != nil {
		return DesktopAgentProfileResult{}, err
	}
	model := stringPointerValue(selected.Model)
	if strings.TrimSpace(model) == "" || strings.TrimSpace(target.BaseURL) == "" {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, "Desktop Agent profile has no provider or model")
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	_, err = u.profiles.WriteAgentBinding(ctx, agentID, profileStore.BindingWriteRequest{
		Provider:   target.ID,
		BaseURL:    target.BaseURL,
		Model:      model,
		ProfileRef: profileID,
	})
	if err != nil {
		return DesktopAgentProfileResult{}, err
	}
	return DesktopAgentProfileResult{
		AgentID: agentID, ProfileID: profileID, ProfileAgentID: profileAgentID,
		Message: "Desktop Agent profile assigned",
	}, nil
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (u *UseCases) desktopAppOptions(output process.OutputListener) desktopapp.Options {
	return desktopapp.Options{
		Home:       u.status.Home,
		Platform:   u.status.Platform,
		Runner:     u.runner,
		Output:     output,
		Downloader: u.httpDoer,
	}
}

func (u *UseCases) publicDesktopAgentStatus(value desktopapp.Status) DesktopAgentStatus {
	status := DesktopAgentStatus{
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
	status.ProfileAgentID = desktopapp.ProfileAgentID(value.ID)
	if binding, err := u.profiles.ReadAgentBinding(status.ProfileAgentID); err == nil && binding != nil && binding.ProfileRef != "" {
		status.ProfileID = nonEmptyPointer(binding.ProfileRef)
	} else {
		for _, profile := range u.profiles.List() {
			if profile.ID != "" && slices.Contains(profile.AgentIDs, status.ProfileAgentID) {
				status.ProfileID = nonEmptyPointer(profile.ID)
				break
			}
		}
	}
	if desktopapp.SharesProfile(value.ID) {
		if manifest, err := catalog.LoadEmbedded(); err == nil {
			shared := manifest.Agents[desktopapp.SharedConfigAgentID]
			status.ConfigPath = configPath(u.status.Home, u.status.Platform.OS, shared)
			status.ConfigSharedWith = shared.Name
		}
	}
	return status
}

func (u *UseCases) publicDesktopAgentAction(value desktopapp.ActionResult) DesktopAgentActionResult {
	return DesktopAgentActionResult{
		Status:        value.Status,
		Message:       value.Message,
		RefreshNeeded: value.RefreshNeeded,
		App:           u.publicDesktopAgentStatus(value.App),
	}
}

func desktopAppInstallError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return oneerrors.New(oneerrors.Timeout, "Desktop app installation was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "Cannot install desktop agent"
	}
	return oneerrors.New(oneerrors.AgentInstallFailed, message, oneerrors.WithRetryable(true), oneerrors.WithCause(err))
}
