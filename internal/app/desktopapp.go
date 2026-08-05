package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	configWriter "github.com/MaimoryLab/OneAgent/internal/config"
	"github.com/MaimoryLab/OneAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/process"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
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
	Protocol              string  `json:"protocol,omitempty"`
	ProfileAgentID        string  `json:"profileAgentId"`
	ProfileID             *string `json:"profileId"`
	PackageFamily         string  `json:"packageFamily,omitempty"`
	InspectionUnavailable *string `json:"inspectionUnavailable,omitempty"`
}

// DesktopAgentProfileResult is the non-secret result of applying a saved
// profile to a desktop Agent. ChatGPT Desktop writes Codex configuration and
// WorkBuddy writes ~/.workbuddy/models.json; other desktop IDs only record
// their own profile membership.
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

func (u *UseCases) desktopAgentStatuses(ctx context.Context) []DesktopAgentStatus {
	if u.status.Platform.OS != "macos" && u.status.Platform.OS != "windows" {
		return nil
	}
	result := make([]DesktopAgentStatus, 0, len(desktopapp.IDs()))
	for _, agentID := range desktopapp.IDs() {
		result = append(result, u.publicDesktopAgentStatus(desktopapp.Inspect(ctx, u.desktopAppOptionsFor(agentID, nil))))
	}
	return result
}

// DesktopAgentStatus returns the current desktop agent state without changing config files.
func (u *UseCases) DesktopAgentStatus(ctx context.Context) (DesktopAgentStatus, error) {
	return u.DesktopAgentStatusFor(ctx, desktopapp.ID)
}

func (u *UseCases) DesktopAgentStatusFor(ctx context.Context, agentID string) (DesktopAgentStatus, error) {
	if u == nil {
		return DesktopAgentStatus{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app status request was cancelled"); err != nil {
		return DesktopAgentStatus{}, err
	}
	agentID, err := knownDesktopAgentID(agentID)
	if err != nil {
		return DesktopAgentStatus{}, err
	}
	return u.publicDesktopAgentStatus(desktopapp.Inspect(ctx, u.desktopAppOptionsFor(agentID, nil))), nil
}

// InstallDesktopAgent downloads and installs the current desktop agent on
// macOS or starts its downloaded official bootstrapper on Windows. It never
// writes shared Agent configuration; configuration remains a separate action.
func (u *UseCases) InstallDesktopAgent(ctx context.Context, output process.OutputListener) (DesktopAgentActionResult, error) {
	return u.InstallDesktopAgentFor(ctx, desktopapp.ID, output)
}

func (u *UseCases) InstallDesktopAgentFor(ctx context.Context, agentID string, output process.OutputListener) (DesktopAgentActionResult, error) {
	if u == nil {
		return DesktopAgentActionResult{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app installation request was cancelled"); err != nil {
		return DesktopAgentActionResult{}, err
	}
	agentID, err := knownDesktopAgentID(agentID)
	if err != nil {
		return DesktopAgentActionResult{}, err
	}
	result, err := desktopapp.Install(ctx, u.desktopAppOptionsFor(agentID, output))
	if err != nil {
		return DesktopAgentActionResult{}, desktopAppInstallError(err)
	}
	return u.publicDesktopAgentAction(result), nil
}

// OpenDesktopAgent launches the already installed app and leaves shared Agent
// configuration untouched.
func (u *UseCases) OpenDesktopAgent(ctx context.Context) error {
	return u.OpenDesktopAgentFor(ctx, desktopapp.ID)
}

func (u *UseCases) OpenDesktopAgentFor(ctx context.Context, agentID string) error {
	if u == nil {
		return oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app launch request was cancelled"); err != nil {
		return err
	}
	agentID, err := knownDesktopAgentID(agentID)
	if err != nil {
		return err
	}
	if err := desktopapp.Open(ctx, u.desktopAppOptionsFor(agentID, nil)); err != nil {
		return oneerrors.New(oneerrors.InternalError, "Cannot open desktop agent", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

// OpenDesktopAgentInstaller downloads the official package and runs the update
// path without opening its URL in a browser.
func (u *UseCases) OpenDesktopAgentInstaller(ctx context.Context, output process.OutputListener) (DesktopAgentActionResult, error) {
	return u.OpenDesktopAgentInstallerFor(ctx, desktopapp.ID, output)
}

func (u *UseCases) OpenDesktopAgentInstallerFor(ctx context.Context, agentID string, output process.OutputListener) (DesktopAgentActionResult, error) {
	if u == nil {
		return DesktopAgentActionResult{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop app installer request was cancelled"); err != nil {
		return DesktopAgentActionResult{}, err
	}
	agentID, err := knownDesktopAgentID(agentID)
	if err != nil {
		return DesktopAgentActionResult{}, err
	}
	result, err := desktopapp.OpenInstaller(ctx, u.desktopAppOptionsFor(agentID, output))
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
	if selected.Protocol == "" {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, "Profile has no API mode")
	}
	if agentID == desktopapp.WorkBuddyID && selected.Protocol != provider.ProtocolOpenAI {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, "WorkBuddy requires an OpenAI-compatible Profile")
	}
	profileAgentID := desktopapp.ProfileAgentID(agentID)
	if desktopapp.SharesProfile(agentID) {
		result, err := u.ActivateAgent(ctx, ActivateAgentOptions{
			AgentID:  profileAgentID,
			Provider: selected.Provider,
			// The endpoint belongs to the Provider and must not be overridden by
			// the profile snapshot.
			APIBaseURL: "",
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
	// The endpoint belongs to the Provider; ignore the profile snapshot.
	target, err := u.providers.Resolve(selected.Provider, "")
	if err != nil {
		return DesktopAgentProfileResult{}, err
	}
	model := stringPointerValue(selected.Model)
	if strings.TrimSpace(model) == "" || strings.TrimSpace(target.BaseURL) == "" {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, "Desktop Agent profile has no provider or model")
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	configPath := ""
	workBuddyConfig := agentID == desktopapp.WorkBuddyID && (u.status.Platform.OS == "macos" || u.status.Platform.OS == "windows")
	if workBuddyConfig {
		configPath, err = u.writeWorkBuddyConfig(ctx, target, model)
		if err != nil {
			return DesktopAgentProfileResult{}, err
		}
	}
	_, err = u.profiles.WriteAgentBinding(ctx, agentID, profileStore.BindingWriteRequest{
		Provider:   target.ID,
		BaseURL:    target.BaseURL,
		Model:      model,
		ProfileRef: profileID,
	})
	if err != nil {
		return DesktopAgentProfileResult{}, err
	}
	result := DesktopAgentProfileResult{
		AgentID: agentID, ProfileID: profileID, ProfileAgentID: profileAgentID,
		Message: "Desktop Agent profile assigned",
	}
	if workBuddyConfig {
		result.Config = configPath
		result.Restart = "Restart WorkBuddy"
		result.Message = "WorkBuddy model applied"
	}
	return result, nil
}

func (u *UseCases) writeWorkBuddyConfig(ctx context.Context, target provider.Entry, model string) (string, error) {
	if strings.TrimSpace(target.APIKey) == "" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	path := filepath.Join(u.status.Home, ".workbuddy", "models.json")
	writer := configWriter.NewWriter(u.status.Home, u.status.Platform.OS, u.filesystem)
	if err := writer.WriteWorkBuddy(ctx, path, target.BaseFor(provider.ProtocolOpenAI), target.APIKey, model); err != nil {
		return "", err
	}
	return path, nil
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (u *UseCases) desktopAppOptions(output process.OutputListener) desktopapp.Options {
	return u.desktopAppOptionsFor(desktopapp.ID, output)
}

func (u *UseCases) desktopAppOptionsFor(agentID string, output process.OutputListener) desktopapp.Options {
	return desktopapp.Options{
		AppID:      agentID,
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
	}
	if desktopapp.SharesProfile(value.ID) {
		if manifest, err := catalog.LoadEmbedded(); err == nil {
			shared := manifest.Agents[desktopapp.SharedConfigAgentID]
			status.ConfigPath = configPath(u.status.Home, u.status.Platform.OS, shared)
			status.ConfigSharedWith = shared.Name
			if u.status.Platform.OS == "macos" || u.status.Platform.OS == "windows" {
				status.Protocol = provider.ProtocolForAdapter(shared.ConfigAdapter)
			}
		}
	} else if value.ID == desktopapp.WorkBuddyID && (u.status.Platform.OS == "macos" || u.status.Platform.OS == "windows") {
		status.ConfigPath = filepath.Join(u.status.Home, ".workbuddy", "models.json")
		status.Protocol = provider.ProtocolOpenAI
	}
	return status
}

func knownDesktopAgentID(value string) (string, error) {
	value = strings.TrimSpace(value)
	for _, agentID := range desktopapp.IDs() {
		if value == agentID {
			return value, nil
		}
	}
	return "", oneerrors.New(oneerrors.InvalidRequest, "Unknown desktop Agent: "+value)
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
