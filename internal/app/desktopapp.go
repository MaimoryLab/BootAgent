package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	configWriter "github.com/MaimoryLab/BootAgent/internal/config"
	"github.com/MaimoryLab/BootAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/process"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
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
	// Edition distinguishes regional builds of one product, so the UI can label
	// them without the region being part of Name.
	Edition string `json:"edition,omitempty"`
	// ManualInstall reports that BootAgent can detect and configure this app but
	// cannot fetch it. Without this the UI offered "Install the official desktop
	// application" for an Agent whose install step only returns a download link,
	// which is a button that cannot do what it says.
	ManualInstall bool   `json:"manualInstall,omitempty"`
	Home          string `json:"home,omitempty"`
}

// DesktopAgentProfileResult is the non-secret result of applying a saved
// profile to a desktop Agent.
type DesktopAgentProfileResult struct {
	AgentID        string `json:"agent"`
	ProfileID      string `json:"profileId"`
	ProfileAgentID string `json:"profileAgentId"`
	Config         string `json:"config,omitempty"`
	Restart        string `json:"restart,omitempty"`
	Message        string `json:"message"`
}

// DesktopAgentActionResult reports a local installation action. Windows Store
// installation continues after its downloaded bootstrapper starts.
type DesktopAgentActionResult struct {
	Status        string             `json:"status"`
	Message       string             `json:"message"`
	RefreshNeeded bool               `json:"refreshNeeded"`
	App           DesktopAgentStatus `json:"app"`
}

func (u *UseCases) desktopAgentStatuses(ctx context.Context) []DesktopAgentStatus {
	definitions := desktopapp.Definitions()
	result := make([]DesktopAgentStatus, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, u.publicDesktopAgentStatus(desktopapp.Inspect(ctx, definition.ID, u.desktopAppOptions(nil))))
	}
	return result
}

// DesktopAgentStatus returns one explicitly selected desktop Agent without changing config files.
func (u *UseCases) DesktopAgentStatus(ctx context.Context, agentID string) (DesktopAgentStatus, error) {
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
	return u.publicDesktopAgentStatus(desktopapp.Inspect(ctx, agentID, u.desktopAppOptions(nil))), nil
}

// InstallDesktopAgent downloads and installs the current desktop agent on
// macOS or starts its downloaded official bootstrapper on Windows. It never
// writes shared Agent configuration; configuration remains a separate action.
func (u *UseCases) InstallDesktopAgent(ctx context.Context, agentID string, output process.OutputListener) (DesktopAgentActionResult, error) {
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
	unlockTask := u.lockTask("install-desktop:" + agentID)
	defer unlockTask()
	if output != nil {
		base := output
		output = func(event process.Output) { event.Agent = agentID; base(event) }
	}
	result, err := desktopapp.Install(ctx, agentID, u.desktopAppOptions(output))
	if err != nil {
		return DesktopAgentActionResult{}, desktopAppInstallError(err)
	}
	return u.publicDesktopAgentAction(result), nil
}

// OpenDesktopAgent launches the already installed app and leaves shared Agent
// configuration untouched.
func (u *UseCases) OpenDesktopAgent(ctx context.Context, agentID string) error {
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
	if err := desktopapp.Open(ctx, agentID, u.desktopAppOptions(nil)); err != nil {
		return oneerrors.New(oneerrors.InternalError, "Cannot open desktop agent", oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

// ConfigureDesktopAgent applies a saved profile without accepting a secret
// from the desktop-specific UI.
func (u *UseCases) ConfigureDesktopAgent(ctx context.Context, agentID, profileID string) (DesktopAgentProfileResult, error) {
	if u == nil {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InternalError, "Desktop agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Desktop agent configuration request was cancelled"); err != nil {
		return DesktopAgentProfileResult{}, err
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, "desktop agent and profile are required")
	}
	agentID, err := knownDesktopAgentID(agentID)
	if err != nil {
		return DesktopAgentProfileResult{}, err
	}
	definition, _ := desktopapp.DefinitionFor(agentID)
	var selected profileStore.Profile
	profiles, err := u.profiles.List()
	if err != nil {
		return DesktopAgentProfileResult{}, err
	}
	for _, candidate := range profiles {
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
	if definition.Protocol != "" && selected.Protocol != definition.Protocol {
		return DesktopAgentProfileResult{}, oneerrors.New(oneerrors.InvalidRequest, definition.Name+" requires a "+provider.ProtocolLabel(definition.Protocol)+" Profile")
	}
	profileAgentID := definition.ProfileAgentID
	if definition.SharedConfigAgentID != "" {
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
			Message: definition.Name + " profile applied",
		}, nil
	}
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
	configPath, managed, err := u.writeDesktopAgentConfig(ctx, definition, target, model)
	if err != nil {
		return DesktopAgentProfileResult{}, err
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
		Message: definition.Name + " profile assigned",
	}
	if managed {
		result.Config = configPath
		result.Restart = "Restart " + definition.Name
		result.Message = definition.Name + " model applied"
	}
	return result, nil
}

func (u *UseCases) writeDesktopAgentConfig(ctx context.Context, definition desktopapp.Definition, target provider.Entry, model string) (string, bool, error) {
	if strings.TrimSpace(definition.ConfigAdapter) == "" || strings.TrimSpace(definition.ConfigPath) == "" {
		return "", false, nil
	}
	if u.status.Platform.OS != "macos" && u.status.Platform.OS != "windows" {
		return "", false, nil
	}
	if strings.TrimSpace(target.APIKey) == "" {
		return "", false, oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	path := filepath.Join(u.status.Home, filepath.FromSlash(definition.ConfigPath))
	writer := configWriter.NewWriter(u.status.Home, u.status.Platform.OS, u.filesystem)
	protocol := definition.Protocol
	if protocol == "" {
		protocol = provider.ProtocolForAdapter(definition.ConfigAdapter)
	}
	if err := writeManagedAgentConfig(ctx, writer, definition.ID, catalog.Agent{
		ConfigAdapter: definition.ConfigAdapter,
	}, path, dshRouteProviderID(target, ""), target.Name, target.BaseFor(protocol), target.APIKey, model, "", ""); err != nil {
		return "", false, err
	}
	return path, true, nil
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
	definition, ok := desktopapp.DefinitionFor(value.ID)
	if !ok {
		status.ProfileAgentID = value.ID
		return status
	}
	status.ProfileAgentID = definition.ProfileAgentID
	status.Protocol = definition.Protocol
	status.ManualInstall = definition.ManualInstall
	status.Home = definition.Home
	status.Edition = definition.Edition
	if binding, err := u.profiles.ReadAgentBinding(status.ProfileAgentID); err == nil && binding != nil && binding.ProfileRef != "" {
		status.ProfileID = nonEmptyPointer(binding.ProfileRef)
	}
	if definition.SharedConfigAgentID != "" {
		if manifest, err := catalog.LoadEmbedded(); err == nil {
			shared := manifest.Agents[definition.SharedConfigAgentID]
			status.ConfigPath = configPath(u.status.Home, u.status.Platform.OS, shared)
			status.ConfigSharedWith = shared.Name
			status.Protocol = provider.ProtocolForAdapter(shared.ConfigAdapter)
		}
	} else if definition.ConfigPath != "" && value.Supported {
		status.ConfigPath = filepath.Join(u.status.Home, filepath.FromSlash(definition.ConfigPath))
	}
	return status
}

func knownDesktopAgentID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if _, ok := desktopapp.DefinitionFor(value); ok {
		return value, nil
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
