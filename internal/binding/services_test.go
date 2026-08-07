package binding

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/install"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

type providerFakeDoer func(*http.Request) (*http.Response, error)

func (fake providerFakeDoer) Do(request *http.Request) (*http.Response, error) {
	return fake(request)
}

func providerResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func providerCore(t *testing.T, client *provider.Client) *app.UseCases {
	t.Helper()
	return app.NewUseCasesWithProviderClient(app.StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	}, client)
}

func TestServiceMethodAllowlist(t *testing.T) {
	tests := []struct {
		service any
		want    []string
	}{
		{&StatusService{}, []string{"GetStatus"}},
		{&ProviderService{}, []string{"DeleteProvider", "GetProvider", "ListModels", "OpenRegistration", "Probe", "SaveProvider"}},
		{&AgentService{}, []string{"Activate", "Install", "Launch", "Update"}},
		{&ProfileService{}, []string{"DeleteProfile", "ListProfiles", "SaveProfile"}},
		{&RuntimeService{}, []string{"GetSettings", "InstallRuntime", "ListRuntimes", "SaveSettings"}},
		{&DesktopAgentService{}, []string{"Configure", "GetStatus", "Install", "Open"}},
		{&TransferService{}, []string{"Read", "Write"}},
		{&UpdateService{}, []string{"Check", "DownloadAndInstall", "Restart"}},
	}
	for _, test := range tests {
		typeOf := reflect.TypeOf(test.service)
		got := make([]string, 0, typeOf.NumMethod())
		for method := range typeOf.Methods() {
			got = append(got, method.Name)
		}
		sort.Strings(got)
		sort.Strings(test.want)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s methods = %v, want %v", typeOf, got, test.want)
		}
	}
}

func TestStatusServiceRunsNativeSmokeHookAfterSuccess(t *testing.T) {
	called := 0
	service := NewServicesWithOptions(app.NewUseCases(app.StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	}), nil, ServicesOptions{AfterGetStatus: func() { called++ }}).Status
	if _, err := service.GetStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("native smoke hook calls = %d, want 1", called)
	}

	if _, err := (&StatusService{afterGetStatus: func() { called++ }}).GetStatus(context.Background()); err == nil {
		t.Fatal("unconfigured status service succeeded")
	}
	if called != 1 {
		t.Fatalf("native smoke hook ran after failed status = %d", called)
	}
}

func TestOpenRegistrationUsesConfiguredProviderURL(t *testing.T) {
	var opened string
	service := NewProviderService(providerCore(t, nil), func(value string) error {
		opened = value
		return nil
	})
	response, err := service.OpenRegistration(context.Background(), OpenRegistrationRequest{Provider: "ppio"})
	if err != nil {
		t.Fatal(err)
	}
	// The key page, not Home: a user pressing this button wants a key, and
	// landing them on the marketing site leaves them to find it themselves.
	want := catalog.KeyManagementURL("ppio")
	if want == "" {
		t.Fatal("ppio has no key management URL to open")
	}
	if opened != want || response.URL != opened {
		t.Fatalf("unexpected registration URL: opened=%q want=%q response=%#v", opened, want, response)
	}

	_, err = service.OpenRegistration(context.Background(), OpenRegistrationRequest{Provider: "https://example.com"})
	if err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("arbitrary URL was not rejected: %v", err)
	}
}

// A user-added Provider has no key page, only whatever Home they typed. The
// button has to keep working for them, so absence falls back rather than
// disabling the affordance.
func TestOpenRegistrationFallsBackToHomeWithoutAKeyPage(t *testing.T) {
	home := t.TempDir()
	core := app.NewUseCases(app.StatusOptions{
		Home: home, Platform: platform.For("linux", "amd64"),
		Lookup: func(string) (string, bool) { return "", false },
	})
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "acme", Name: "Acme", Home: "https://acme.example.com/", BaseURL: "https://api.acme.example.com/openai",
	}); err != nil {
		t.Fatal(err)
	}
	var opened string
	service := NewProviderService(core, func(value string) error {
		opened = value
		return nil
	})
	response, err := service.OpenRegistration(context.Background(), OpenRegistrationRequest{Provider: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if opened != "https://acme.example.com/" || response.URL != opened {
		t.Fatalf("custom Provider did not fall back to its home URL: opened=%q response=%#v", opened, response)
	}
}

func TestProviderServicePersistsCRUDAndReturnsKeyOnlyOnExplicitRead(t *testing.T) {
	home := t.TempDir()
	core := app.NewUseCases(app.StatusOptions{
		Home: home, Platform: platform.For("linux", "amd64"),
		Lookup: func(string) (string, bool) { return "", false },
	})
	service := NewProviderService(core, nil)
	request := SaveProviderRequest{
		ID: "acme", Name: "Acme", Home: "https://acme.test/",
		BaseURL: "https://api.acme.test/openai", APIKey: "sk-provider",
	}
	if saved, err := service.SaveProvider(context.Background(), request); err != nil || saved.Entry.ID != "acme" {
		t.Fatalf("saved Provider = %#v, err=%v", saved, err)
	}
	entry, err := service.GetProvider(context.Background(), ProviderIDRequest{ID: "acme"})
	if err != nil || entry.APIKey != "sk-provider" {
		t.Fatalf("read Provider = %#v, err=%v", entry, err)
	}
	status, err := core.GetStatus(context.Background())
	wire, marshalErr := json.Marshal(status)
	if err != nil || marshalErr != nil || strings.Contains(string(wire), "sk-provider") || !status.Providers["acme"].HasKey {
		t.Fatalf("Provider status leaked key: %s, err=%v/%v", wire, err, marshalErr)
	}
	deleted, err := service.DeleteProvider(context.Background(), ProviderIDRequest{ID: "acme"})
	if err != nil || !deleted.OK {
		t.Fatalf("delete Provider = %#v, err=%v", deleted, err)
	}
	if _, err := service.GetProvider(context.Background(), ProviderIDRequest{ID: "acme"}); err == nil {
		t.Fatal("deleted Provider was still available")
	}
}

func TestServiceCancellationUsesStableTimeoutCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&ProviderService{}).Probe(ctx, ProbeRequest{})
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestProfileServiceListsPublicSummaries(t *testing.T) {
	home := t.TempDir()
	profilesDir := filepath.Join(home, ".oneagent", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "team.json"), []byte(`{"schema_version":2,"id":"team","label":"Team","provider":"ppio","model":"model","config_mode":"provider","agent_ids":["codex"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(home, ".oneagent", "secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, "team.env"), []byte("export ONEAGENT_API_KEY=sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core := app.NewUseCases(app.StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	service := NewProfileService(core)
	profiles, err := service.ListProfiles(context.Background())
	if err != nil || len(profiles) != 1 || profiles[0].ID != "team" {
		t.Fatalf("profiles = %#v, err=%v", profiles, err)
	}
	wire, err := json.Marshal(profiles)
	if err != nil || strings.Contains(string(wire), "sk-secret") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("profile listing leaked secret data: %s (%v)", wire, err)
	}
}

func TestProfileServiceSavesWithoutReturningSecret(t *testing.T) {
	core := app.NewUseCases(app.StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	service := NewProfileService(core)
	summary, err := service.SaveProfile(context.Background(), SaveProfileRequest{
		ID:         "team",
		Label:      "Team",
		Provider:   "ppio",
		Model:      "model-a",
		APIKey:     "sk-secret",
		ConfigMode: "provider",
	})
	if err != nil || summary.ID != "team" {
		t.Fatalf("saved profile = %#v, err=%v", summary, err)
	}
	wire, err := json.Marshal(summary)
	if err != nil || strings.Contains(string(wire), "sk-secret") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("binding response leaked secret data: %s (%v)", wire, err)
	}
}

func TestProfileServiceDeletesProfile(t *testing.T) {
	core := app.NewUseCases(app.StatusOptions{
		Home: t.TempDir(), Platform: platform.For("linux", "amd64"),
		Lookup: func(string) (string, bool) { return "", false },
	})
	service := NewProfileService(core)
	if _, err := service.SaveProfile(context.Background(), SaveProfileRequest{
		ID: "team", Provider: "ppio", Model: "model-a", ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.DeleteProfile(context.Background(), ProviderIDRequest{ID: "team"})
	if err != nil || !deleted.OK {
		t.Fatalf("delete Profile = %#v, err=%v", deleted, err)
	}
	profiles, err := service.ListProfiles(context.Background())
	if err != nil || len(profiles) != 0 {
		t.Fatalf("profiles after delete = %#v, err=%v", profiles, err)
	}
}

func TestAgentServiceActivatesThroughGoUseCase(t *testing.T) {
	home := t.TempDir()
	core := app.NewUseCases(app.StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	service := &AgentService{core: core}
	response, err := service.Activate(context.Background(), ActivateRequest{
		AgentID:  "codex",
		Provider: "ppio",
		APIKey:   "binding-secret",
		Model:    "model-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Agent != "codex" || response.Provider != "ppio" || response.Model != "model-a" {
		t.Fatalf("activation response = %#v", response)
	}
	wire, err := json.Marshal(response)
	if err != nil || strings.Contains(string(wire), "binding-secret") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("activation binding response leaked secret material: %s (%v)", wire, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatalf("Go activation did not write Codex config: %v", err)
	}
}

func TestAgentServiceInstallsThroughGoUseCase(t *testing.T) {
	home := t.TempDir()
	core := app.NewUseCases(app.StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	service := &AgentService{core: core}
	response, err := service.Install(context.Background(), InstallRequest{
		Agents:    []string{"codex"},
		Provider:  "ppio",
		APIKey:    "binding-install-secret",
		Model:     "model-a",
		Configure: true,
		SkipTest:  true,
		Timeout:   30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || len(response.Results) != 1 || response.Results[0].Status != "configured" {
		t.Fatalf("install response = %#v", response)
	}
	wire, err := json.Marshal(response)
	if err != nil || strings.Contains(string(wire), "binding-install-secret") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("install binding response leaked secret material: %s (%v)", wire, err)
	}
	if !strings.Contains(string(wire), `"installed":false`) || !strings.Contains(string(wire), `"version":`) {
		t.Fatalf("install binding response lost established result fields: %s", wire)
	}
	if _, err := os.Stat(filepath.Join(home, ".oneagent", "profile.json")); err != nil {
		t.Fatalf("Go install did not publish profile: %v", err)
	}
}

// Timeout 0 has to mean "use the Go default", because that is what the frontend
// now sends. Previously the frontend sent a hardcoded 180 and this branch was
// never exercised, so the two sides could disagree without any test noticing.
func TestAgentServiceTreatsZeroTimeoutAsTheGoDefault(t *testing.T) {
	home := t.TempDir()
	core := app.NewUseCases(app.StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	service := &AgentService{core: core}
	response, err := service.Install(context.Background(), InstallRequest{
		Agents: []string{"codex"}, Provider: "ppio", APIKey: "secret", Model: "model-a",
		Configure: true, SkipTest: true, Timeout: 0,
	})
	if err != nil {
		t.Fatalf("zero timeout was rejected instead of taking the default: %v", err)
	}
	if !response.OK {
		t.Fatalf("zero timeout install response = %#v", response)
	}
}

// The ceiling must exceed the default, or a caller could not request the default
// value explicitly. Both rejections return InvalidRequest rather than silently
// clamping, because a clamped timeout would be a surprise, not a fix.
func TestAgentServiceRejectsOutOfRangeTimeouts(t *testing.T) {
	service := &AgentService{core: app.NewUseCases(app.StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})}
	for _, timeout := range []int{-1, maxInstallTimeoutSeconds + 1} {
		_, err := service.Install(context.Background(), InstallRequest{
			Agents: []string{"codex"}, Provider: "ppio", APIKey: "secret", Model: "m", Timeout: timeout,
		})
		if err == nil {
			t.Fatalf("timeout %d was accepted", timeout)
		}
	}
	if int(install.DefaultCommandTimeout/time.Second) > maxInstallTimeoutSeconds {
		t.Fatalf("the ceiling %d is below the default %v, so the default cannot be requested explicitly",
			maxInstallTimeoutSeconds, install.DefaultCommandTimeout)
	}
}

func TestInstallResultBindingPreservesFieldPresence(t *testing.T) {
	tests := []struct {
		name string
		item app.AgentInstallResult
		want string
	}{
		{
			name: "automatic",
			item: app.AgentInstallResult{
				Agent: "codex", Status: "configured", Config: "/tmp/config.json",
				Installed: false, LockedVersion: "0.145.0",
			},
			want: `{"agent":"codex","status":"configured","config":"/tmp/config.json","installed":false,"version":null,"lockedVersion":"0.145.0","retryable":false}`,
		},
		{
			name: "guide-only",
			item: app.AgentInstallResult{Agent: "gemini-cli", Status: "guide-only", Message: "use login"},
			want: `{"agent":"gemini-cli","status":"guide-only","message":"use login","retryable":false}`,
		},
		{
			name: "failed",
			item: app.AgentInstallResult{
				Agent: "codex", Status: "failed", Code: 7, ErrorCode: "AGENT_INSTALL_FAILED",
				Message: "npm missing", Retryable: true,
			},
			want: `{"agent":"codex","status":"failed","code":7,"error_code":"AGENT_INSTALL_FAILED","message":"npm missing","retryable":true}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire, err := json.Marshal(installResult(test.item))
			if err != nil {
				t.Fatal(err)
			}
			if got := string(wire); got != test.want {
				t.Fatalf("wire = %s, want %s", got, test.want)
			}
		})
	}
}

func TestProviderServiceAggregatesSelectedAgentProtocols(t *testing.T) {
	seen := make([]string, 0)
	var seenMu sync.Mutex
	client := provider.NewClient(providerFakeDoer(func(request *http.Request) (*http.Response, error) {
		seenMu.Lock()
		defer seenMu.Unlock()
		seen = append(seen, request.URL.Path)
		return providerResponse(http.StatusNoContent, ""), nil
	}))
	service := NewProviderService(providerCore(t, client), nil)
	result, err := service.Probe(context.Background(), ProbeRequest{
		Provider:   "custom",
		APIBaseURL: "https://proxy.test/v1",
		APIKey:     "key",
		Model:      "model",
		Agents:     []string{"codex", "claude-code", "opencode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || len(result.Protocols) != 3 || result.ErrorCode != nil {
		t.Fatalf("aggregated probe = %#v", result)
	}
	sort.Strings(seen)
	if !reflect.DeepEqual(seen, []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"}) {
		t.Fatalf("probe paths = %v", seen)
	}
	if result.Protocol == nil || *result.Protocol != "anthropic" {
		t.Fatalf("primary protocol = %v", result.Protocol)
	}
	wire, err := json.Marshal(result)
	if err != nil || strings.Contains(string(wire), "key") {
		t.Fatalf("probe response leaked input: %s (%v)", wire, err)
	}
	if strings.Count(string(wire), `"protocols"`) != 1 {
		t.Fatalf("nested probe results must omit empty protocol maps: %s", wire)
	}
}

func TestProviderServiceUsesStableFailureAsPrimary(t *testing.T) {
	client := provider.NewClient(providerFakeDoer(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v1/responses" {
			return providerResponse(http.StatusBadRequest, `{"message":"does not support endpoint"}`), nil
		}
		return providerResponse(http.StatusNoContent, ""), nil
	}))
	service := NewProviderService(providerCore(t, client), nil)
	result, err := service.Probe(context.Background(), ProbeRequest{
		Provider: "custom", APIBaseURL: "https://proxy.test/v1", APIKey: "key", Model: "model", Agents: []string{"codex", "opencode"},
	})
	if err != nil || result.OK || result.ErrorCode == nil || *result.ErrorCode != oneerrors.ProtocolUnsupported {
		t.Fatalf("failure probe = %#v, err=%v", result, err)
	}
	if len(result.Protocols) != 2 || result.Protocols["responses"].OK {
		t.Fatalf("protocol details = %#v", result.Protocols)
	}
}

func TestProviderServiceListsModels(t *testing.T) {
	client := provider.NewClient(providerFakeDoer(func(*http.Request) (*http.Response, error) {
		return providerResponse(http.StatusOK, `{"data":[{"id":"chat-model"}]}`), nil
	}))
	service := NewProviderService(providerCore(t, client), nil)
	result, err := service.ListModels(context.Background(), ModelsRequest{Provider: "ppio", APIKey: "key"})
	if err != nil || !result.OK || !reflect.DeepEqual(result.Models, []string{"chat-model"}) {
		t.Fatalf("models = %#v, err=%v", result, err)
	}
	wire, err := json.Marshal(result)
	if err != nil || strings.Contains(string(wire), `"protocol"`) || !strings.Contains(string(wire), `"error_code":null`) {
		t.Fatalf("model response null/omitted fields diverged: %s (%v)", wire, err)
	}
}
