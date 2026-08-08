package app

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	configWriter "github.com/MaimoryLab/OneAgent/internal/config"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/install"
	"github.com/MaimoryLab/OneAgent/internal/process"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

// InstallAgentsOptions is one installation request. It stays independent of
// Wails transport types so validation and write ordering remain in the core.
type InstallAgentsOptions struct {
	Agents         []string
	Provider       string
	APIBaseURL     string
	APIKey         string
	Model          string
	SmallFastModel string
	Configure      bool
	InstallAgent   bool
	CheckAgentOnly bool
	SkipTest       bool
	AgentVersion   string
	Timeout        time.Duration
	Registry       string
	Output         process.OutputListener
	// ProfileID is optional. A configured install without one keeps the current
	// profile ID, or uses "default" on the first run.
	ProfileID string
	// ProfileLabel is optional and only used when the install creates the
	// profile named by ProfileID. An existing profile keeps its own label, so a
	// re-run cannot silently rename what the user already named.
	ProfileLabel string
}

// AgentInstallResult is the public per-Agent outcome. It contains no key or
// process output; the latter is reduced to the redacted aggregate Log field.
type AgentInstallResult struct {
	Agent         string `json:"agent"`
	Status        string `json:"status"`
	Config        string `json:"config,omitempty"`
	Installed     bool   `json:"installed,omitempty"`
	Version       string `json:"version,omitempty"`
	LockedVersion string `json:"lockedVersion,omitempty"`
	Registry      string `json:"registry,omitempty"`
	Code          int    `json:"code,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	Message       string `json:"message,omitempty"`
	Retryable     bool   `json:"retryable"`
	// checkOnly distinguishes the check path's skipped result from a normal
	// configure=false result. The public contract includes config only for the
	// latter, not for check-agent-only results.
	checkOnly bool
}

// InstallAgentsResult is the final aggregate outcome of one request.
type InstallAgentsResult struct {
	OK      bool                            `json:"ok"`
	Code    int                             `json:"code"`
	Results []AgentInstallResult            `json:"results"`
	Log     string                          `json:"log"`
	Next    string                          `json:"next"`
	Probe   *provider.ProbeResult           `json:"probe"`
	Probes  map[string]provider.ProbeResult `json:"probes"`
}

// InstallAgents validates, installs and configures the requested Agents. A
// failure for one Agent is recorded and the remaining Agents are still tried;
// request-shape and manifest errors are returned before any write occurs.
func (u *UseCases) InstallAgents(ctx context.Context, options InstallAgentsOptions) (InstallAgentsResult, error) {
	if u == nil {
		return InstallAgentsResult{}, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Agent installation request was cancelled"); err != nil {
		return InstallAgentsResult{}, err
	}
	// Installation writes env/config/profile/binding files. Keep the whole
	// operation under the same coordinator as Activate and SaveProfile so two
	// Wails calls cannot interleave backups or publish a stale profile pointer.
	u.writeMu.Lock()
	defer u.writeMu.Unlock()

	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return InstallAgentsResult{}, err
	}
	options, err = u.validateInstall(ctx, manifest, options)
	if err != nil {
		return InstallAgentsResult{}, err
	}
	// Install and update share the Agent target lock: npm must not mutate a
	// package while another request is installing or configuring that same Agent.
	unlockTasks := u.lockTasks("agent-task:", options.Agents)
	defer unlockTasks()

	autoAgents := make([]string, 0, len(options.Agents))
	for _, id := range options.Agents {
		agent := manifest.Agents[id]
		if agent.ConfigMode == "auto" {
			autoAgents = append(autoAgents, id)
		}
	}

	baseURL, providerName, err := u.prepareInstallCredentials(ctx, options, autoAgents)
	if err != nil {
		return InstallAgentsResult{}, err
	}
	probes, err := u.probeInstallProtocols(ctx, options, manifest, autoAgents)
	if err != nil {
		return InstallAgentsResult{}, err
	}

	run := installRun{
		core:         u,
		manifest:     manifest,
		options:      options,
		providerName: providerName,
		probes:       probes,
		output:       options.Output,
	}
	for _, agentID := range options.Agents {
		run.step(ctx, agentID)
	}
	return run.finish(ctx, baseURL), nil
}

// validateInstall rejects an unusable request before credentials or config are
// written. Model resolution happens here so every later step uses one value.
func (u *UseCases) validateInstall(ctx context.Context, manifest catalog.Manifest, options InstallAgentsOptions) (InstallAgentsOptions, error) {
	options.AgentVersion = strings.TrimSpace(options.AgentVersion)
	if err := install.ValidateVersion(options.AgentVersion); err != nil {
		return options, err
	}
	if options.Timeout <= 0 {
		return options, oneerrors.New(oneerrors.InvalidRequest, "timeout must be greater than zero")
	}
	if len(options.Agents) == 0 {
		return options, oneerrors.New(oneerrors.InvalidRequest, "At least one Agent is required")
	}
	if strings.TrimSpace(options.Provider) == "" {
		options.Provider = "ppio"
	}
	target, err := u.providers.Resolve(options.Provider, options.APIBaseURL)
	if err != nil {
		return options, err
	}
	profileID := strings.TrimSpace(options.ProfileID)
	if profileID != "" {
		if err := profileStore.ValidateID(profileID); err != nil {
			return options, err
		}
		options.ProfileID = profileID
	}
	if options.APIKey == "" {
		options.APIKey = target.APIKey
	}
	if options.Configure && options.ProfileID == "" {
		options.ProfileID = u.profiles.LoadActive().ID
		if options.ProfileID == "" {
			options.ProfileID = "default"
		}
	}
	for _, id := range options.Agents {
		agent, present := manifest.Agents[id]
		if !present {
			return options, oneerrors.New(oneerrors.InvalidRequest, "Unknown Agent: "+id)
		}
		if options.AgentVersion != "" && agent.Package != nil && agent.Package.Manager == "official-script" {
			return options, oneerrors.New(oneerrors.InvalidRequest, agent.Name+" official installer does not accept --agent-version")
		}
	}
	// Apply the stored mirror preference before validating, so one code path
	// covers an explicit request registry, the preference, and the official default.
	// Validate even when all commands are already installed: this prevents an
	// invalid setting from being silently accepted on a no-op run.
	options.Registry = u.packageRegistry(ctx, options.Registry)
	if _, err := install.ResolveRegistry(options.Registry); err != nil {
		return options, err
	}
	if strings.TrimSpace(options.Model) == "" {
		if options.SkipTest {
			options.Model = target.FallbackModel
		} else {
			if u.provider == nil {
				return options, oneerrors.New(oneerrors.InternalError, "Model discovery is not configured", oneerrors.WithStatus(501))
			}
			model, _, err := u.resolveProviderModel(ctx, target, options.APIKey, "")
			if err != nil {
				return options, err
			}
			options.Model = model
		}
	}
	return options, nil
}

func (u *UseCases) prepareInstallCredentials(ctx context.Context, options InstallAgentsOptions, autoAgents []string) (baseURL, providerName string, err error) {
	if !options.Configure || len(autoAgents) == 0 || options.CheckAgentOnly {
		return "", "", nil
	}
	if options.APIKey == "" {
		return "", "", oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	if err := u.providers.SaveKey(ctx, options.Provider, options.APIKey); err != nil {
		return "", "", err
	}
	target, err := u.providers.Resolve(options.Provider, options.APIBaseURL)
	if err != nil {
		return "", "", err
	}
	return target.BaseURL, target.Name, nil
}

func (u *UseCases) probeInstallProtocols(ctx context.Context, options InstallAgentsOptions, manifest catalog.Manifest, autoAgents []string) (map[string]provider.ProbeResult, error) {
	probes := make(map[string]provider.ProbeResult)
	if !options.Configure || len(autoAgents) == 0 || options.SkipTest || options.CheckAgentOnly {
		return probes, nil
	}
	target, err := u.providers.Resolve(options.Provider, options.APIBaseURL)
	if err != nil {
		return nil, err
	}
	protocols := make(map[string]bool)
	for _, agentID := range autoAgents {
		protocols[provider.ProtocolForAdapter(manifest.Agents[agentID].ConfigAdapter)] = true
	}
	ordered := make([]string, 0, len(protocols))
	for protocolID := range protocols {
		ordered = append(ordered, protocolID)
	}
	sort.Strings(ordered)
	if u.provider == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Provider probing is not configured", oneerrors.WithStatus(501))
	}
	probes, err = u.probeProtocols(ctx, ordered, options.APIKey, options.Model, target.BaseFor)
	if err != nil {
		return nil, err
	}
	u.sharpenInstallModelDiagnosis(ctx, probes, options)
	return probes, nil
}

func (u *UseCases) sharpenInstallModelDiagnosis(ctx context.Context, probes map[string]provider.ProbeResult, options InstallAgentsOptions) {
	failing := false
	for _, verdict := range probes {
		if !verdict.OK {
			failing = true
			break
		}
	}
	if !failing || u.provider == nil {
		return
	}
	target, err := u.providers.Resolve(options.Provider, options.APIBaseURL)
	if err != nil {
		return
	}
	listing, err := u.provider.ListModels(ctx, "custom", options.APIKey, target.BaseURL)
	if err != nil || !listing.OK || len(listing.Models) == 0 {
		return
	}
	if slices.Contains(listing.Models, options.Model) {
		return
	}
	sample := listing.Models
	if len(sample) > 5 {
		sample = sample[:5]
	}
	for protocolID, verdict := range probes {
		if verdict.OK {
			continue
		}
		code := oneerrors.ModelsUnsupported
		verdict.ErrorCode = &code
		verdict.Retryable = false
		verdict.Message = fmt.Sprintf("Model %q was not found in the endpoint's model list; the %s probe refused it. Available models include: %s.", options.Model, provider.ProtocolLabel(protocolID), strings.Join(sample, ", "))
		probes[protocolID] = verdict
	}
}

type installRun struct {
	core         *UseCases
	manifest     catalog.Manifest
	options      InstallAgentsOptions
	providerName string
	probes       map[string]provider.ProbeResult
	output       process.OutputListener
	results      []AgentInstallResult
	logs         []string
	nextSteps    []string
	firstCode    int
}

func (r *installRun) step(ctx context.Context, agentID string) {
	agent := r.manifest.Agents[agentID]
	if !contains(agent.Platforms, r.core.status.Platform.OS) {
		r.fail(agentID, oneerrors.New(oneerrors.PrerequisiteMissing, fmt.Sprintf("%s is not supported on %s", agent.Name, r.core.status.Platform.OS)))
		return
	}
	if agent.ConfigMode == "guide" {
		guide := agent.Guide
		r.results = append(r.results, AgentInstallResult{Agent: agentID, Status: "guide-only", Message: guide, Retryable: false})
		r.logs = append(r.logs, "## "+agentID+"\nGuide only. "+guide)
		r.nextSteps = append(r.nextSteps, guide)
		return
	}
	if err := r.configure(ctx, agentID, agent); err != nil {
		r.fail(agentID, err)
	}
}

func (r *installRun) configure(ctx context.Context, agentID string, agent catalog.Agent) error {
	// The managed runtime directories go on PATH before anything is looked up,
	// so an npm or uv installed by an earlier OneAgent run is found instead of
	// being reported missing and downloaded again.
	runtime := r.core.installRuntime(nil)
	runtime.OnOutput = func(output process.Output) {
		output.Agent = agentID
		output.Text = install.Redact(output.Text, []string{r.options.APIKey})
		for index, argument := range output.Args {
			output.Args[index] = install.Redact(argument, []string{r.options.APIKey})
		}
		if r.output != nil {
			r.output(output)
		}
	}
	installed := install.Result{Version: install.InstalledVersion(ctx, runtime, agent)}
	_, agentPresent := runtime.Runner.LookPath(agent.Command)
	officialScript := agent.Package != nil && agent.Package.Manager == "official-script"
	launchInstaller := r.options.InstallAgent && officialScript && !agentPresent
	if r.options.InstallAgent && !officialScript {
		// Install the package manager this Agent needs before installing the
		// Agent itself, otherwise a machine without Node or uv fails on a
		// prerequisite the user cannot resolve from this screen.
		bootstrapped, err := r.ensureAgentRuntime(ctx, agent, runtime)
		if err != nil {
			return err
		}
		runtime = bootstrapped
		result, err := install.InstallAgent(ctx, runtime, agent, install.Options{
			Version:  r.options.AgentVersion,
			Timeout:  r.options.Timeout,
			Registry: r.options.Registry,
		})
		if err != nil {
			return err
		}
		installed = result
		if result.Installed {
			// npm creates the managed global prefix during this install, so the
			// directory holding the Agent CLI only exists now. Persisting after a
			// runtime install alone records the runtime directory and stops there,
			// which is why `node` resolved in the user's own terminal but `codex`
			// did not.
			if runtimes, err := catalog.LoadEmbeddedRuntimes(); err == nil {
				if _, err := r.core.persistRuntimePath(ctx, runtime, runtimes); err != nil {
					return err
				}
			}
		}
		if result.Registry != "" {
			r.logs = append(r.logs, "## "+agentID+"\nregistry: "+result.Registry)
		}
	} else if agent.Command != "" {
		if _, present := r.core.runner.LookPath(agent.Command); !present {
			r.logs = append(r.logs, "## "+agentID+"\nofficial install: "+officialInstallCommand(agent))
		}
	}
	if r.options.CheckAgentOnly {
		status := "skipped"
		if (agent.Command != "" && agentPresent) || installed.Installed {
			status = "installed"
		}
		r.results = append(r.results, installResultFor(agentID, status, "", installed, true))
		r.logs = append(r.logs, "## "+agentID+"\nAgent check complete.")
		return nil
	}
	configPathValue := ""
	if r.options.Configure {
		protocolID := provider.ProtocolForAdapter(agent.ConfigAdapter)
		if verdict, found := r.probes[protocolID]; found && !verdict.OK {
			code := pointerString(verdict.ErrorCode)
			if code == "" {
				code = oneerrors.ProviderUnreachable
			}
			failure := oneerrors.New(code, fmt.Sprintf("%s: %s", agent.Name, verdict.Message), oneerrors.WithRetryable(verdict.Retryable))
			return failure
		}
		target, err := r.core.providers.Resolve(r.options.Provider, r.options.APIBaseURL)
		if err != nil {
			return err
		}
		configBase := target.BaseFor(protocolID)
		configPathValue = configPath(r.core.status.Home, r.core.status.Platform.OS, agent)
		if configPathValue == "" {
			return oneerrors.New(oneerrors.ConfigWriteFailed, fmt.Sprintf("Managed Agent %s has no configuration path", agentID))
		}
		writer := configWriter.NewWriter(r.core.status.Home, r.core.status.Platform.OS, r.core.filesystem)
		if err := writeManagedAgentConfig(ctx, writer, agentID, agent, configPathValue, r.providerName, configBase, r.options.APIKey, r.options.Model, r.options.SmallFastModel); err != nil {
			return err
		}
		if _, err := r.core.profiles.WriteAgentBinding(ctx, agentID, profileStore.BindingWriteRequest{
			Provider:   r.options.Provider,
			BaseURL:    configBase,
			Model:      r.options.Model,
			ProfileRef: r.options.ProfileID,
		}); err != nil {
			return err
		}
	}
	if launchInstaller {
		if err := r.launchOfficialInstaller(agent); err != nil {
			return err
		}
		r.logs = append(r.logs, "## "+agentID+"\nOfficial installer opened in a new terminal.")
	}
	status := "skipped"
	message := "Model configuration skipped"
	if r.options.Configure {
		status = "configured"
		message = "Configured"
	}
	r.results = append(r.results, installResultFor(agentID, status, configPathValue, installed, false))
	r.logs = append(r.logs, "## "+agentID+"\n"+message+".")
	if next := nextStep(r.core.status.Platform.OS, agentID, agent, r.options.Model); next != "" {
		r.nextSteps = append(r.nextSteps, next)
	}
	return nil
}

func (r *installRun) launchOfficialInstaller(agent catalog.Agent) error {
	if agent.Package == nil {
		return oneerrors.New(oneerrors.AgentInstallFailed, agent.Name+" has no official installer")
	}
	command := agent.Package.InstallCommand
	var argv []string
	var err error
	if r.core.status.Platform.OS == "windows" {
		command = agent.Package.WindowsInstallCommand
		argv = []string{"powershell.exe", "-NoExit", "-Command", command}
	} else {
		argv, err = terminalArgv(r.core.status.Platform.OS, command, r.core.runner.LookPath)
		if err != nil {
			return err
		}
	}
	launcher, ok := process.AsLauncher(r.core.runner)
	if !ok {
		return oneerrors.New(oneerrors.InternalError, "This build cannot open a terminal window", oneerrors.WithStatus(501))
	}
	if err := launcher.Start(argv, r.core.installRuntime(nil).Env); err != nil {
		return oneerrors.New(oneerrors.AgentInstallFailed, "Cannot open the official installer for "+agent.Name, oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

// ensureAgentRuntime installs the runtime that provides this Agent's package
// manager when the command is not resolvable, and returns a runtime whose PATH
// includes it. A manager already on the machine is left alone: OneAgent does
// not replace a user's own Node or uv.
func (r *installRun) ensureAgentRuntime(ctx context.Context, agent catalog.Agent, runtime install.Runtime) (install.Runtime, error) {
	if agent.Package == nil {
		return runtime, nil
	}
	manager := agent.Package.Manager
	if executable, present := runtime.Runner.LookPath(manager); present && executable != "" {
		return runtime, nil
	}
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		return runtime, err
	}
	runtimeID, entry, known := install.RuntimeForCommand(manifest, manager)
	if !known {
		return runtime, nil
	}
	// Agent installation can bootstrap the same runtime exposed by the runtime
	// page. Share its download lock so both paths never write one archive/tree
	// concurrently; InstallAgents already holds writeMu before reaching here.
	unlockTask := r.core.lockTask("download:" + runtimeID)
	defer unlockTask()
	// The download preference is machine-level, so an install triggered by
	// activating an Agent honors it exactly as the runtime list does.
	options := install.RuntimeOptions{PreferMirror: r.core.preferMirror(ctx)}
	updated, installed, err := install.EnsureRuntime(ctx, runtime, r.core.httpDoer, runtimeID, entry, options)
	if err != nil {
		return runtime, err
	}
	if installed {
		// Record the new directory on the login PATH now rather than at the end
		// of the run: a failure here must surface against the Agent that needed
		// it, not silently leave the runtime off PATH for the next session.
		if _, err := r.core.persistRuntimePath(ctx, updated, manifest); err != nil {
			return updated, err
		}
	}
	return updated, nil
}

func (r *installRun) fail(agentID string, err error) {
	converted := oneerrors.As(err)
	if r.firstCode == 0 {
		r.firstCode = converted.ExitCode
	}
	r.results = append(r.results, AgentInstallResult{
		Agent: agentID, Status: "failed", Code: converted.ExitCode,
		ErrorCode: converted.Code, Message: converted.Message, Retryable: converted.Retryable,
	})
	r.logs = append(r.logs, "## "+agentID+"\n"+converted.Message)
}

func (r *installRun) finish(ctx context.Context, baseURL string) InstallAgentsResult {
	chosen := chooseInstallProbe(r.probes)
	for _, protocolID := range sortedProbeIDs(r.probes) {
		verdict := r.probes[protocolID]
		if verdict.OK {
			continue
		}
		if r.firstCode == 0 {
			r.firstCode = oneerrors.ExitCodes[pointerString(verdict.ErrorCode)]
			if r.firstCode == 0 {
				r.firstCode = oneerrors.ExitCodes[oneerrors.ProviderUnreachable]
			}
		}
		r.logs = append(r.logs, "## provider ("+protocolID+")\n"+verdict.Message)
	}
	failed := false
	for _, result := range r.results {
		if result.Status == "failed" {
			failed = true
			break
		}
	}
	probeOK := chosen == nil || chosen.OK
	if !failed && probeOK && !r.options.CheckAgentOnly {
		profileAPIKey := r.options.APIKey
		// Managed Providers own their credentials. `custom` has no persistent
		// Provider record, so keep its explicit key in the profile secret store.
		if r.options.Provider != "custom" {
			profileAPIKey = ""
		}
		profileProtocol := ""
		for _, agentID := range r.options.Agents {
			if agent, ok := r.manifest.Agents[agentID]; ok && agent.ConfigMode == "auto" {
				profileProtocol = provider.ProtocolForAdapter(agent.ConfigAdapter)
				break
			}
		}
		if _, err := r.core.profiles.WriteActive(ctx, profileStore.ActiveRequest{
			ProfileID: r.options.ProfileID,
			Label:     r.options.ProfileLabel,
			Configure: r.options.Configure,
			Provider:  r.options.Provider,
			BaseURL:   baseURL,
			Model:     r.options.Model,
			APIKey:    profileAPIKey,
			Protocol:  profileProtocol,
		}); err != nil {
			converted := oneerrors.As(err)
			failed = true
			if r.firstCode == 0 {
				r.firstCode = converted.ExitCode
			}
			// Config files are the source of truth for running Agents; preserve
			// them and surface profile bookkeeping failure in the redacted log.
			r.logs = append(r.logs, "## profile\n"+converted.Message)
		}
	}
	return InstallAgentsResult{
		OK:      !failed && probeOK,
		Code:    r.firstCode,
		Results: r.results,
		Log:     install.Redact(strings.Join(r.logs, "\n\n"), []string{r.options.APIKey}),
		Next:    strings.Join(r.nextSteps, "\n"),
		Probe:   chosen,
		Probes:  r.probes,
	}
}

func installResultFor(agentID, status, path string, installed install.Result, checkOnly bool) AgentInstallResult {
	return AgentInstallResult{
		Agent:     agentID,
		Status:    status,
		Config:    path,
		Installed: installed.Installed,
		Version:   installed.Version,
		Registry:  installed.Registry,
		// Only success rows are built here; a failure goes through installRun.fail,
		// which carries the error's own Retryable through. Nothing to retry on a
		// row that succeeded.
		Retryable: false,
		checkOnly: checkOnly,
	}
}

func officialInstallCommand(agent catalog.Agent) string {
	if agent.Package == nil {
		return "Unsupported package manager: missing"
	}
	switch agent.Package.Manager {
	case "npm":
		return "npm install -g " + agent.Package.Name
	case "uv":
		return "uv tool install --force --python " + install.AiderPythonVersion + " " + agent.Package.Name
	case "official-script":
		return agent.Package.InstallCommand
	default:
		manager := agent.Package.Manager
		if manager == "" {
			manager = "missing"
		}
		return "Unsupported package manager: " + manager
	}
}

func chooseInstallProbe(probes map[string]provider.ProbeResult) *provider.ProbeResult {
	var chosen *provider.ProbeResult
	for _, protocolID := range sortedProbeIDs(probes) {
		verdict := probes[protocolID]
		if chosen == nil || (!verdict.OK && chosen.OK) {
			candidate := verdict
			chosen = &candidate
		}
	}
	return chosen
}

func sortedProbeIDs(probes map[string]provider.ProbeResult) []string {
	ids := make([]string, 0, len(probes))
	for id := range probes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
