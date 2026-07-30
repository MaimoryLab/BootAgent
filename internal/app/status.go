// Package app contains transport-independent use cases. Status and Provider
// reads are the first migrated slices; installation and config writes remain
// in Python until their Go equivalents pass their own gates.
package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

type CommandLookup func(string) (string, bool)

type StatusOptions struct {
	Home     string
	Platform platform.Info
	Lookup   CommandLookup
}

type UseCases struct {
	status   StatusOptions
	provider *provider.Client
	profiles profileStore.Store
}

func NewUseCases(options StatusOptions) *UseCases {
	return NewUseCasesWithProviderClient(options, nil)
}

// NewUseCasesWithProviderClient keeps network access injectable while the
// Python production path remains active and Go behavior is verified with fake
// transports.
func NewUseCasesWithProviderClient(options StatusOptions, client *provider.Client) *UseCases {
	if options.Platform.OS == "" {
		options.Platform = platform.Current()
	}
	if options.Home == "" {
		options.Home = platform.ResolveHome(nil, options.Platform.OS)
	}
	if options.Lookup == nil {
		options.Lookup = defaultLookup
	}
	if client == nil {
		client = provider.NewClient(nil)
	}
	return &UseCases{
		status:   options,
		provider: client,
		profiles: profileStore.NewStore(options.Home, options.Platform.OS),
	}
}

func NewUseCasesFromEnvironment() *UseCases {
	info := platform.Current()
	return NewUseCases(StatusOptions{
		Home:     platform.ResolveHome(nil, info.OS),
		Platform: info,
	})
}

// LookupForCLI exposes only command presence to the compatibility command. It
// does not expose the injected resolver or any environment values.
func (u *UseCases) LookupForCLI(command string) (string, bool) {
	if u == nil || u.status.Lookup == nil {
		return "", false
	}
	return u.status.Lookup(command)
}

func defaultLookup(command string) (string, bool) {
	path, err := exec.LookPath(command)
	return path, err == nil
}

type StatusResponse struct {
	APIVersion       int                         `json:"apiVersion"`
	Platform         platform.Info               `json:"platform"`
	Capabilities     Capabilities                `json:"capabilities"`
	Agents           map[string]AgentStatus      `json:"agents"`
	Catalog          []catalog.CatalogItem       `json:"catalog"`
	Groups           []catalog.Group             `json:"groups"`
	Providers        map[string]catalog.Provider `json:"providers"`
	Mirrors          []catalog.Mirror            `json:"mirrors"`
	Paths            map[string]string           `json:"paths"`
	Backups          map[string]bool             `json:"backups"`
	Profiles         []ProfileSummary            `json:"profiles"`
	ActiveProfile    *string                     `json:"activeProfile"`
	Environment      any                         `json:"environment"`
	EnvironmentError *string                     `json:"environmentError"`
}

type Capabilities struct {
	CanInstall        map[string]bool `json:"canInstall"`
	SupportedAgentIDs []string        `json:"supportedAgentIds"`
}

type AgentStatus struct {
	Installed     bool            `json:"installed"`
	Configured    bool            `json:"configured"`
	GuideOnly     bool            `json:"guideOnly"`
	Config        string          `json:"config"`
	Version       *string         `json:"version"`
	LockedVersion *string         `json:"lockedVersion"`
	CanInstall    bool            `json:"canInstall"`
	Provider      *string         `json:"provider"`
	Model         *string         `json:"model"`
	BaseURL       *string         `json:"baseUrl"`
	UpdatedAt     *string         `json:"updatedAt"`
	Detected      *DetectedConfig `json:"detected"`
}

type DetectedConfig struct {
	BaseURL           string  `json:"baseUrl"`
	Model             string  `json:"model"`
	ManagedByOneAgent bool    `json:"managedByOneAgent"`
	Unreadable        *string `json:"unreadable"`
}

// ProfileSummary is intentionally a public projection. It has no credential
// field; hasKey only reports whether a secret exists in the secure store.
type ProfileSummary struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Provider    string   `json:"provider"`
	BaseURL     *string  `json:"baseUrl"`
	Model       *string  `json:"model"`
	AgentIDs    []string `json:"agentIds"`
	ActivatedAt *string  `json:"activatedAt"`
	HasKey      bool     `json:"hasKey"`
}

func (u *UseCases) GetStatus(ctx context.Context) (StatusResponse, error) {
	if u == nil {
		return StatusResponse{}, oneerrors.New(oneerrors.InternalError, "Status service is not configured", oneerrors.WithStatus(501))
	}
	if err := ctx.Err(); err != nil {
		return StatusResponse{}, oneerrors.New(oneerrors.Timeout, "Status request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return StatusResponse{}, err
	}
	options := u.status
	paths := map[string]string{
		"env_file": filepath.Join(options.Home, ".oneagent", envFilename(options.Platform.OS)),
		"profile":  filepath.Join(options.Home, ".oneagent", "profile.json"),
	}
	capabilities := Capabilities{
		CanInstall:        make(map[string]bool, len(manifest.Agents)),
		SupportedAgentIDs: make([]string, 0, len(manifest.Agents)),
	}
	statuses := make(map[string]AgentStatus, len(manifest.Agents))
	for _, id := range catalog.AgentIDs(manifest) {
		agent := manifest.Agents[id]
		configPath := configPath(options.Home, options.Platform.OS, agent)
		if configPath != "" {
			paths[id+"_config"] = configPath
		}
		installed := false
		if agent.Command != "" {
			_, installed = options.Lookup(agent.Command)
		}
		canInstall := false
		if agent.Package != nil {
			_, canInstall = options.Lookup(agent.Package.Manager)
			if agent.Package.Manager == "npm" {
				_, canInstall = options.Lookup("npm")
			} else if agent.Package.Manager == "uv" {
				_, canInstall = options.Lookup("uv")
			}
		}
		if options.Platform.OS == "windows" {
			for _, prerequisite := range agent.WindowsPrerequisites {
				_, present := options.Lookup(prerequisite)
				canInstall = canInstall && present
			}
		}
		if contains(agent.Platforms, options.Platform.OS) {
			capabilities.SupportedAgentIDs = append(capabilities.SupportedAgentIDs, id)
		}
		capabilities.CanInstall[id] = canInstall
		var lockedVersion *string
		if agent.Package != nil {
			version := agent.Package.Version
			lockedVersion = &version
		}
		statuses[id] = AgentStatus{
			Installed:     installed,
			Configured:    fileExists(configPath),
			GuideOnly:     agent.ConfigMode == "guide",
			Config:        configPath,
			LockedVersion: lockedVersion,
			CanInstall:    canInstall,
		}
	}
	profiles, activeProfile, environment, environmentError := u.profileStatus()
	return StatusResponse{
		APIVersion:       1,
		Platform:         options.Platform,
		Capabilities:     capabilities,
		Agents:           statuses,
		Catalog:          catalog.PublicCatalog(manifest, options.Platform.OS),
		Groups:           catalog.Groups(),
		Providers:        catalog.PublicProviders(),
		Mirrors:          catalog.Mirrors(),
		Paths:            paths,
		Backups:          backupState(options.Home, options.Platform.OS, manifest),
		Profiles:         profiles,
		ActiveProfile:    activeProfile,
		Environment:      environment,
		EnvironmentError: environmentError,
	}, nil
}

// ListProfiles returns only the public profile projection. Secret files are
// checked for existence by the profile store, but their contents never enter
// this use case or its transport DTOs.
func (u *UseCases) ListProfiles(ctx context.Context) ([]ProfileSummary, error) {
	if u == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Profile service is not configured", oneerrors.WithStatus(501))
	}
	if err := ctx.Err(); err != nil {
		return nil, oneerrors.New(oneerrors.Timeout, "Profile request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return u.profileSummaries(), nil
}

func (u *UseCases) profileStatus() ([]ProfileSummary, *string, any, *string) {
	active := u.profiles.LoadActive()
	// Load the active profile first so the status projection follows the same
	// read ordering as the legacy implementation when a v1 pointer is present.
	profiles := u.profileSummaries()
	var activeID *string
	if active.ID != "" {
		id := active.ID
		activeID = &id
	}
	var environment any
	if active.Environment != nil {
		environment = active.Environment
	}
	var environmentError *string
	if active.Error != "" {
		errorText := active.Error
		environmentError = &errorText
	}
	return profiles, activeID, environment, environmentError
}

func (u *UseCases) profileSummaries() []ProfileSummary {
	stored := u.profiles.List()
	result := make([]ProfileSummary, 0, len(stored))
	for _, item := range stored {
		summary := item.Summary()
		result = append(result, ProfileSummary{
			ID:          summary.ID,
			Label:       summary.Label,
			Provider:    summary.Provider,
			BaseURL:     summary.BaseURL,
			Model:       summary.Model,
			AgentIDs:    summary.AgentIDs,
			ActivatedAt: summary.ActivatedAt,
			HasKey:      summary.HasKey,
		})
	}
	return result
}

func envFilename(osID string) string {
	if osID == "windows" {
		return "env.ps1"
	}
	return "env"
}

func configPath(home, osID string, agent catalog.Agent) string {
	if agent.ConfigPath == "" {
		return ""
	}
	relative := agent.ConfigPath
	if osID == "windows" && agent.WindowsConfigPath != "" {
		relative = agent.WindowsConfigPath
	}
	return filepath.Join(home, filepath.FromSlash(relative))
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func backupState(home, osID string, manifest catalog.Manifest) map[string]bool {
	result := make(map[string]bool)
	for _, id := range catalog.AgentIDs(manifest) {
		agent := manifest.Agents[id]
		path := configPath(home, osID, agent)
		if path == "" {
			continue
		}
		matches, err := filepath.Glob(path + ".backup-*")
		result[id] = err == nil && len(matches) > 0
	}
	envMatches, err := filepath.Glob(filepath.Join(home, ".oneagent", "env.backup-*"))
	result["env"] = err == nil && len(envMatches) > 0
	profileMatches, err := filepath.Glob(filepath.Join(home, ".oneagent", "profile.json.backup-*"))
	result["profile"] = err == nil && len(profileMatches) > 0
	return result
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}
