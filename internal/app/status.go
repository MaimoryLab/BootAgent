// Package app contains the use cases exposed through the desktop binding.
package app

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	configWriter "github.com/MaimoryLab/BootAgent/internal/config"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/install"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

type CommandLookup func(string) (string, bool)

const versionProbeConcurrency = 3

type StatusOptions struct {
	Home     string
	Platform platform.Info
	Lookup   CommandLookup
	// SystemRegion is populated from the native Windows APIs by the desktop
	// constructor. Tests and non-Windows builds leave it empty.
	SystemRegion string
	// FileSystem is optional and exists for tests or platform-specific hosts
	// that need to inject ACL behavior. Production callers use the default
	// securefs implementation for the selected platform.
	FileSystem  *securefs.Store
	Runner      process.Runner
	Environment map[string]string
}

type UseCases struct {
	status          StatusOptions
	provider        *provider.Client
	providers       provider.Store
	profiles        profileStore.Store
	filesystem      securefs.Store
	runner          process.Runner
	environment     map[string]string
	migrationNotice string
	// httpDoer is shared by internal runtime and desktop-agent downloads. It is
	// injectable so install behavior is testable without reaching a CDN.
	httpDoer install.Doer
	writeMu  sync.Mutex
	// taskLocks serialize one install/update/download target without exposing a
	// second transport-level task service. The existing writeMu still protects
	// shared config publication; these locks prevent duplicate work at the edge.
	taskMu    sync.Mutex
	taskLocks map[string]*sync.Mutex
	// The region behind the default download host cannot change without the user
	// changing a system setting, so a successful probe is remembered for the
	// process. regionKnown is separate from the answer so a probe that failed is
	// retried instead of pinning "not China" for the session.
	regionMu        sync.Mutex
	regionKnown     bool
	regionIsChinese bool
	// Registry answers for the update dot, cached so a status poll does not spend
	// a request per Agent every time it runs.
	latestVersions latestVersionCache
	mcpDraft       mcpDraftState
	skillDraft     skillDraftState
	skillPreviewMu sync.Mutex
	skillPreviews  map[string]skillPreview
}

// DraftState combines the independent MCP and Skills drafts for the native
// close guard without coupling either feature's state to the other.
func (u *UseCases) DraftState() (bool, string) {
	if u == nil {
		return false, "zh"
	}
	mcpDirty, mcpLocale := u.MCPDraftState()
	skillDirty, skillLocale := u.SkillDraftState()
	locale := mcpLocale
	if skillLocale != "" {
		locale = skillLocale
	}
	return mcpDirty || skillDirty, locale
}

// SetRuntimeDownloader overrides the HTTP client used for internal downloads.
// The historical name remains because runtime bootstrap was its first caller.
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

func newUseCases(options StatusOptions, client *provider.Client, profiles profileStore.Store) *UseCases {
	if options.Platform.OS == "" {
		options.Platform = platform.Current()
	}
	if options.Home == "" {
		options.Home = platform.ResolveHome(nil, options.Platform.OS)
	}
	migrationNotice, migrationErr := migrateLegacyHome(options.Home, options.Platform.OS)
	if migrationErr != nil {
		migrationNotice = "无法迁移旧 BootAgent 配置：" + migrationErr.Error()
	}
	// Lookup stays nil unless a caller injected one. A default here would resolve
	// against the BootAgent process PATH, which disagrees with the environment
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
	injectedProfiles := profiles.Home != ""
	if profiles.Home == "" {
		profiles = profileStore.NewStore(options.Home, options.Platform.OS)
	}
	filesystem := securefs.Store{}
	if options.FileSystem != nil {
		filesystem = *options.FileSystem
	} else if injectedProfiles && profiles.FS != nil {
		filesystem = *profiles.FS
	} else {
		backupRoot := filepath.Join(options.Home, ".bootagent", "backup")
		filesystem = securefs.New(securefs.Options{
			OS:         options.Platform.OS,
			BackupRoot: backupRoot,
			Retention: func() int {
				return backupRetentionFromFile(filepath.Join(options.Home, ".bootagent", "settings.json"))
			},
		})
	}
	// Reuse one filesystem policy for profile, environment, Provider, MCP, Skill,
	// and Agent config writes. Tests may inject a filesystem with custom ACL hooks;
	// production uses the managed backup policy above.
	profiles.FS = &filesystem
	return &UseCases{
		status:          options,
		provider:        client,
		providers:       provider.NewStore(options.Home, filesystem),
		profiles:        profiles,
		filesystem:      filesystem,
		runner:          runner,
		environment:     cloneEnvironment(options.Environment),
		migrationNotice: migrationNotice,
	}
}

// CommandLogDir holds one log file per day recording every subprocess BootAgent
// runs. The desktop build is a GUI process with no console, so these files are
// the only place a failing npm, uv or launch command can be read back from.
func CommandLogDir(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".bootagent", "logs")
}

func cloneEnvironment(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	maps.Copy(result, source)
	return result
}

func NewUseCasesFromEnvironment() *UseCases {
	info := platform.Current()
	region, _ := platform.SystemRegion()
	return NewUseCases(StatusOptions{
		Home:         platform.ResolveHome(nil, info.OS),
		Platform:     info,
		SystemRegion: region,
	})
}

type StatusResponse struct {
	APIVersion    int                         `json:"apiVersion"`
	Platform      platform.Info               `json:"platform"`
	Capabilities  Capabilities                `json:"capabilities"`
	Agents        map[string]AgentStatus      `json:"agents"`
	Catalog       []catalog.CatalogItem       `json:"catalog"`
	Groups        []catalog.Group             `json:"groups"`
	Providers     map[string]catalog.Provider `json:"providers"`
	Mirrors       []catalog.Mirror            `json:"mirrors"`
	Paths         map[string]string           `json:"paths"`
	Backups       map[string]bool             `json:"backups"`
	Profiles      []ProfileSummary            `json:"profiles"`
	ActiveProfile *string                     `json:"activeProfile"`
	// FirstRun reports that ~/.bootagent does not exist yet, which is the signal
	// the UI uses to open onboarding instead of the overview. Agent detection is
	// not a substitute: an Agent installed before BootAgent would suppress it.
	FirstRun         bool                 `json:"firstRun"`
	MigrationNotice  string               `json:"migrationNotice,omitempty"`
	Runtimes         []RuntimeStatus      `json:"runtimes"`
	Environment      any                  `json:"environment"`
	EnvironmentError *string              `json:"environmentError"`
	DesktopAgents    []DesktopAgentStatus `json:"desktopAgents"`
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
	Installed     bool    `json:"installed"`
	Configured    bool    `json:"configured"`
	GuideOnly     bool    `json:"guideOnly"`
	Config        string  `json:"config"`
	Version       *string `json:"version"`
	LockedVersion *string `json:"lockedVersion"`
	// LatestVersion is the newest version published to the package registry, or
	// nil when it is not knowable -- offline, rate limited, or not an npm
	// package. It drives the update dot only, so nil means "say nothing" rather
	// than "up to date".
	LatestVersion *string         `json:"latestVersion"`
	CanInstall    bool            `json:"canInstall"`
	Provider      *string         `json:"provider"`
	ProfileID     *string         `json:"profileId"`
	Model         *string         `json:"model"`
	BaseURL       *string         `json:"baseUrl"`
	UpdatedAt     *string         `json:"updatedAt"`
	Detected      *DetectedConfig `json:"detected"`
}

type DetectedConfig struct {
	BaseURL            string  `json:"baseUrl"`
	Model              string  `json:"model"`
	ManagedByBootAgent bool    `json:"managedByBootAgent"`
	Unreadable         *string `json:"unreadable"`
}

// ProfileSummary is intentionally a public projection with no credential field.
type ProfileSummary struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	Provider        string  `json:"provider"`
	BaseURL         *string `json:"baseUrl"`
	Model           *string `json:"model"`
	ReasoningEffort string  `json:"reasoningEffort,omitempty"`
	Protocol        string  `json:"protocol"`
	ActivatedAt     *string `json:"activatedAt"`
	CreatedAt       string  `json:"createdAt,omitempty"`
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
		"launch_directory": currentDirectory(),
		"profile":          filepath.Join(options.Home, ".bootagent", "profile.json"),
		// The Task Center points users at this directory when a command fails.
		// It has to come from here rather than being spelled out in the UI: a
		// hardcoded "~/.bootagent/logs" names a path that does not exist on
		// Windows, where this resolves to C:\Users\<name>\.bootagent\logs.
		"logs": CommandLogDir(options.Home),
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
	bindings, err := u.profiles.ListAgentBindings()
	if err != nil {
		return StatusResponse{}, err
	}
	latestVersions := u.latestAgentVersions(ctx, manifest, agentLookup)
	installedVersions := u.installedVersions(ctx, manifest, agentLookup)
	for _, id := range catalog.AgentIDs(manifest) {
		agent := manifest.Agents[id]
		configPath := configPath(options.Home, options.Platform.OS, agent)
		if configPath != "" {
			paths[id+"_config"] = configPath
		}
		installed := false
		if agent.Command != "" {
			_, installed = agentLookup(agent.Command)
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
			detected = detectedConfig(configWriter.DetectFile(configPath, agent.ConfigAdapter, agent.EnvVars))
		}
		var installedVersion *string
		if installed && agent.ConfigMode == "auto" {
			installedVersion = installedVersions[id]
		}
		statuses[id] = AgentStatus{
			Installed:     installed,
			Configured:    fileExists(configPath),
			GuideOnly:     agent.ConfigMode == "guide",
			Config:        configPath,
			Version:       installedVersion,
			LockedVersion: nil,
			LatestVersion: latestVersions[id],
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
	profiles, activeProfile, environment, environmentError, err := u.profileStatus(ctx)
	if err != nil {
		return StatusResponse{}, err
	}
	desktopAgents := u.desktopAgentStatuses(ctx)
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
		FirstRun:         !fileExists(filepath.Join(options.Home, ".bootagent")),
		MigrationNotice:  u.migrationNotice,
		DesktopAgents:    desktopAgents,
	}, nil
}

func currentDirectory() string {
	directory, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Clean(directory)
}

func (u *UseCases) installedVersions(ctx context.Context, manifest catalog.Manifest, lookup func(string) (string, bool)) map[string]*string {
	queries := make([]struct {
		id, executable string
		args           []string
	}, 0, len(manifest.Agents))
	for id, agent := range manifest.Agents {
		if agent.ConfigMode != "auto" || agent.Command == "" {
			continue
		}
		executable, installed := lookup(agent.Command)
		if installed {
			queries = append(queries, struct {
				id, executable string
				args           []string
			}{id, executable, agent.VersionArgs})
		}
	}
	if len(queries) == 0 {
		return nil
	}
	versions := make(map[string]*string, len(queries))
	var mu sync.Mutex
	var group sync.WaitGroup
	tokens := make(chan struct{}, versionProbeConcurrency)
	for _, query := range queries {
		group.Add(1)
		go func(query struct {
			id, executable string
			args           []string
		}) {
			defer group.Done()
			tokens <- struct{}{}
			defer func() { <-tokens }()
			if version := u.installedVersion(ctx, query.executable, query.args); version != nil {
				mu.Lock()
				versions[query.id] = version
				mu.Unlock()
			}
		}(query)
	}
	group.Wait()
	return versions
}

var versionPattern = regexp.MustCompile(`(^|[^\d])(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)`)

// installedVersion runs the Agent's version command and takes the first
// semver-looking token from either stream.
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

func detectedConfig(value *configWriter.Detected) *DetectedConfig {
	if value == nil {
		return nil
	}
	return &DetectedConfig{
		BaseURL:            value.BaseURL,
		Model:              value.Model,
		ManagedByBootAgent: value.ManagedByBootAgent,
		Unreadable:         value.Unreadable,
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
	return u.profileSummaries()
}

// ListAgentBindings returns the public per-Agent routing records. Binding files
// contain no credential material.
func (u *UseCases) ListAgentBindings(ctx context.Context) (map[string]profileStore.AgentBinding, error) {
	if u == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Agent listing request was cancelled"); err != nil {
		return nil, err
	}
	return u.profiles.ListAgentBindings()
}

type SaveProfileOptions struct {
	ID              string
	Label           string
	Provider        string
	APIBaseURL      string
	APIKey          string
	Model           string
	ReasoningEffort string
	ConfigMode      string
	Protocol        string
}

// SaveProfileResult reports which Agents followed the Profile to its new
// Provider or model, mirroring SaveProviderResult. Reapply failures are returned
// per Agent rather than failing the save: the Profile record on disk is already
// correct, and reverting it would lose the edit.
type SaveProfileResult struct {
	Profile   ProfileSummary    `json:"profile"`
	Reapplied []string          `json:"reapplied"`
	Failures  map[string]string `json:"failures"`
}

func (u *UseCases) SaveProfile(ctx context.Context, options SaveProfileOptions) (SaveProfileResult, error) {
	if u == nil {
		return SaveProfileResult{}, oneerrors.New(oneerrors.InternalError, "Profile service is not configured", oneerrors.WithStatus(501))
	}
	if err := ctx.Err(); err != nil {
		return SaveProfileResult{}, oneerrors.New(oneerrors.Timeout, "Profile request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	// Rejected at the edit, not on the user's first request: an unsupported
	// depth would otherwise surface as an activation error, or -- for adapters
	// whose own schema accepts any string, like dsh's -- as a per-request
	// dispatch failure far from the Profile page that caused it. The Agent
	// vocabularies are narrower and each write gates again; this rejects only
	// what no adapter could ever express.
	if effort := strings.TrimSpace(options.ReasoningEffort); effort != "" {
		if err := configWriter.ValidateProfileReasoningEffort(effort); err != nil {
			return SaveProfileResult{}, err
		}
	}
	target, err := u.providers.Resolve(options.Provider, options.APIBaseURL)
	if err != nil {
		return SaveProfileResult{}, err
	}
	providedKey := options.APIKey
	providerKey := providedKey
	if strings.TrimSpace(providerKey) == "" {
		providerKey = target.APIKey
	}
	if strings.TrimSpace(providerKey) != "" {
		if err := u.providers.SaveKey(ctx, options.Provider, providerKey); err != nil {
			return SaveProfileResult{}, err
		}
	}
	before := u.profileByID(options.ID)
	stored, err := u.profiles.Save(ctx, profileStore.SaveRequest{
		ID:       options.ID,
		Label:    options.Label,
		Provider: options.Provider,
		BaseURL:  target.BaseFor(options.Protocol),
		// Keep explicit keys accepted by callers, but do not copy a
		// Provider-resolved key into every Profile secret file.
		APIKey:               providedKey,
		ProviderKeyAvailable: strings.TrimSpace(providerKey) != "",
		Model:                options.Model,
		ReasoningEffort:      options.ReasoningEffort,
		ConfigMode:           options.ConfigMode,
		Protocol:             options.Protocol,
	})
	if err != nil {
		return SaveProfileResult{}, err
	}
	result := SaveProfileResult{Profile: profileSummary(stored)}
	result.Reapplied, result.Failures, err = u.reapplyProfileLocked(ctx, before, stored)
	if err != nil {
		return result, err
	}
	return result, nil
}

// reapplyProfileLocked carries a Profile edit through to the Agents bound to it.
// Without this a Profile could be switched to another Provider while every Agent
// following it stayed pointed at the old endpoint -- the Profile page showed the
// new Provider, the Agent kept sending traffic to the old one, and neither
// Provider card listed the Agent correctly. Callers must hold writeMu.
func (u *UseCases) reapplyProfileLocked(ctx context.Context, before, after profileStore.Profile) ([]string, map[string]string, error) {
	model := ""
	if after.Model != nil {
		model = strings.TrimSpace(*after.Model)
	}
	previousModel := ""
	if before.Model != nil {
		previousModel = strings.TrimSpace(*before.Model)
	}
	// Nothing an Agent config carries has changed. A label or config-mode edit
	// leaves every binding correct, so there is nothing to rewrite. An effort
	// edit does rewrite: the depth lives in the Agent's own config, so leaving
	// the bindings would show the new depth on the Profile page while every
	// Agent kept thinking at the old one.
	if before.ID == after.ID && before.Provider == after.Provider && previousModel == model &&
		before.ReasoningEffort == after.ReasoningEffort {
		return nil, nil, nil
	}
	// A Profile with no model cannot produce a valid binding: WriteAgentBinding
	// requires one, and activation refuses an empty model. Leave the bindings as
	// they are rather than failing every one of them.
	if model == "" {
		return nil, nil, nil
	}
	target, err := u.providers.Get(after.Provider)
	if err != nil {
		return nil, nil, err
	}
	return u.reapplyBindingsLocked(ctx, func(binding profileStore.AgentBinding) bool {
		return binding.ProfileRef == after.ID
	}, func(profileStore.AgentBinding) (provider.Entry, string) {
		return target, model
	})
}

func (u *UseCases) profileByID(id string) profileStore.Profile {
	id = strings.TrimSpace(id)
	profiles, err := u.profiles.List()
	if err != nil {
		return profileStore.Profile{}
	}
	for _, saved := range profiles {
		if saved.ID == id {
			return saved
		}
	}
	return profileStore.Profile{}
}

func (u *UseCases) DeleteProfile(ctx context.Context, id string) error {
	if u == nil {
		return oneerrors.New(oneerrors.InternalError, "Profile service is not configured", oneerrors.WithStatus(501))
	}
	if err := ctx.Err(); err != nil {
		return oneerrors.New(oneerrors.Timeout, "Profile request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	id = strings.TrimSpace(id)
	var users []string
	bindings, err := u.catalogAgentBindings(true)
	if err != nil {
		return err
	}
	for agentID, binding := range bindings {
		if binding.ProfileRef == id {
			users = append(users, agentID)
		}
	}
	if len(users) > 0 {
		sort.Strings(users)
		return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Profile %s is used by Agent(s): %s", id, strings.Join(users, ", ")))
	}
	return u.profiles.Delete(ctx, id)
}

// catalogAgentBindings mirrors the management pages' user lists. Profiles only
// show auto-configured Agents; Providers show every catalog Agent.
func (u *UseCases) catalogAgentBindings(autoOnly bool) (map[string]profileStore.AgentBinding, error) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return nil, err
	}
	bindings, err := u.profiles.ListAgentBindings()
	if err != nil {
		return nil, err
	}
	result := map[string]profileStore.AgentBinding{}
	for agentID, binding := range bindings {
		agent, ok := manifest.Agents[agentID]
		if !ok || (autoOnly && agent.ConfigMode != "auto") {
			continue
		}
		result[agentID] = binding
	}
	return result, nil
}

func (u *UseCases) profileStatus(ctx context.Context) ([]ProfileSummary, *string, any, *string, error) {
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	active := u.profiles.LoadActiveContext(ctx)
	profiles, err := u.profileSummaries()
	if err != nil {
		return nil, nil, nil, nil, err
	}
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
	return profiles, activeID, environment, environmentError, nil
}

func (u *UseCases) profileSummaries() ([]ProfileSummary, error) {
	stored, err := u.profiles.List()
	if err != nil {
		return nil, err
	}
	result := make([]ProfileSummary, 0, len(stored))
	for _, item := range stored {
		result = append(result, profileSummary(item))
	}
	return result, nil
}

func profileSummary(item profileStore.Profile) ProfileSummary {
	summary := item.Summary()
	return ProfileSummary{
		ID:              summary.ID,
		Label:           summary.Label,
		Provider:        summary.Provider,
		BaseURL:         nil,
		Model:           summary.Model,
		ReasoningEffort: summary.ReasoningEffort,
		Protocol:        summary.Protocol,
		ActivatedAt:     summary.ActivatedAt,
		CreatedAt:       summary.CreatedAt,
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
	backupRoot := filepath.Join(home, ".bootagent", "backup")
	for _, id := range catalog.AgentIDs(manifest) {
		agent := manifest.Agents[id]
		path := configPath(home, osID, agent)
		if path == "" {
			continue
		}
		matches, err := filepath.Glob(path + ".backup-*")
		result[id] = err == nil && len(matches) > 0 || managedBackupExists(backupRoot, path)
	}
	profileMatches, err := filepath.Glob(filepath.Join(home, ".bootagent", "profile.json.backup-*"))
	profilePath := filepath.Join(home, ".bootagent", "profile.json")
	result["profile"] = err == nil && len(profileMatches) > 0 || managedBackupExists(backupRoot, profilePath)
	return result
}

func managedBackupExists(root, target string) bool {
	for _, group := range []string{securefs.BackupGroupPath(root, target), securefs.LegacyBackupGroupPath(root, target)} {
		entries, err := os.ReadDir(group)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "backup-") && entry.Type().IsRegular() {
				return true
			}
		}
	}
	return false
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}
