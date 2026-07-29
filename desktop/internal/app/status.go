package app

import (
	"os"
	"path/filepath"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/install"
	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/desktop/internal/profile"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// StatusAgent is one Agent's state, as the overview shows it.
type StatusAgent struct {
	Installed bool   `json:"installed"`
	Config    string `json:"config"`
	// Configured means the config file exists, not that OneAgent wrote it.
	Configured    bool    `json:"configured"`
	GuideOnly     bool    `json:"guideOnly"`
	Version       *string `json:"version"`
	LockedVersion *string `json:"lockedVersion"`
	CanInstall    bool    `json:"canInstall"`
	// Provider, Model, BaseURL and UpdatedAt come from this Agent's own binding,
	// independent of every other Agent.
	Provider  *string `json:"provider"`
	Model     *string `json:"model"`
	BaseURL   *string `json:"baseUrl"`
	UpdatedAt *string `json:"updatedAt"`
	// Detected is what the Agent's own config file says, reported alongside the
	// binding rather than instead of it: a disagreement between the two is itself
	// worth showing, because it means the config changed outside OneAgent.
	Detected *config.Detected `json:"detected"`
}

// Capabilities says what this machine can do, per Agent.
type Capabilities struct {
	CanInstall map[string]bool `json:"canInstall"`
	// SupportedAgentIDs lists the Agents this platform can run at all, which is a
	// different question from whether the tooling to install them is present.
	SupportedAgentIDs []string `json:"supportedAgentIds"`
}

// Platform describes the machine.
type Platform struct {
	OS    string `json:"os"`
	Arch  string `json:"arch"`
	Shell string `json:"shell"`
}

// Status is the payload the whole frontend reads.
//
// Field names are camelCase because that is the existing transport contract; the
// migration does not renumber or rename any of it.
type Status struct {
	APIVersion   int                               `json:"apiVersion"`
	Platform     Platform                          `json:"platform"`
	Capabilities Capabilities                      `json:"capabilities"`
	Agents       map[string]StatusAgent            `json:"agents"`
	Catalog      []catalog.PublicAgent             `json:"catalog"`
	Groups       []catalog.Group                   `json:"groups"`
	Providers    map[string]catalog.PublicProvider `json:"providers"`
	Mirrors      []catalog.Mirror                  `json:"mirrors"`
	Paths        map[string]string                 `json:"paths"`
	Backups      map[string]bool                   `json:"backups"`
	// Profiles carry no key material; hasKey says whether one is stored.
	Profiles         []*jsonorder.Object `json:"profiles"`
	ActiveProfile    *string             `json:"activeProfile"`
	Environment      *jsonorder.Object   `json:"environment"`
	EnvironmentError *string             `json:"environmentError"`
}

// Status reports everything the overview needs in one read.
func (s *Service) Status() (Status, error) {
	manifest, err := catalog.Load()
	if err != nil {
		return Status{}, err
	}
	rt := s.Runtime

	paths := map[string]string{
		"env_file": config.EnvPath(rt),
		"profile":  profile.PointerPath(rt),
	}
	capabilities := Capabilities{CanInstall: map[string]bool{}, SupportedAgentIDs: []string{}}
	agents := map[string]StatusAgent{}

	// Read once: the bindings are one small directory, and reading inside the loop
	// would re-scan it for every Agent in the catalog.
	bindings, err := s.Store.ListBindings()
	if err != nil {
		return Status{}, err
	}

	// Declaration order, not rank order: supportedAgentIds is built in this loop
	// and Python iterates the parsed manifest dict, which keeps the order the file
	// lists. The two orders really differ -- cursor sits third by rank and eighth by
	// declaration -- so using the catalog ordering here changes an array the
	// frontend renders.
	for _, agentID := range manifest.DeclaredIDs() {
		agent, _ := manifest.Agent(agentID)
		configPath := config.ConfigPath(rt, agent)
		if configPath != "" {
			paths[agentID+"_config"] = configPath
		}

		_, installed := rt.Which(agent.Command)
		installed = installed && agent.Command != ""
		canInstall := s.canInstall(agent)
		capabilities.CanInstall[agentID] = canInstall
		if supportedOn(agent, rt.OSID) {
			capabilities.SupportedAgentIDs = append(capabilities.SupportedAgentIDs, agentID)
		}

		entry := StatusAgent{
			Installed:  installed,
			Config:     configPath,
			Configured: configPath != "" && fileExists(configPath),
			GuideOnly:  agent.GuideOnly(),
			CanInstall: canInstall,
			Detected:   config.DetectAgentConfig(rt, agent),
		}
		if agent.Package != nil {
			locked := agent.Package.Version
			entry.LockedVersion = &locked
		}
		// The version is only asked for when there is something to ask: running a
		// --version for an Agent that is not installed costs a process per Agent.
		if installed && !agent.GuideOnly() {
			if version := install.InstalledVersion(rt, agent); version != "" {
				entry.Version = &version
			}
		}
		if binding, present := bindings[agentID]; present {
			entry.Provider = optionalString(binding, "provider")
			entry.Model = optionalString(binding, "model")
			entry.BaseURL = optionalString(binding, "base_url")
			entry.UpdatedAt = optionalString(binding, "updated_at")
		}
		agents[agentID] = entry
	}

	environment, environmentError, err := s.Store.Load()
	if err != nil {
		return Status{}, err
	}
	profiles, err := s.Store.List()
	if err != nil {
		return Status{}, err
	}
	summaries := make([]*jsonorder.Object, 0, len(profiles))
	for _, item := range profiles {
		summaries = append(summaries, profile.PublicSummary(item))
	}

	status := Status{
		APIVersion: 1,
		// Arch and Shell describe the host; only OS is taken from the runtime. That
		// asymmetry is the Python core's -- it spreads current_platform() and
		// overrides os alone -- and it is only observable when a runtime simulates
		// another platform, which is a test, not production.
		Platform: Platform{
			OS:    rt.OSID,
			Arch:  runtime.CurrentArch(),
			Shell: runtime.ShellFor(runtime.CurrentOSID()),
		},
		Capabilities: capabilities,
		Agents:       agents,
		Catalog:      manifest.PublicCatalog(runtime.CurrentOSID()),
		Groups:       catalog.Groups,
		Providers:    catalog.PublicProviders(),
		Mirrors:      catalog.PublicMirrors(),
		Paths:        paths,
		Backups:      s.backups(manifest),
		Profiles:     summaries,
		Environment:  environment,
	}
	if active := s.Store.ActiveID(); active != "" {
		status.ActiveProfile = &active
	}
	if environmentError != "" {
		status.EnvironmentError = &environmentError
	}
	return status, nil
}

// canInstall reports whether the tooling this Agent needs is present.
//
// Derived from the manifest's declared package manager and Windows prerequisites,
// so an Agent added later is answered correctly without a second place to update.
func (s *Service) canInstall(agent catalog.Agent) bool {
	if agent.Package == nil {
		return false
	}
	present := false
	switch agent.Package.Manager {
	case "npm":
		_, present = s.Runtime.Which("npm")
	case "uv":
		if _, found := s.Runtime.Which("uv"); found {
			// Aider's upstream is a Python tool, so uv alone is not enough -- the
			// interpreter it needs has to be there too.
			_, err := install.Python312ForUV(s.Runtime)
			present = err == nil
		}
	}
	if s.Runtime.OSID == "windows" {
		for _, prerequisite := range agent.WindowsPrerequisites {
			if _, found := s.Runtime.Which(prerequisite); !found {
				return false
			}
		}
	}
	return present
}

// backups reports which files have a backup on disk.
//
// Derived from each Agent's declared config_path rather than a hand-written list:
// a new auto Agent's backups appear the moment its lock entry lands, with no second
// place to keep in sync.
func (s *Service) backups(manifest *catalog.Manifest) map[string]bool {
	found := map[string]bool{}
	for _, agentID := range manifest.IDs() {
		agent, _ := manifest.Agent(agentID)
		configPath := config.ConfigPath(s.Runtime, agent)
		if configPath == "" {
			continue
		}
		found[agentID] = hasBackup(configPath)
	}
	found["env"] = hasBackup(config.EnvPath(s.Runtime))
	found["profile"] = hasBackup(profile.PointerPath(s.Runtime))
	return found
}

// hasBackup reports whether any timestamped backup of a path exists.
func hasBackup(path string) bool {
	matches, err := filepath.Glob(path + ".backup-*")
	if err != nil {
		return false
	}
	return len(matches) > 0
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// optionalString reads a field as a pointer, so an absent value encodes as null
// rather than as the empty string -- the frontend reads those differently.
func optionalString(object *jsonorder.Object, key string) *string {
	value, present := object.Get(key)
	if !present {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	return &text
}
