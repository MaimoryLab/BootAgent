package app

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

type appProviderDoer func(*http.Request) (*http.Response, error)

func (doer appProviderDoer) Do(request *http.Request) (*http.Response, error) {
	return doer(request)
}

func appProviderResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func providerUseCases(t *testing.T, doer provider.HTTPDoer) *UseCases {
	t.Helper()
	return NewUseCasesWithProviderClient(StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	}, provider.NewClient(doer))
}

func TestProbeProviderAggregatesAgentProtocols(t *testing.T) {
	seen := make([]string, 0)
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		seen = append(seen, request.URL.Path)
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	result, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider:   "custom",
		APIBaseURL: "https://proxy.test/v1",
		APIKey:     "key",
		Model:      "model",
		AgentIDs:   []string{"codex", "claude-code", "opencode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Primary.OK || len(result.Protocols) != 3 || result.Primary.Protocol == nil || *result.Primary.Protocol != provider.ProtocolAnthropic {
		t.Fatalf("aggregated probe = %#v", result)
	}
	sort.Strings(seen)
	if !reflect.DeepEqual(seen, []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"}) {
		t.Fatalf("probe paths = %v", seen)
	}
}

func TestProbeProviderSelectsFirstFailure(t *testing.T) {
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v1/responses" {
			return appProviderResponse(http.StatusBadRequest, `{"message":"does not support endpoint"}`), nil
		}
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	result, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider: "custom", APIBaseURL: "https://proxy.test/v1", APIKey: "key", Model: "model", AgentIDs: []string{"codex", "opencode"},
	})
	if err != nil || result.Primary.OK || result.Primary.ErrorCode == nil || *result.Primary.ErrorCode != oneerrors.ProtocolUnsupported {
		t.Fatalf("failure result = %#v, err=%v", result, err)
	}
}

func TestProviderUseCasesHonorCancellationBeforeNetwork(t *testing.T) {
	called := false
	core := providerUseCases(t, appProviderDoer(func(*http.Request) (*http.Response, error) {
		called = true
		return appProviderResponse(http.StatusOK, `{}`), nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := core.ListProviderModels(ctx, "ppio", "key", "")
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout || called {
		t.Fatalf("cancelled call = %v, called=%v", err, called)
	}
}

func TestSavedProviderDrivesStatusAndProbeWithoutResendingKey(t *testing.T) {
	home := t.TempDir()
	var request *http.Request
	client := provider.NewClient(appProviderDoer(func(value *http.Request) (*http.Response, error) {
		request = value
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	options := StatusOptions{
		Home: home, Platform: platform.For("linux", "amd64"),
		Lookup: func(string) (string, bool) { return "", false },
	}
	core := NewUseCasesWithProviderClient(options, client)
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "acme", Name: "Acme", BaseURL: "https://api.acme.test/openai", APIKey: "saved-key",
	}); err != nil {
		t.Fatal(err)
	}

	reloaded := NewUseCasesWithProviderClient(options, client)
	status, err := reloaded.GetStatus(context.Background())
	if err != nil || !status.Providers["acme"].Custom || !status.Providers["acme"].HasKey {
		t.Fatalf("saved Provider status = %#v, err=%v", status.Providers["acme"], err)
	}
	result, err := reloaded.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider: "acme", Model: "model-a",
	})
	if err != nil || !result.Primary.OK {
		t.Fatalf("saved Provider probe = %#v, err=%v", result, err)
	}
	if request == nil || request.URL.Host != "api.acme.test" || request.Header.Get("Authorization") != "Bearer saved-key" {
		t.Fatalf("saved Provider request = %#v", request)
	}
}

func TestSaveProviderReappliesEveryAgentBoundToIt(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	for _, agentID := range []string{"codex", "opencode"} {
		if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
			AgentID: agentID, Provider: "ppio", APIKey: "first-key", Model: "model-" + agentID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A second Provider must stay untouched: only Agents bound to the edited one
	// get rewritten.
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "claude-code", Provider: "novita", APIKey: "other-key", Model: "model-other",
	}); err != nil {
		t.Fatal(err)
	}
	otherConfig, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "ppio", Name: "PPIO", BaseURL: "https://relay.ppio.test/openai", APIKey: "rotated-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 0 || !reflect.DeepEqual(result.Reapplied, []string{"codex", "opencode"}) {
		t.Fatalf("reapply outcome = %#v", result)
	}
	// Each Agent keeps its own model while picking up the new endpoint and key.
	// Codex takes the key from auth.json and the endpoint from config.toml;
	// OpenCode carries both in its own config.
	credentialFiles := map[string][]string{
		"codex":    {filepath.Join(home, ".codex", "auth.json"), filepath.Join(home, ".codex", "config.toml")},
		"opencode": {filepath.Join(home, ".config", "opencode", "opencode.json")},
	}
	for _, agentID := range []string{"codex", "opencode"} {
		var applied strings.Builder
		for _, path := range credentialFiles[agentID] {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s config %s: %v", agentID, path, err)
			}
			applied.WriteString(string(data))
		}
		if !strings.Contains(applied.String(), "rotated-key") || !strings.Contains(applied.String(), "relay.ppio.test") {
			t.Fatalf("%s did not pick up the rotated Provider", agentID)
		}
		binding, err := core.profiles.ReadAgentBinding(agentID)
		if err != nil || binding == nil || binding.Model != "model-"+agentID || !strings.Contains(binding.BaseURL, "relay.ppio.test") {
			t.Fatalf("%s binding = %#v, err=%v", agentID, binding, err)
		}
	}
	if after, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); err != nil || string(after) != string(otherConfig) {
		t.Fatalf("Agent on another Provider was rewritten: %q, err=%v", after, err)
	}
}

func TestSaveProviderSkipsReapplyWhenOnlyMetadataChanges(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "codex", Provider: "ppio", APIKey: "key", Model: "model-a",
	}); err != nil {
		t.Fatal(err)
	}
	entry, err := core.GetProvider(context.Background(), "ppio")
	if err != nil {
		t.Fatal(err)
	}
	entry.Name = "PPIO Cloud"
	result, err := core.SaveProvider(context.Background(), entry)
	if err != nil || len(result.Reapplied) != 0 || len(result.Failures) != 0 {
		t.Fatalf("metadata-only save reapplied: %#v, err=%v", result, err)
	}
}

func TestDeleteProviderRejectsBoundAgents(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "acme", Name: "Acme", BaseURL: "https://api.acme.test", APIKey: "key",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "codex", Provider: "acme", Model: "model-a",
	}); err != nil {
		t.Fatal(err)
	}
	err := core.DeleteProvider(context.Background(), "acme")
	if err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("deleting in-use Provider returned %v", err)
	}
	if _, err := core.GetProvider(context.Background(), "acme"); err != nil {
		t.Fatalf("guard deleted Provider: %v", err)
	}
}

func TestDeleteProfileRejectsBoundAgents(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Provider: "ppio", Model: "model-a", ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: "codex", Provider: "ppio", APIKey: "key", Model: "model-a", ProfileID: "team",
	}); err != nil {
		t.Fatal(err)
	}
	err := core.DeleteProfile(context.Background(), "team")
	if err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("deleting in-use Profile returned %v", err)
	}
	if _, err := core.profiles.ProfilePath("team"); err != nil {
		t.Fatal(err)
	}
	if len(core.profiles.List()) != 1 {
		t.Fatal("in-use Profile was deleted")
	}
}

func TestDeleteIgnoresStaleBindingWithoutAgentConfig(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "acme", Name: "Acme", BaseURL: "https://api.acme.test", APIKey: "key",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "stale", Provider: "acme", Model: "model-a", ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.profiles.WriteAgentBinding(context.Background(), "workbuddy", profileStore.BindingWriteRequest{
		Provider: "acme", BaseURL: "https://api.acme.test", Model: "model-a", ProfileRef: "stale",
	}); err != nil {
		t.Fatal(err)
	}
	if err := core.DeleteProfile(context.Background(), "stale"); err != nil {
		t.Fatalf("stale binding blocked Profile deletion: %v", err)
	}
	if err := core.DeleteProvider(context.Background(), "acme"); err != nil {
		t.Fatalf("stale binding blocked Provider deletion: %v", err)
	}
}
