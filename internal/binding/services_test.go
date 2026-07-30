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
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/app"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
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
		{&ProviderService{}, []string{"ListModels", "ListProviders", "OpenRegistration", "Probe"}},
		{&AgentService{}, []string{"Activate", "Install"}},
		{&ProfileService{}, []string{"ListProfiles", "SaveProfile"}},
	}
	for _, test := range tests {
		typeOf := reflect.TypeOf(test.service)
		got := make([]string, 0, typeOf.NumMethod())
		for index := 0; index < typeOf.NumMethod(); index++ {
			got = append(got, typeOf.Method(index).Name)
		}
		sort.Strings(got)
		sort.Strings(test.want)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s methods = %v, want %v", typeOf, got, test.want)
		}
	}
}

func TestOpenRegistrationUsesCatalogURLOnly(t *testing.T) {
	var opened string
	service := &ProviderService{opener: func(value string) error {
		opened = value
		return nil
	}}
	response, err := service.OpenRegistration(context.Background(), OpenRegistrationRequest{Provider: "ppio"})
	if err != nil {
		t.Fatal(err)
	}
	if opened != "https://ppio.com/" || response.URL != opened {
		t.Fatalf("unexpected registration URL: opened=%q response=%#v", opened, response)
	}

	_, err = service.OpenRegistration(context.Background(), OpenRegistrationRequest{Provider: "https://example.com"})
	if err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("arbitrary URL was not rejected: %v", err)
	}
}

func TestServiceCancellationUsesStableTimeoutCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&ProviderService{}).ListProviders(ctx)
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
	if err := os.WriteFile(filepath.Join(profilesDir, "team.json"), []byte(`{"schema_version":2,"id":"team","label":"Team","provider":"ppio","base_url":null,"model":"model","config_mode":"provider","agent_ids":["codex"]}`), 0o600); err != nil {
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
	if err != nil || len(profiles) != 1 || profiles[0].ID != "team" || !profiles[0].HasKey {
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
		AgentIDs:   []string{"codex"},
	})
	if err != nil || summary.ID != "team" || !summary.HasKey {
		t.Fatalf("saved profile = %#v, err=%v", summary, err)
	}
	wire, err := json.Marshal(summary)
	if err != nil || strings.Contains(string(wire), "sk-secret") || strings.Contains(string(wire), "api_key") {
		t.Fatalf("binding response leaked secret data: %s (%v)", wire, err)
	}
}

func TestAgentServiceActivatesThroughGoUseCase(t *testing.T) {
	home := t.TempDir()
	core := app.NewUseCases(app.StatusOptions{
		Home:     home,
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	service := NewAgentService(core)
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
	service := NewAgentService(core)
	response, err := service.Install(context.Background(), InstallRequest{
		Agents:        []string{"codex"},
		ProfileAgents: []string{"codex"},
		Provider:      "ppio",
		APIKey:        "binding-install-secret",
		Model:         "model-a",
		Configure:     true,
		SkipTest:      true,
		Timeout:       30,
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
		t.Fatalf("install binding response lost Python result fields: %s", wire)
	}
	if _, err := os.Stat(filepath.Join(home, ".oneagent", "profile.json")); err != nil {
		t.Fatalf("Go install did not publish profile: %v", err)
	}
}

func TestProviderServiceAggregatesSelectedAgentProtocols(t *testing.T) {
	seen := make([]string, 0)
	client := provider.NewClient(providerFakeDoer(func(request *http.Request) (*http.Response, error) {
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
