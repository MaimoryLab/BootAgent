package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/install"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/profile"
	"github.com/MaimoryLab/OneAgent/desktop/internal/provider"
)

// Install checks, installs and configures the requested Agents.
//
// One request covers several Agents, and one failing Agent must not abandon the
// rest: each is attempted independently and reports its own outcome, while the
// exit code carries the first failure so a script sees a reason rather than a
// generic one.
func (s *Service) Install(options InstallOptions) (InstallResult, error) {
	manifest, err := catalog.Load()
	if err != nil {
		return InstallResult{}, err
	}

	options, err = s.validateInstall(manifest, options)
	if err != nil {
		return InstallResult{}, err
	}

	autoAgents := []string{}
	for _, id := range options.Agents {
		if agent, _ := manifest.Agent(id); !agent.GuideOnly() {
			autoAgents = append(autoAgents, id)
		}
	}

	baseURL, providerName, err := s.prepareCredentials(manifest, options, autoAgents)
	if err != nil {
		return InstallResult{}, err
	}

	probes, err := s.probeProtocols(manifest, options, autoAgents)
	if err != nil {
		return InstallResult{}, err
	}

	run := &installRun{service: s, manifest: manifest, options: options,
		providerName: providerName, probes: probes}
	for _, agentID := range options.Agents {
		run.step(agentID)
	}

	result := run.finish(baseURL)
	return result, nil
}

// validateInstall refuses a request that cannot be carried out, before anything is
// written. Resolving the model belongs here too: an empty model reaches this far
// when a caller omits it, and writing "" into a config produces a file that looks
// configured and cannot answer a request.
func (s *Service) validateInstall(manifest *catalog.Manifest, options InstallOptions) (InstallOptions, error) {
	if options.LockedVersion && options.Latest {
		return options, oerr.New("INVALID_REQUEST", "locked_version and latest cannot be enabled together")
	}
	if options.Timeout <= 0 {
		return options, oerr.New("INVALID_REQUEST", "timeout must be greater than zero")
	}
	if len(options.Agents) == 0 {
		return options, oerr.New("INVALID_REQUEST", "At least one Agent is required")
	}

	profileAgents := options.ProfileAgents
	if len(profileAgents) == 0 {
		profileAgents = options.Agents
	}
	listed := map[string]bool{}
	for _, id := range profileAgents {
		listed[id] = true
	}
	for _, id := range options.Agents {
		if !listed[id] {
			return options, oerr.New("INVALID_REQUEST", "profile_agents must include every requested Agent")
		}
	}
	options.ProfileAgents = profileAgents

	for _, id := range append(append([]string{}, options.Agents...), profileAgents...) {
		if _, present := manifest.Agent(id); !present {
			return options, oerr.Newf("INVALID_REQUEST", "Unknown Agent: %s", id)
		}
	}

	// Validated here rather than only inside the per-Agent install, which returns
	// early for an Agent that is already present. An unusable registry was
	// therefore accepted silently whenever nothing needed installing, so the
	// request looked successful and the setting was never applied or reported.
	// A browser review found that as an HTTP 200; keeping the check at this level
	// is what stops it coming back.
	if _, err := install.ResolveRegistry(options.Registry); err != nil {
		return options, err
	}

	if options.Model == "" {
		if options.SkipTest {
			options.Model = catalog.FallbackProbeModel(options.Provider)
		} else {
			options.Model = s.Probes.ResolveModel(
				options.Provider, options.APIKey, "", options.APIBaseURL,
			)
		}
	}
	return options, nil
}

// prepareCredentials writes the env files the selected Agents need, before any
// config references them.
func (s *Service) prepareCredentials(
	manifest *catalog.Manifest, options InstallOptions, autoAgents []string,
) (baseURL, providerName string, err error) {
	if !options.Configure || len(autoAgents) == 0 || options.CheckAgentOnly {
		return "", "", nil
	}
	if options.APIKey == "" {
		return "", "", oerr.New("INVALID_REQUEST", "API key is required")
	}
	baseURL, err = provider.Base(options.Provider, options.APIBaseURL)
	if err != nil {
		return "", "", err
	}
	providerName = "Custom"
	if known, present := catalog.Providers[options.Provider]; present {
		providerName = known.Name
	}

	wroteAny := false
	for _, agentID := range autoAgents {
		agent, _ := manifest.Agent(agentID)
		if !config.NeedsEnvFile(agent) {
			continue
		}
		if _, err := s.Writer.WriteAgentEnv(agentID, agent, config.Settings{
			AgentID: agentID, ProviderName: providerName, BaseURL: baseURL,
			APIKey: options.APIKey, Model: options.Model,
			SmallFastModel: options.SmallFastModel,
		}); err != nil {
			return "", "", err
		}
		wroteAny = true
	}
	if wroteAny {
		// Configs written by earlier versions still name ONEAGENT_API_KEY, so the
		// shared file stays until those have been rewritten.
		if _, err := s.Writer.WriteSharedEnv(options.APIKey, baseURL); err != nil {
			return "", "", err
		}
	}
	return baseURL, providerName, nil
}

// probeProtocols verifies the model over each protocol the selected Agents speak,
// before any config is written.
//
// A model that answers Chat Completions may still reject Responses or Anthropic
// Messages, and writing a config for a pair the endpoint refuses only moves the
// failure into the Agent, where the user cannot see what happened.
func (s *Service) probeProtocols(
	manifest *catalog.Manifest, options InstallOptions, autoAgents []string,
) (map[string]provider.Verdict, error) {
	probes := map[string]provider.Verdict{}
	if !options.Configure || len(autoAgents) == 0 || options.SkipTest || options.CheckAgentOnly {
		return probes, nil
	}

	protocols := map[string]bool{}
	for _, agentID := range autoAgents {
		agent, _ := manifest.Agent(agentID)
		protocols[catalog.AgentProtocol(agent.ConfigAdapter)] = true
	}
	ordered := []string{}
	for protocol := range protocols {
		ordered = append(ordered, protocol)
	}
	sort.Strings(ordered)

	for _, protocol := range ordered {
		verdict, err := s.Probes.Probe(provider.ProbeRequest{
			Protocol: protocol, Provider: options.Provider, CustomBase: options.APIBaseURL,
			APIKey: options.APIKey, Model: options.Model,
		})
		if err != nil {
			return nil, err
		}
		probes[protocol] = verdict
	}
	s.sharpenModelDiagnosis(probes, options)
	return probes, nil
}

// sharpenModelDiagnosis relabels probe failures that really mean "unknown model".
//
// Endpoints refuse an unknown model with the same 404/400 shapes they use for an
// unsupported protocol, so a bare verdict reads "model does not support <protocol>"
// and sends the user hunting a protocol mismatch. When discovery succeeds and the
// model is absent from the list, the verdicts are rewritten to name the real
// problem. Discovery failing, or listing the model, leaves them untouched: then
// "wrong model" and "unreachable endpoint" are indistinguishable, and blocking on a
// guess would lock offline users out.
func (s *Service) sharpenModelDiagnosis(probes map[string]provider.Verdict, options InstallOptions) {
	failing := []string{}
	for protocol, verdict := range probes {
		if !verdict.OK {
			failing = append(failing, protocol)
		}
	}
	if len(failing) == 0 {
		return
	}
	listing, err := s.Probes.ListModels(options.Provider, options.APIBaseURL, options.APIKey)
	if err != nil || !listing.OK || len(listing.Models) == 0 {
		return
	}
	for _, model := range listing.Models {
		if model == options.Model {
			return
		}
	}

	sample := listing.Models
	if len(sample) > 5 {
		sample = sample[:5]
	}
	for _, protocol := range failing {
		verdict := probes[protocol]
		verdict.ErrorCode = "MODELS_UNSUPPORTED"
		verdict.Retryable = false
		verdict.Message = fmt.Sprintf(
			"Model %s was not found in the endpoint's model list; the %s probe refused it. "+
				"Available models include: %s.",
			provider.PythonRepr(options.Model), provider.ProtocolLabel(verdict.Protocol),
			strings.Join(sample, ", "),
		)
		probes[protocol] = verdict
	}
}

// installRun accumulates what the per-Agent steps produce.
type installRun struct {
	service      *Service
	manifest     *catalog.Manifest
	options      InstallOptions
	providerName string
	probes       map[string]provider.Verdict

	results   []AgentResult
	logs      []string
	nextSteps []string
	firstCode int
}

// step handles one Agent. A failure is recorded and the loop continues: with
// several Agents selected, abandoning the rest because one is unsupported on this
// platform would be worse than reporting each outcome.
func (r *installRun) step(agentID string) {
	agent, _ := r.manifest.Agent(agentID)

	if !supportedOn(agent, r.service.Runtime.OSID) {
		failure := oerr.Newf("PREREQUISITE_MISSING", "%s is not supported on %s", agent.Name, r.service.Runtime.OSID)
		r.fail(agentID, failure)
		return
	}
	if agent.GuideOnly() {
		guide := guideText(agent)
		r.results = append(r.results, AgentResult{Agent: agentID, Status: "guide-only", Message: guide})
		r.logs = append(r.logs, "## "+agentID+"\nGuide only. "+guide)
		r.nextSteps = append(r.nextSteps, guide)
		return
	}
	if err := r.configure(agentID, agent); err != nil {
		r.fail(agentID, err)
	}
}

// configure runs the steps that can fail for one Agent.
func (r *installRun) configure(agentID string, agent catalog.Agent) error {
	installed := install.Result{Version: install.InstalledVersion(r.service.Runtime, agent)}
	if agent.Package != nil {
		installed.LockedVersion = agent.Package.Version
	}

	if r.options.InstallAgent {
		result, err := install.LockedAgent(r.service.Runtime, agentID, agent, install.Options{
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
			// Recorded so the user can tell afterwards which registry a package
			// actually came from.
			r.logs = append(r.logs, "## "+agentID+"\nregistry: "+result.Registry)
		}
	} else if agent.Command != "" {
		if _, present := r.service.Runtime.Which(agent.Command); !present {
			r.logs = append(r.logs, "## "+agentID+"\nofficial install: "+officialInstallCommand(agent))
		}
	}

	if r.options.CheckAgentOnly {
		_, present := r.service.Runtime.Which(agent.Command)
		status := "skipped"
		if (agent.Command != "" && present) || installed.Installed {
			status = "installed"
		}
		r.results = append(r.results, resultFor(agentID, status, "", installed))
		r.logs = append(r.logs, "## "+agentID+"\nAgent check complete.")
		return nil
	}

	configPath := ""
	if r.options.Configure {
		protocol := catalog.AgentProtocol(agent.ConfigAdapter)
		if verdict, taken := r.probes[protocol]; taken && !verdict.OK {
			code := verdict.ErrorCode
			if code == "" {
				code = "PROVIDER_UNREACHABLE"
			}
			failure := oerr.Newf(code, "%s: %s", agent.Name, verdict.Message)
			if verdict.Retryable {
				failure = failure.Set(oerr.WithRetryable())
			}
			return failure
		}
		configBase, err := provider.ConfigBase(r.options.Provider, r.options.APIBaseURL, protocol)
		if err != nil {
			return err
		}
		written, err := r.service.Writer.Write(agent, config.Settings{
			AgentID: agentID, ProviderName: r.providerName, BaseURL: configBase,
			APIKey: r.options.APIKey, Model: r.options.Model,
			SmallFastModel: r.options.SmallFastModel,
		})
		if err != nil {
			return err
		}
		configPath = written
		// Recorded only after the config write succeeded, so a failed write never
		// leaves a binding claiming a state the Agent's own config does not have.
		if _, err := r.service.Store.WriteBinding(
			agentID, r.options.Provider, configBase, r.options.Model, "",
		); err != nil {
			return err
		}
	}

	status := "skipped"
	message := "Model configuration skipped"
	if r.options.Configure {
		status = "configured"
		message = "Configured"
	}
	r.results = append(r.results, resultFor(agentID, status, configPath, installed))
	r.logs = append(r.logs, "## "+agentID+"\n"+message+".")
	if step := nextStep(r.service.Runtime, agent, agentID, r.options.Model); step != "" {
		r.nextSteps = append(r.nextSteps, step)
	}
	return nil
}

// fail records one Agent's failure and keeps the first exit code.
func (r *installRun) fail(agentID string, err error) {
	var converted *oerr.Error
	if !errors.As(err, &converted) {
		converted = oerr.Newf("INTERNAL_ERROR", "%v", err)
	}
	if r.firstCode == 0 {
		r.firstCode = converted.ExitCode
	}
	r.results = append(r.results, AgentResult{
		Agent: agentID, Status: "failed", Code: converted.ExitCode,
		ErrorCode: converted.Code, Message: converted.Message, Retryable: converted.Retryable,
	})
	r.logs = append(r.logs, "## "+agentID+"\n"+converted.Message)
}

// finish assembles the response and records the profile when nothing failed.
func (r *installRun) finish(baseURL string) InstallResult {
	// Surface the failing protocol first: with several Agents selected the GUI
	// shows one probe, and the actionable one is the protocol that refused.
	var chosen *provider.Verdict
	for _, protocol := range sortedKeys(r.probes) {
		verdict := r.probes[protocol]
		if chosen == nil || (!verdict.OK && chosen.OK) {
			copied := verdict
			chosen = &copied
		}
	}
	for _, protocol := range sortedKeys(r.probes) {
		verdict := r.probes[protocol]
		if verdict.OK {
			continue
		}
		if r.firstCode == 0 {
			r.firstCode = oerr.ExitCodeFor(verdict.ErrorCode)
		}
		r.logs = append(r.logs, "## provider ("+protocol+")\n"+verdict.Message)
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
		if _, err := r.service.Store.Activate(profile.ActivateRequest{
			AgentIDs: r.options.ProfileAgents, Configure: r.options.Configure,
			Provider: r.options.Provider, BaseURL: baseURL,
			Model: r.options.Model, APIKey: r.options.APIKey,
		}); err != nil {
			// The Agents are configured at this point, so a profile write failure
			// is reported without undoing them: the configs on disk are what the
			// Agents read, and the profile is bookkeeping.
			r.logs = append(r.logs, "## profile\n"+err.Error())
		}
	}

	return InstallResult{
		OK:      !failed && probeOK,
		Code:    r.firstCode,
		Results: r.results,
		Log:     install.Redact(strings.Join(r.logs, "\n\n"), []string{r.options.APIKey}),
		Next:    strings.Join(r.nextSteps, "\n"),
		Probe:   chosen,
		Probes:  r.probes,
	}
}

func sortedKeys(probes map[string]provider.Verdict) []string {
	keys := make([]string, 0, len(probes))
	for key := range probes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resultFor(agentID, status, configPath string, installed install.Result) AgentResult {
	return AgentResult{
		Agent: agentID, Status: status, Config: configPath,
		Installed: installed.Installed, Version: installed.Version,
		LockedVersion: installed.LockedVersion, Registry: installed.Registry,
	}
}

func supportedOn(agent catalog.Agent, osID string) bool {
	for _, platform := range agent.Platforms {
		if platform == osID {
			return true
		}
	}
	return false
}

// guideText renders the manifest's guide field, which is a string today but typed
// as any so a manifest carrying a richer shape does not silently read as empty.
func guideText(agent catalog.Agent) string {
	if text, ok := agent.Guide.(string); ok {
		return text
	}
	if agent.Guide == nil {
		return ""
	}
	return fmt.Sprintf("%v", agent.Guide)
}

// officialInstallCommand is what the user would run themselves. Shown rather than
// executed when the request did not ask OneAgent to install.
func officialInstallCommand(agent catalog.Agent) string {
	if agent.Package == nil {
		return "Unsupported package manager: missing"
	}
	switch agent.Package.Manager {
	case "npm":
		return "npm install -g " + agent.Package.Name + "@" + agent.Package.Version
	case "uv":
		return "uv tool install --force --python python3.12 --no-python-downloads " +
			agent.Package.Name + "==" + agent.Package.Version
	default:
		manager := agent.Package.Manager
		if manager == "" {
			manager = "missing"
		}
		return "Unsupported package manager: " + manager
	}
}
