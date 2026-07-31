package app

import (
	"context"
	"fmt"
	"path/filepath"
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

// InstallAgentsOptions is one installation request. The use case deliberately
// keeps the request independent of Wails or CLI transport types so both entry
// points exercise the same validation and write ordering.
type InstallAgentsOptions struct {
	Agents         []string
	ProfileAgents  []string
	Provider       string
	APIBaseURL     string
	APIKey         string
	Model          string
	SmallFastModel string
	Configure      bool
	InstallAgent   bool
	CheckAgentOnly bool
	SkipTest       bool
	LockedVersion  bool
	Latest         bool
	Timeout        time.Duration
	Registry       string
	Output         process.OutputListener
	// ProfileID is optional. It is used only as the binding's profile reference;
	// the active profile store keeps its existing/default id semantics.
	ProfileID string
}

// InstallOptions is retained as a convenient compatibility name for callers
// that used the earlier Go orchestration prototype.
type InstallOptions = InstallAgentsOptions

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

// AgentResult is the short compatibility name used by the first Go port.
type AgentResult = AgentInstallResult

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

// InstallResult is retained for callers of the earlier orchestration API.
type InstallResult = InstallAgentsResult

// InstallAgents validates, installs and configures the requested Agents. A
// failure for one Agent is recorded and the remaining Agents are still tried;
// request-shape and manifest errors are returned before any write occurs.
func (u *UseCases) InstallAgents(ctx context.Context, options InstallAgentsOptions) (InstallAgentsResult, error) {
	if u == nil {
		return InstallAgentsResult{}, oneerrors.New(oneerrors.InternalError, "Agent service is not configured", oneerrors.WithStatus(501))
	}
	if ctx == nil {
		ctx = context.Background()
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

	autoAgents := make([]string, 0, len(options.Agents))
	for _, id := range options.Agents {
		agent := manifest.Agents[id]
		if agent.ConfigMode == "auto" {
			autoAgents = append(autoAgents, id)
		}
	}

	baseURL, providerName, err := u.prepareInstallCredentials(ctx, options, manifest, autoAgents)
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
	if options.LockedVersion && options.Latest {
		return options, oneerrors.New(oneerrors.InvalidRequest, "locked_version and latest cannot be enabled together")
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
	profileAgents := append([]string(nil), options.ProfileAgents...)
	if len(profileAgents) == 0 {
		profileAgents = append([]string(nil), options.Agents...)
	}
	options.ProfileAgents = profileAgents
	listed := make(map[string]bool, len(profileAgents))
	for _, id := range profileAgents {
		listed[id] = true
	}
	for _, id := range options.Agents {
		if !listed[id] {
			return options, oneerrors.New(oneerrors.InvalidRequest, "profile_agents must include every requested Agent")
		}
	}
	for _, id := range append(append([]string(nil), options.Agents...), profileAgents...) {
		if _, present := manifest.Agents[id]; !present {
			return options, oneerrors.New(oneerrors.InvalidRequest, "Unknown Agent: "+id)
		}
	}
	// Validate the registry even when all commands are already installed. This
	// prevents an invalid setting from being silently accepted on a no-op run.
	if _, err := install.ResolveRegistry(options.Registry); err != nil {
		return options, err
	}
	if strings.TrimSpace(options.Model) == "" {
		if options.SkipTest {
			options.Model = provider.FallbackProbeModel(options.Provider)
		} else {
			if u.provider == nil {
				return options, oneerrors.New(oneerrors.InternalError, "Model discovery is not configured", oneerrors.WithStatus(501))
			}
			model, err := u.provider.ResolveProbeModel(ctx, options.Provider, options.APIKey, "", options.APIBaseURL)
			if err != nil {
				return options, err
			}
			options.Model = model
		}
	}
	return options, nil
}

func (u *UseCases) prepareInstallCredentials(ctx context.Context, options InstallAgentsOptions, manifest catalog.Manifest, autoAgents []string) (baseURL, providerName string, err error) {
	if !options.Configure || len(autoAgents) == 0 || options.CheckAgentOnly {
		return "", "", nil
	}
	if options.APIKey == "" {
		return "", "", oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	baseURL, err = provider.ProviderBase(options.Provider, options.APIBaseURL)
	if err != nil {
		return "", "", err
	}
	providerName = "Custom"
	if definition, found := catalog.ProviderByID(options.Provider); found {
		providerName = definition.Name
	}
	wroteAny := false
	for _, agentID := range autoAgents {
		agent := manifest.Agents[agentID]
		if !needsAgentEnv(agent) {
			continue
		}
		path := agentEnvPath(u.status.Home, u.status.Platform.OS, agentID)
		if err := configWriter.WriteAgentEnv(ctx, u.filesystem, path, u.status.Platform.OS, agentID, options.APIKey, baseURL, options.Model, options.SmallFastModel, agent.EnvVars); err != nil {
			return "", "", err
		}
		wroteAny = true
	}
	if wroteAny {
		// Keep the legacy shared file while configurations written by older
		// versions may still reference ONEAGENT_API_KEY.
		path := filepath.Join(u.status.Home, ".oneagent", envFilename(u.status.Platform.OS))
		if err := configWriter.WriteSharedEnv(ctx, u.filesystem, path, u.status.Platform.OS, options.APIKey, baseURL); err != nil {
			return "", "", err
		}
	}
	return baseURL, providerName, nil
}

func (u *UseCases) probeInstallProtocols(ctx context.Context, options InstallAgentsOptions, manifest catalog.Manifest, autoAgents []string) (map[string]provider.ProbeResult, error) {
	probes := make(map[string]provider.ProbeResult)
	if !options.Configure || len(autoAgents) == 0 || options.SkipTest || options.CheckAgentOnly {
		return probes, nil
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
	for _, protocolID := range ordered {
		if u.provider == nil {
			return nil, oneerrors.New(oneerrors.InternalError, "Provider probing is not configured", oneerrors.WithStatus(501))
		}
		verdict, err := u.provider.Probe(ctx, protocolID, options.Provider, options.APIKey, options.Model, options.APIBaseURL)
		if err != nil {
			return nil, err
		}
		probes[protocolID] = verdict
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
	listing, err := u.provider.ListModels(ctx, options.Provider, options.APIKey, options.APIBaseURL)
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
	runtime := install.NewRuntime(r.core.status.Home, r.core.status.Platform, r.core.runner, r.core.environment)
	runtime.OnOutput = func(output process.Output) {
		output.Text = install.Redact(output.Text, []string{r.options.APIKey})
		for index, argument := range output.Args {
			output.Args[index] = install.Redact(argument, []string{r.options.APIKey})
		}
		if r.output != nil {
			r.output(output)
		}
	}
	installed := install.Result{Version: install.InstalledVersion(ctx, runtime, agent)}
	if agent.Package != nil {
		installed.LockedVersion = agent.Package.Version
	}
	if r.options.InstallAgent {
		result, err := install.InstallLockedAgent(ctx, runtime, agentID, agent, install.Options{
			EnforceLocked: r.options.LockedVersion,
			Latest:        r.options.Latest,
			Timeout:       r.options.Timeout,
			Registry:      r.options.Registry,
		})
		if err != nil {
			return err
		}
		installed = result
		if result.Registry != "" {
			r.logs = append(r.logs, "## "+agentID+"\nregistry: "+result.Registry)
		}
	} else if agent.Command != "" {
		if _, present := r.core.runner.LookPath(agent.Command); !present {
			r.logs = append(r.logs, "## "+agentID+"\nofficial install: "+officialInstallCommand(agent))
		}
	}
	if r.options.CheckAgentOnly {
		_, present := r.core.runner.LookPath(agent.Command)
		status := "skipped"
		if (agent.Command != "" && present) || installed.Installed {
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
		configBase, err := provider.ProviderConfigBase(r.options.Provider, r.options.APIBaseURL, protocolID)
		if err != nil {
			return err
		}
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
		if _, err := r.core.profiles.WriteActive(ctx, profileStore.ActiveRequest{
			Agents:    r.options.ProfileAgents,
			Configure: r.options.Configure,
			Provider:  r.options.Provider,
			BaseURL:   baseURL,
			Model:     r.options.Model,
			APIKey:    r.options.APIKey,
		}); err != nil {
			// Config files are the source of truth for running Agents; preserve
			// them and surface profile bookkeeping failure in the redacted log.
			r.logs = append(r.logs, "## profile\n"+oneerrors.As(err).Message)
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
		Agent:         agentID,
		Status:        status,
		Config:        path,
		Installed:     installed.Installed,
		Version:       installed.Version,
		LockedVersion: installed.LockedVersion,
		Registry:      installed.Registry,
		Retryable:     false,
		checkOnly:     checkOnly,
	}
}

func officialInstallCommand(agent catalog.Agent) string {
	if agent.Package == nil {
		return "Unsupported package manager: missing"
	}
	switch agent.Package.Manager {
	case "npm":
		return "npm install -g " + agent.Package.Name + "@" + agent.Package.Version
	case "uv":
		return "uv tool install --force --python python3.12 --no-python-downloads " + agent.Package.Name + "==" + agent.Package.Version
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
			copy := verdict
			chosen = &copy
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
