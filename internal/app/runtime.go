package app

import (
	"context"
	"fmt"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/install"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

// RuntimeStatus is the public projection of one bootstrappable runtime.
type RuntimeStatus = install.RuntimeState

// InstallRuntimeOptions is one runtime bootstrap request.
type InstallRuntimeOptions struct {
	RuntimeID string
	Timeout   time.Duration
	Output    process.OutputListener
	// PreferMirror overrides the stored setting for this request only. Nil means
	// use the setting, which is what the UI sends.
	PreferMirror *bool
}

// InstallRuntimeResult reports what the bootstrap did. PathUpdated is separate
// from Installed because an already-downloaded runtime may still need its
// directory recorded on the login PATH.
type InstallRuntimeResult struct {
	Runtime     string          `json:"runtime"`
	Installed   bool            `json:"installed"`
	Version     string          `json:"version"`
	PathUpdated bool            `json:"pathUpdated"`
	Runtimes    []RuntimeStatus `json:"runtimes"`
}

// RuntimeStatuses reports every runtime in the lock file for this platform.
func (u *UseCases) RuntimeStatuses(ctx context.Context) ([]RuntimeStatus, error) {
	if u == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Runtime service is not configured", oneerrors.WithStatus(501))
	}
	if _, err := catalog.LoadEmbeddedRuntimes(); err != nil {
		return nil, err
	}
	states, _ := u.runtimeCapability(ctx)
	return states, nil
}

// InstallRuntime downloads and installs one locked runtime, then records its
// directory on the login PATH so the user's own terminal and later OneAgent
// runs both resolve it.
func (u *UseCases) InstallRuntime(ctx context.Context, options InstallRuntimeOptions) (InstallRuntimeResult, error) {
	if u == nil {
		return InstallRuntimeResult{}, oneerrors.New(oneerrors.InternalError, "Runtime service is not configured", oneerrors.WithStatus(501))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx, "Runtime installation request was cancelled"); err != nil {
		return InstallRuntimeResult{}, err
	}
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		return InstallRuntimeResult{}, err
	}
	entry, present := manifest.Runtimes[options.RuntimeID]
	if !present {
		return InstallRuntimeResult{}, oneerrors.New(oneerrors.InvalidRequest, "Unknown runtime: "+options.RuntimeID)
	}

	// The bootstrap writes into the OneAgent home and touches shell profiles;
	// keep it under the same coordinator as Agent installs so two requests
	// cannot interleave a profile rewrite.
	u.writeMu.Lock()
	defer u.writeMu.Unlock()

	preferMirror := u.preferMirror(ctx)
	if options.PreferMirror != nil {
		preferMirror = *options.PreferMirror
	}
	runtime := u.installRuntime(options.Output)
	updated, installed, err := install.EnsureRuntime(ctx, runtime, u.httpDoer, options.RuntimeID, entry, install.RuntimeOptions{PreferMirror: preferMirror})
	if err != nil {
		return InstallRuntimeResult{}, err
	}
	pathUpdated, err := u.persistRuntimePath(ctx, updated, manifest)
	if err != nil {
		return InstallRuntimeResult{}, err
	}
	// Report status through the updated runtime so the freshly installed
	// directory is on the PATH used for the version probe.
	states := install.RuntimeStates(ctx, updated, manifest)
	result := InstallRuntimeResult{
		Runtime:     options.RuntimeID,
		Installed:   installed,
		PathUpdated: pathUpdated,
		Runtimes:    states,
	}
	for _, state := range states {
		if state.ID == options.RuntimeID {
			result.Version = state.Version
			break
		}
	}
	return result, nil
}

func (u *UseCases) persistRuntimePath(ctx context.Context, runtime install.Runtime, manifest catalog.RuntimeManifest) (bool, error) {
	dirs := install.ManagedPathDirs(u.status.Home, u.status.Platform.OS, u.status.Platform.Arch, manifest)
	if len(dirs) == 0 {
		return false, nil
	}
	return install.PersistRuntimePath(ctx, runtime, u.filesystem, dirs)
}

// installRuntime builds the process runtime with managed directories already on
// PATH. Every caller that runs npm or uv must go through this so a runtime
// installed in an earlier session is found instead of reinstalled.
func (u *UseCases) installRuntime(output process.OutputListener) install.Runtime {
	runtime := install.NewRuntime(u.status.Home, u.status.Platform, u.runner, u.environment)
	runtime.OnOutput = output
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		return runtime
	}
	for _, directory := range install.ManagedPathDirs(u.status.Home, u.status.Platform.OS, u.status.Platform.Arch, manifest) {
		runtime = install.WithManagedPath(runtime, directory)
	}
	return runtime
}

// runtimeCapability reads runtime state once for GetStatus and returns a lookup
// that answers two questions per package manager: is the command available now,
// and if not, can OneAgent install the runtime that provides it.
func (u *UseCases) runtimeCapability(ctx context.Context) ([]RuntimeStatus, runtimeCapabilityLookup) {
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		return nil, runtimeCapabilityLookup{}
	}
	// Status must resolve commands in the same environment installs run in, so
	// resolution goes through the managed-PATH runner. An injected Lookup still
	// wins: it is the seam that keeps status reproducible in tests and keeps a
	// developer's own Node out of a test's answer.
	runtime := u.installRuntime(nil)
	if u.status.Lookup != nil {
		runtime.Runner = statusRunner{Runner: runtime.Runner, lookup: u.status.Lookup}
	}
	states := install.RuntimeStates(ctx, runtime, manifest)
	agents, agentErr := catalog.LoadEmbedded()
	byCommand := make(map[string]RuntimeStatus, len(states)*2)
	for index, state := range states {
		if agentErr == nil {
			states[index].RequiredByHint = requiredByHint(manifest, agents, state.ID, u.status.Platform.OS)
		}
		for _, command := range manifest.Runtimes[state.ID].Commands {
			byCommand[command] = states[index]
		}
	}
	return states, runtimeCapabilityLookup{byCommand: byCommand, resolve: runtime.Runner.LookPath}
}

// statusRunner routes command resolution through the status Lookup while
// keeping the real runner for version probes.
type statusRunner struct {
	process.Runner
	lookup CommandLookup
}

func (r statusRunner) LookPath(command string) (string, bool) {
	return r.lookup(command)
}

type runtimeCapabilityLookup struct {
	byCommand map[string]RuntimeStatus
	// resolve is the managed-PATH resolver, the same one status used to probe
	// runtime versions. GetStatus reuses it so an Agent CLI installed under the
	// managed global prefix is found, not just the runtimes themselves.
	resolve CommandLookup
}

// available reports whether this package manager can be executed. A managed
// runtime counts even when the host PATH has no such command, because installs
// run with the managed directory prepended.
func (l runtimeCapabilityLookup) available(manager string) bool {
	if state, known := l.byCommand[manager]; known {
		if state.Installed || state.Managed {
			return true
		}
	}
	return l.present(manager)
}

// present reports whether a command resolves the way an install would.
func (l runtimeCapabilityLookup) present(command string) bool {
	_, ok := l.lookup(command)
	return ok
}

// lookup resolves a command the way an install would. It answers false rather
// than falling back to the process PATH when no resolver was built, so status
// cannot report a command OneAgent would not actually be able to run.
func (l runtimeCapabilityLookup) lookup(command string) (string, bool) {
	if l.resolve == nil || command == "" {
		return "", false
	}
	return l.resolve(command)
}

// provider names the runtime that supplies this manager, when the current
// platform has a locked download for it.
func (l runtimeCapabilityLookup) provider(manager string) (string, bool) {
	state, known := l.byCommand[manager]
	if !known || !state.Supported {
		return "", false
	}
	return state.ID, true
}

// requiredByHint names the Agents that cannot be installed without this
// runtime, so the UI can explain why an install button matters.
func requiredByHint(runtimes catalog.RuntimeManifest, agents catalog.Manifest, runtimeID, osID string) string {
	entry := runtimes.Runtimes[runtimeID]
	managers := make(map[string]bool, len(entry.Commands))
	for _, command := range entry.Commands {
		managers[command] = true
	}
	names := make([]string, 0, len(agents.Agents))
	for _, id := range catalog.AgentIDs(agents) {
		agent := agents.Agents[id]
		if agent.Package == nil || !managers[agent.Package.Manager] {
			continue
		}
		if !contains(agent.Platforms, osID) {
			continue
		}
		names = append(names, agent.Name)
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) > 3 {
		return fmt.Sprintf("%s, %s, %s +%d", names[0], names[1], names[2], len(names)-3)
	}
	return joinNames(names)
}

func joinNames(names []string) string {
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + ", " + names[1]
	default:
		return names[0] + ", " + names[1] + ", " + names[2]
	}
}
