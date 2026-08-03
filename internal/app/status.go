// Package app contains transport-independent use cases shared by the desktop
// binding and the headless CLI.
package app

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	configReader "github.com/MaimoryLab/OneAgent/internal/config"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/install"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

type CommandLookup func(string) (string, bool)

type StatusOptions struct {
	Home     string
	Platform platform.Info
	Lookup   CommandLookup
	// FileSystem is optional and exists for tests or platform-specific hosts
	// that need to inject ACL behavior. Production callers use the default
	// securefs implementation for the selected platform.
	FileSystem  *securefs.Store
	Runner      process.Runner
	Environment map[string]string
}

type UseCases struct {
	status      StatusOptions
	provider    *provider.Client
	providers   provider.Store
	profiles    profileStore.Store
	filesystem  securefs.Store
	runner      process.Runner
	environment map[string]string
	// httpDoer is only used for runtime archive downloads. It is injectable so
	// bootstrap behavior is testable without reaching nodejs.org.
	httpDoer install.Doer
	writeMu  sync.Mutex
	// The region behind the default download host cannot change without the user
	// changing a system setting, so a successful probe is remembered for the
	// process. regionKnown is separate from the answer so a probe that failed is
	// retried instead of pinning "not China" for the session.
	regionMu        sync.Mutex
	regionKnown     bool
	regionIsChinese bool
}

// SetRuntimeDownloader overrides the HTTP client used for runtime archive
// downloads. It exists for tests and for hosts that must route downloads
// through their own transport.
func (u *UseCases) SetRuntimeDownloader(client install.Doer) {
	if u != nil {
		u.httpDoer = client
	}
}

func NewUseCases(options StatusOptions) *UseCases {
	return NewUseCasesWithProviderClient(options, nil)
}

// NewUseCasesWithProviderClient keeps network access injectable so Go behavior
// can be verified with fake transports.
func NewUseCasesWithProviderClient(options StatusOptions, client *provider.Client) *UseCases {
	return newUseCases(options, client, profileStore.Store{})
}

func NewUseCasesWithDependencies(options StatusOptions, client *provider.Client, profiles profileStore.Store) *UseCases {
	return newUseCases(options, client, profiles)
}

func newUseCases(options StatusOptions, client *provider.Client, profiles profileStore.Store) *UseCases {
	if options.Platform.OS == "" {
		options.Platform = platform.Current()
	}
	if options.Home == "" {
		options.Home = platform.ResolveHome(nil, options.Platform.OS)
	}
	// Lookup stays nil unless a caller injected one. A default here would resolve
	// against the OneAgent process PATH, which disagrees with the environment
	// installs and version probes actually run in: a managed runtime and any
	// Agent CLI under the managed global prefix would be reported missing.
	// runtimeCapability supplies the managed-PATH lookup instead.
	if options.Lookup == nil && options.Runner != nil {
		options.Lookup = options.Runner.LookPath
	}
	if client == nil {
		client = provider.NewClient(nil)
	}
	runner := options.Runner
	if runner == nil {
		current := process.Current()
		runner = current
		if options.Environment == nil {
			options.Environment = current.Env
		}
		// Only the real runner is logged. A caller that injected a runner either
		// is a test or has its own record, and writing a log from a test would
		// put files in a temp home nobody reads.
		runner = process.LoggingRunner{Inner: runner, Dir: CommandLogDir(options.Home)}
	}
	if options.Environment == nil {
		options.Environment = map[string]string{}
	}
	if profiles.Home == "" {
		profiles = profileStore.NewStore(options.Home, options.Platform.OS)
	}
	filesystem := securefs.New(securefs.Options{OS: options.Platform.OS})
	if options.FileSystem != nil {
		filesystem = *options.FileSystem
		profiles.FS = &filesystem
	} else if profiles.FS != nil {
		// Reuse an injected profile filesystem so one operation has one
		// security policy for profile, env, and Agent config writes.
		filesystem = *profiles.FS
	}
	return &UseCases{
		status:      options,
		provider:    client,
		providers:   provider.NewStore(options.Home, filesystem),
		profiles:    profiles,
		filesystem:  filesystem,
		runner:      runner,
		environment: cloneEnvironment(options.Environment),
	}
}

// CommandLogDir holds one log file per day recording every subprocess OneAgent
// runs. The desktop build is a GUI process with no console, so these files are
// the only place a failing npm, uv or launch command can be read back from.
func CommandLogDir(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".oneagent", "logs")
}

func cloneEnvironment(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	maps.Copy(result, source)
	return result
}

func NewUseCasesFromEnvironment() *UseCases {
	info := platform.Current()
	return NewUseCases(StatusOptions{
		Home:     platform.ResolveHome(nil, info.OS),
		Platform: info,
	})
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
	// FirstRun reports that ~/.oneagent does not exist yet, which is the signal
	// the UI uses to open onboarding instead of the overview. Agent detection is
	// not a substitute: an Agent installed before OneAgent would suppress it.
	FirstRun bool `json:"firstRun"`
	Runtimes         []RuntimeStatus             `json:"runtimes"`
	Environment      any                         `json:"environment"`
	EnvironmentError *string                     `json:"environmentError"`
}

type Capabilities struct {
	CanInstall map[string]bool `json:"canInstall"`
	// MissingRuntime names the runtime an Agent needs before it can be
	// installed, keyed by Agent id. An entry means canInstall is false only
	// because a bootstrappable runtime is absent, which the UI turns into a
	// "install the runtime first" prompt rather than a dead end.
	MissingRuntime    map[string]string `json:"missingRuntime"`
	SupportedAgentIDs []string          `json:"supportedAgentIds"`
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
	ProfileID     *string         `json:"profileId"`
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
		"profile": filepath.Join(options.Home, ".oneagent", "profile.json"),
	}
	capabilities := Capabilities{
		CanInstall:        make(map[string]bool, len(manifest.Agents)),
		MissingRuntime:    make(map[string]string, len(manifest.Agents)),
		SupportedAgentIDs: make([]string, 0, len(manifest.Agents)),
	}
	// Runtime state is read once: it decides both the reported runtime list and
	// whether a missing package manager is bootstrappable or a hard blocker. Its
	// resolver is also how Agent commands are found, so a CLI installed into the
	// managed global prefix reports as installed instead of missing.
	runtimes, runtimeLookup := u.runtimeCapability(ctx)
	agentLookup := runtimeLookup.lookup
	statuses := make(map[string]AgentStatus, len(manifest.Agents))
	bindings := u.profiles.ListAgentBindings()
	for _, id := range catalog.AgentIDs(manifest) {
		agent := manifest.Agents[id]
		configPath := configPath(options.Home, options.Platform.OS, agent)
		if configPath != "" {
			paths[id+"_config"] = configPath
		}
		installed := false
		executable := ""
		if agent.Command != "" {
			executable, installed = agentLookup(agent.Command)
		}
		canInstall := false
		if agent.Package != nil {
			manager := agent.Package.Manager
			canInstall = runtimeLookup.available(manager)
			if !canInstall {
				if runtimeID, bootstrappable := runtimeLookup.provider(manager); bootstrappable {
					capabilities.MissingRuntime[id] = runtimeID
				}
			}
		}
		if options.Platform.OS == "windows" {
			for _, prerequisite := range agent.WindowsPrerequisites {
				canInstall = canInstall && runtimeLookup.present(prerequisite)
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
		var boundProvider, boundProfileID, boundModel, boundBaseURL, boundUpdatedAt *string
		if binding, ok := bindings[id]; ok {
			boundProvider = nonEmptyPointer(binding.Provider)
			boundProfileID = nonEmptyPointer(binding.ProfileRef)
			boundModel = nonEmptyPointer(binding.Model)
			boundBaseURL = nonEmptyPointer(binding.BaseURL)
			boundUpdatedAt = nonEmptyPointer(binding.UpdatedAt)
		}
		var detected *DetectedConfig
		if agent.ConfigMode == "auto" && configPath != "" {
			detected = detectedConfig(configReader.DetectFile(configPath, agent.ConfigAdapter, agent.EnvVars))
		}
		var installedVersion *string
		if installed && agent.ConfigMode == "auto" {
			installedVersion = u.installedVersion(ctx, executable, agent.VersionArgs)
		}
		statuses[id] = AgentStatus{
			Installed:     installed,
			Configured:    fileExists(configPath),
			GuideOnly:     agent.ConfigMode == "guide",
			Config:        configPath,
			Version:       installedVersion,
			LockedVersion: lockedVersion,
			CanInstall:    canInstall,
			Provider:      boundProvider,
			ProfileID:     boundProfileID,
			Model:         boundModel,
			BaseURL:       boundBaseURL,
			UpdatedAt:     boundUpdatedAt,
			Detected:      detected,
		}
	}
	providers, err := u.providers.Public()
	if err != nil {
		return StatusResponse{}, err
	}
	profiles, activeProfile, environment, environmentError := u.profileStatus(ctx)
	return StatusResponse{
		APIVersion:       1,
		Platform:         options.Platform,
		Capabilities:     capabilities,
		Agents:           statuses,
		Catalog:          catalog.PublicCatalog(manifest, options.Platform.OS),
		Groups:           catalog.Groups(),
		Providers:        providers,
		Mirrors:          catalog.Mirrors(),
		Paths:            paths,
		Backups:          backupState(options.Home, options.Platform.OS, manifest),
		Profiles:         profiles,
		ActiveProfile:    activeProfile,
		Runtimes:         runtimes,
		Environment:      environment,
		EnvironmentError: environmentError,
		FirstRun:         !fileExists(filepath.Join(options.Home, ".oneagent")),
	}, nil
}

var versionPattern = regexp.MustCompile(`(^|[^\d])(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)`)

// installedVersion mirrors the legacy installed_version(): run the Agent's
// version command and take the first semver-looking token from either stream.
// Any failure means "unknown", never an error — status must not break because
// an Agent's --version is misbehaving.
func (u *UseCases) installedVersion(ctx context.Context, executable string, versionArgs []string) *string {
	if executable == "" || u.runner == nil {
		return nil
	}
	args := versionArgs
	if len(args) == 0 {
		args = []string{"--version"}
	}
	result, err := u.runner.Run(ctx, append([]string{executable}, args...), nil, 30*time.Second)
	if err != nil {
		return nil
	}
	match := versionPattern.FindStringSubmatch(result.Stdout + "\n" + result.Stderr)
	if match == nil {
		return nil
	}
	return &match[2]
}

func detectedConfig(value *configReader.Detected) *DetectedConfig {
	if value == nil {
		return nil
	}
	return &DetectedConfig{
		BaseURL:           value.BaseURL,
		Model:             value.Model,
		ManagedByOneAgent: value.ManagedByOneAgent,
		Unreadable:        value.Unreadable,
	}
}

func nonEmptyPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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

// ListAgentBindings returns the public per-Agent routing records used by the
// CLI's `agent list` command. Binding files contain no credential material.
func (u *UseCases) ListAgentBindings(ctx context.Context) (map[string]profileStore.AgentBinding, error) {
	if u == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx, "Agent listing request was cancelled"); err != nil {
		return nil, err
	}
	return u.profiles.ListAgentBindings(), nil
}

type SaveProfileOptions struct {
	ID         string
	Label      string
	Provider   string
	APIBaseURL string
	APIKey     string
	Model      string
	ConfigMode string
	AgentIDs   []string
}

func (u *UseCases) SaveProfile(ctx context.Context, options SaveProfileOptions) (ProfileSummary, error) {
	if u == nil {
		return ProfileSummary{}, oneerrors.New(oneerrors.InternalError, "Profile service is not configured", oneerrors.WithStatus(501))
	}
	if err := ctx.Err(); err != nil {
		return ProfileSummary{}, oneerrors.New(oneerrors.Timeout, "Profile request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	target, err := u.providers.Resolve(options.Provider, options.APIBaseURL)
	if err != nil {
		return ProfileSummary{}, err
	}
	if options.APIKey == "" {
		preserveStoredKey := false
		for _, item := range u.profiles.List() {
			if item.ID == options.ID && item.Provider == options.Provider {
				preserveStoredKey = true
				break
			}
		}
		if preserveStoredKey {
			storedKey, readErr := u.profiles.ReadSecret(ctx, options.ID)
			if readErr != nil {
				return ProfileSummary{}, readErr
			}
			preserveStoredKey = storedKey != ""
		}
		if !preserveStoredKey {
			options.APIKey = target.APIKey
		}
	}
	if options.APIKey != "" {
		if err := u.providers.SaveKey(ctx, options.Provider, options.APIKey); err != nil {
			return ProfileSummary{}, err
		}
	}
	stored, err := u.profiles.Save(ctx, profileStore.SaveRequest{
		ID:         options.ID,
		Label:      options.Label,
		Provider:   options.Provider,
		BaseURL:    target.BaseURL,
		APIKey:     options.APIKey,
		Model:      options.Model,
		ConfigMode: options.ConfigMode,
		AgentIDs:   append([]string(nil), options.AgentIDs...),
	})
	if err != nil {
		return ProfileSummary{}, err
	}
	return profileSummary(stored), nil
}

func (u *UseCases) profileStatus(ctx context.Context) ([]ProfileSummary, *string, any, *string) {
	// A v1 read can perform the one-time migration. Serialize that path with
	// profile writes so the old pointer is backed up before another operation
	// can publish a new profile state.
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	active := u.profiles.LoadActiveContext(ctx)
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
		result = append(result, profileSummary(item))
	}
	return result
}

func profileSummary(item profileStore.Profile) ProfileSummary {
	summary := item.Summary()
	return ProfileSummary{
		ID:          summary.ID,
		Label:       summary.Label,
		Provider:    summary.Provider,
		BaseURL:     summary.BaseURL,
		Model:       summary.Model,
		AgentIDs:    summary.AgentIDs,
		ActivatedAt: summary.ActivatedAt,
		HasKey:      summary.HasKey,
	}
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
