package app

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/profile"
	"github.com/MaimoryLab/OneAgent/desktop/internal/provider"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/testutil"
)

// serviceFor builds a Service whose subprocesses and HTTP are both fakes, with
// `present` deciding which commands the machine appears to have.
func serviceFor(t *testing.T, home string, present bool, transport provider.Doer) *Service {
	t.Helper()
	runner := testutil.NewRecordingRunner(t, testutil.Succeed("1.0.0"))
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{"HOME": home}),
		runtime.WithRunner(runner.Runner()),
		runtime.WithLookup(func(name string) (string, bool) {
			if !present {
				return "", false
			}
			return "/usr/local/bin/" + name, true
		}),
	)
	if transport == nil {
		transport = cannedTransport{status: 200, body: "{}"}
	}
	return &Service{
		Runtime: rt,
		Writer:  config.NewWriter(rt),
		Store:   profile.NewStore(rt),
		Probes:  &provider.Client{HTTP: transport},
	}
}

func baseOptions() InstallOptions {
	return InstallOptions{
		Agents: []string{"codex"}, Provider: "ppio", APIKey: "sk-test",
		Model: "m", Configure: true, SkipTest: true, Timeout: 30 * time.Second,
	}
}

func TestARequestThatCannotBeCarriedOutIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	cases := map[string]func(*InstallOptions){
		"locked and latest together": func(o *InstallOptions) { o.LockedVersion, o.Latest = true, true },
		"no timeout":                 func(o *InstallOptions) { o.Timeout = 0 },
		"negative timeout":           func(o *InstallOptions) { o.Timeout = -time.Second },
		"no agents":                  func(o *InstallOptions) { o.Agents = nil },
		"unknown agent":              func(o *InstallOptions) { o.Agents = []string{"not-an-agent"} },
		"unknown profile agent": func(o *InstallOptions) {
			o.ProfileAgents = []string{"codex", "not-an-agent"}
		},
		"profile narrower than the install": func(o *InstallOptions) {
			o.Agents = []string{"codex", "aider"}
			o.ProfileAgents = []string{"codex"}
		},
		"registry over plain http": func(o *InstallOptions) { o.Registry = "http://mirror.example.com/" },
		"registry with credentials": func(o *InstallOptions) {
			o.Registry = "https://user:pass@mirror.example.com/"
		},
		"no key while configuring": func(o *InstallOptions) { o.APIKey = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			service := serviceFor(t, home, true, nil)
			options := baseOptions()
			mutate(&options)

			if _, err := service.Install(options); err == nil {
				t.Fatal("the request was accepted")
			}
			// Nothing was written, which is the point of validating first: a
			// half-applied request is worse than a refused one.
			if files := collectTree(t, home); len(files) != 0 {
				t.Errorf("a refused request left %d files behind: %v", len(files), keysOf(files))
			}
		})
	}
}

func TestAnUnusableRegistryIsRefusedEvenWhenNothingNeedsInstalling(t *testing.T) {
	// The defect a browser review found, ported forward. resolve_registry used to
	// be reached only inside the per-Agent install, which returns early for an
	// Agent that is already present -- so the request answered 200 and the setting
	// was silently dropped. The check belongs beside the Agent id validation:
	// either the request is acceptable or it is not, regardless of what needs
	// installing.
	service := serviceFor(t, t.TempDir(), true, nil)
	options := baseOptions()
	options.InstallAgent = true
	options.Registry = "http://evil.test/"

	_, err := service.Install(options)
	if err == nil {
		t.Fatal("an http registry was accepted")
	}
	var converted *oerr.Error
	if !errors.As(err, &converted) || converted.Code != "INVALID_REQUEST" {
		t.Fatalf("error = %v, want INVALID_REQUEST", err)
	}

	// And the credential in a registry URL is not echoed back in the message.
	options.Registry = "https://user:sk-secret@evil.test/"
	_, err = service.Install(options)
	if err == nil {
		t.Fatal("a registry carrying credentials was accepted")
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("the message echoes the credential: %v", err)
	}
}

func TestOneFailingAgentDoesNotAbandonTheRest(t *testing.T) {
	// With several Agents selected, giving up because one is unsupported here
	// would hide every other outcome.
	service := serviceFor(t, t.TempDir(), true, nil)
	// Windows-only prerequisites are not what makes this fail; an Agent absent
	// from this platform's list is.
	manifest := catalog.MustLoad()
	unsupported := ""
	for _, id := range manifest.AutoAgents() {
		agent, _ := manifest.Agent(id)
		if !supportedOn(agent, "linux") {
			unsupported = id
			break
		}
	}
	if unsupported == "" {
		t.Skip("every auto Agent supports linux, so this cannot be exercised here")
	}

	options := baseOptions()
	options.Agents = []string{unsupported, "codex"}
	options.ProfileAgents = options.Agents
	result, err := service.Install(options)
	if err != nil {
		t.Fatalf("the request itself must not fail: %v", err)
	}
	if result.OK {
		t.Error("a run with a failed Agent reported ok")
	}
	if len(result.Results) != 2 {
		t.Fatalf("got %d results, want one per Agent", len(result.Results))
	}
	if result.Results[0].Status != "failed" || result.Results[1].Status != "configured" {
		t.Errorf("statuses = %q/%q, want failed then configured",
			result.Results[0].Status, result.Results[1].Status)
	}
	if result.Code == 0 {
		t.Error("the exit code does not carry the failure")
	}
}

func TestAProfileIsNotRecordedWhenSomethingFailed(t *testing.T) {
	// The profile says what the machine is pointed at. Writing it after a failed
	// run would claim a state the Agent configs do not have.
	home := t.TempDir()
	service := serviceFor(t, home, true, cannedTransport{status: 401, body: `{"error":"bad key"}`})
	options := baseOptions()
	options.SkipTest = false

	result, err := service.Install(options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OK {
		t.Fatal("a refused key reported ok")
	}
	if _, reason, err := service.Store.Load(); err != nil || reason != "" {
		t.Fatalf("unexpected profile state: reason=%q err=%v", reason, err)
	} else if service.Store.ActiveID() != "" {
		t.Error("a profile was recorded despite the failure")
	}
}

func TestTheKeyIsRedactedFromTheLog(t *testing.T) {
	// npm echoes its environment on some failures, and the log is shown and stored.
	const key = "sk-must-not-appear"
	service := serviceFor(t, t.TempDir(), false, nil)
	options := baseOptions()
	options.APIKey = key
	options.Agents = []string{"codex"}
	options.ProfileAgents = options.Agents

	result, err := service.Install(options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result.Log, key) {
		t.Errorf("the log carries the key: %q", result.Log)
	}
	// The log is not empty, so the check above is not vacuous.
	if result.Log == "" {
		t.Error("no log was produced, so the redaction proves nothing")
	}
}

func TestAMissingAgentIsReportedWithTheOfficialCommandRatherThanInstalled(t *testing.T) {
	// Without install_agent, OneAgent must not install anything behind the user's
	// back -- it says what they would run.
	service := serviceFor(t, t.TempDir(), false, nil)
	options := baseOptions()
	options.Agents = []string{"codex", "aider"}
	options.ProfileAgents = options.Agents

	result, err := service.Install(options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Log, "official install: npm install -g") {
		t.Errorf("the log does not name the npm command: %q", result.Log)
	}
	if !strings.Contains(result.Log, "official install: uv tool install") {
		t.Errorf("the log does not name the uv command: %q", result.Log)
	}
	// Pinned, never floating: the manifest forbids `latest` and the integrity
	// check cannot apply to it.
	if strings.Contains(result.Log, "@latest") {
		t.Errorf("an official command suggested a floating version: %q", result.Log)
	}
}

func TestTheOfficialCommandComesFromTheManifest(t *testing.T) {
	manifest := catalog.MustLoad()
	codex, _ := manifest.Agent("codex")
	if got := officialInstallCommand(codex); !strings.Contains(got, codex.Package.Version) {
		t.Errorf("officialInstallCommand = %q, want the pinned version", got)
	}
	aider, _ := manifest.Agent("aider")
	if got := officialInstallCommand(aider); !strings.Contains(got, "uv tool install") {
		t.Errorf("officialInstallCommand = %q, want the uv form", got)
	}
	// An Agent the manifest gives no package is reported rather than guessed at.
	if got := officialInstallCommand(catalog.Agent{}); !strings.Contains(got, "Unsupported") {
		t.Errorf("officialInstallCommand = %q, want it to say the manager is unsupported", got)
	}
	if got := officialInstallCommand(catalog.Agent{
		Package: &catalog.Package{Manager: "brew", Name: "x", Version: "1"},
	}); !strings.Contains(got, "brew") {
		t.Errorf("officialInstallCommand = %q, want it to name the unsupported manager", got)
	}
}

func TestAnUnknownModelIsNamedRatherThanBlamedOnTheProtocol(t *testing.T) {
	// Endpoints refuse an unknown model with the same shapes they use for an
	// unsupported protocol, so the bare verdict sends the user hunting a protocol
	// mismatch. Discovery listing other models is what makes the real cause
	// knowable.
	service := serviceFor(t, t.TempDir(), true, &routedTransport{
		probeStatus: 404,
		modelsBody:  `{"data":[{"id":"real-a"},{"id":"real-b"}]}`,
	})
	options := baseOptions()
	options.SkipTest = false
	options.Model = "typo-model"

	result, err := service.Install(options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Probe == nil {
		t.Fatal("no probe verdict came back")
	}
	if result.Probe.ErrorCode != "MODELS_UNSUPPORTED" {
		t.Errorf("error_code = %q, want MODELS_UNSUPPORTED", result.Probe.ErrorCode)
	}
	for _, fragment := range []string{"typo-model", "was not found", "real-a"} {
		if !strings.Contains(result.Probe.Message, fragment) {
			t.Errorf("message %q does not mention %q", result.Probe.Message, fragment)
		}
	}
}

func TestADiscoveryFailureLeavesTheProbeVerdictAlone(t *testing.T) {
	// Then "wrong model" and "unreachable endpoint" are indistinguishable, and
	// relabelling on a guess would mislead an offline user.
	service := serviceFor(t, t.TempDir(), true, &routedTransport{
		probeStatus: 404, modelsStatus: 500, modelsBody: "boom",
	})
	options := baseOptions()
	options.SkipTest = false

	result, err := service.Install(options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Probe == nil {
		t.Fatal("no probe verdict came back")
	}
	if result.Probe.ErrorCode == "MODELS_UNSUPPORTED" {
		t.Error("the verdict was relabelled on a failed discovery")
	}
}

func TestAListingThatContainsTheModelLeavesTheVerdictAlone(t *testing.T) {
	service := serviceFor(t, t.TempDir(), true, &routedTransport{
		probeStatus: 404, modelsBody: `{"data":[{"id":"m"}]}`,
	})
	options := baseOptions()
	options.SkipTest = false
	options.Model = "m"

	result, err := service.Install(options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Probe == nil {
		t.Fatal("no probe verdict came back")
	}
	if result.Probe.ErrorCode == "MODELS_UNSUPPORTED" {
		t.Error("the model was listed, so the failure is not an unknown model")
	}
}

func TestTheNextStepTellsTheUserToSourceTheCredentialFile(t *testing.T) {
	// Naming the bare command for an Agent whose credential lives in a file is how
	// a user ends up starting it unauthenticated.
	manifest := catalog.MustLoad()
	for _, osID := range []string{"linux", "windows"} {
		rt := runtime.New(runtime.WithHome(t.TempDir()), runtime.WithOSID(osID))
		for _, id := range manifest.AutoAgents() {
			agent, _ := manifest.Agent(id)
			step := nextStep(rt, agent, id, "some-model")
			if step == "" {
				t.Errorf("%s/%s: no next step", osID, id)
				continue
			}
			if !strings.Contains(step, agent.Command) {
				t.Errorf("%s/%s: %q does not name the command", osID, id, step)
			}
			if config.NeedsEnvFile(agent) || agent.ConfigAdapter == config.AdapterAider {
				sources := strings.Contains(step, "source ") || strings.Contains(step, ". \"$HOME")
				if !sources {
					t.Errorf("%s/%s: %q does not source the credential file", osID, id, step)
				}
			}
			// The restart hint has the same requirement, for the same reason.
			hint := restartHint(agent, id)
			if config.NeedsEnvFile(agent) && !strings.Contains(hint, "sources") {
				t.Errorf("%s/%s: restart hint %q does not mention sourcing", osID, id, hint)
			}
		}
	}
	// A guide-only Agent has no command to start.
	guide, _ := manifest.Agent("gemini-cli")
	rt := runtime.New(runtime.WithHome(t.TempDir()), runtime.WithOSID("linux"))
	if step := nextStep(rt, guide, "gemini-cli", "m"); step != "" {
		t.Errorf("a guide-only Agent produced a next step: %q", step)
	}
	if hint := restartHint(catalog.Agent{}, "mystery"); !strings.Contains(hint, "mystery") {
		t.Errorf("restartHint = %q, want it to name the Agent", hint)
	}
}

func TestGuideTextSurvivesAManifestThatCarriesARicherShape(t *testing.T) {
	// The field is typed as any so a future manifest cannot silently read as empty.
	if got := guideText(catalog.Agent{Guide: "plain text"}); got != "plain text" {
		t.Errorf("guideText = %q", got)
	}
	if got := guideText(catalog.Agent{}); got != "" {
		t.Errorf("guideText = %q, want empty for an absent guide", got)
	}
	if got := guideText(catalog.Agent{Guide: []any{"a", "b"}}); got == "" {
		t.Error("a non-string guide read as empty")
	}
}

func TestNewServiceAssemblesEverythingTheOperationNeeds(t *testing.T) {
	rt := runtime.New(runtime.WithHome(t.TempDir()), runtime.WithOSID("linux"))
	service := NewService(rt, 30*time.Second)
	if service.Writer == nil || service.Store == nil || service.Probes == nil {
		t.Fatal("NewService left a collaborator nil, which would panic on first use")
	}
	if service.Runtime != rt {
		t.Error("NewService did not keep the runtime it was given")
	}
}

// routedTransport answers the probe and the model listing differently, which is
// what the model diagnosis needs: the probe refuses while discovery succeeds.
type routedTransport struct {
	probeStatus  int
	probeBody    string
	modelsStatus int
	modelsBody   string
}

func (r *routedTransport) Do(request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/models") {
		status := r.modelsStatus
		if status == 0 {
			status = 200
		}
		return cannedTransport{status: status, body: r.modelsBody}.Do(request)
	}
	status := r.probeStatus
	if status == 0 {
		status = 200
	}
	return cannedTransport{status: status, body: r.probeBody}.Do(request)
}

func keysOf(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}
