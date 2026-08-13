package app

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
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

// The Provider editor tests what is on screen, which is not on disk yet, so the
// probe has to accept an ID it cannot look up. Resolve refuses one -- that is the
// right behaviour for the wizard and is asserted separately below.
func TestProbeProviderTestsADraftProviderNotYetSaved(t *testing.T) {
	var seen []string
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		seen = append(seen, request.URL.String()+" auth="+request.Header.Get("Authorization"))
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	result, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider: "not-saved-yet", APIBaseURL: "https://api.draft.test/openai",
		APIKey: "typed-key", Model: "model-a", AgentIDs: []string{"opencode"}, Draft: true,
	})
	if err != nil || !result.Primary.OK {
		t.Fatalf("draft probe = %#v, err=%v", result, err)
	}
	if len(seen) != 1 || !strings.Contains(seen[0], "api.draft.test") || !strings.Contains(seen[0], "Bearer typed-key") {
		t.Fatalf("draft probe requests = %v", seen)
	}
}

// Each editor field is probed against what the user typed in it. Resolve clears
// AnthropicBaseURL whenever an OpenAI base is supplied, which would send the
// Anthropic probe to the OpenAI host and report a verdict about the wrong
// endpoint.
func TestProbeProviderTestsBothDraftEndpointsIndependently(t *testing.T) {
	seen := map[string]string{}
	var seenMu sync.Mutex
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		seenMu.Lock()
		defer seenMu.Unlock()
		seen[request.URL.Path] = request.URL.Host
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	result, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider:         "not-saved-yet",
		APIBaseURL:       "https://openai.draft.test/v1",
		AnthropicBaseURL: "https://anthropic.draft.test",
		APIKey:           "typed-key",
		Model:            "model-a",
		AgentIDs:         []string{"opencode", "claude-code"},
		Draft:            true,
	})
	if err != nil || !result.Primary.OK || len(result.Protocols) != 2 {
		t.Fatalf("two-endpoint draft probe = %#v, err=%v", result, err)
	}
	if seen["/v1/chat/completions"] != "openai.draft.test" {
		t.Fatalf("OpenAI probe went to %q", seen["/v1/chat/completions"])
	}
	if seen["/v1/messages"] != "anthropic.draft.test" {
		t.Fatalf("Anthropic probe went to %q, want anthropic.draft.test", seen["/v1/messages"])
	}
}

// A draft with no endpoints at all is the built-in Provider case: only the key is
// being tested, so the catalog endpoints still apply.
func TestProbeProviderDraftWithoutEndpointsUsesTheStoredProvider(t *testing.T) {
	var host string
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		host = request.URL.Host
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	if _, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider: "ppio", APIKey: "typed-key", Model: "model-a", AgentIDs: []string{"opencode"}, Draft: true,
	}); err != nil {
		t.Fatal(err)
	}
	if host != "api.ppio.com" {
		t.Fatalf("keyless-endpoint draft probe went to %q", host)
	}
}

// The guard on the change above: without Draft, an unknown ID must still fail
// rather than silently probing whatever base URL came with the request. A typo'd
// Provider ID reported as a connection result would be a wrong verdict.
func TestProbeProviderStillRefusesAnUnknownProviderWithoutDraft(t *testing.T) {
	core := providerUseCases(t, appProviderDoer(func(*http.Request) (*http.Response, error) {
		t.Fatal("an unknown Provider was probed")
		return nil, nil
	}))
	_, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider: "not-saved-yet", APIBaseURL: "https://api.draft.test/openai",
		APIKey: "typed-key", Model: "model-a", AgentIDs: []string{"opencode"},
	})
	if err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("unknown Provider without Draft = %v", err)
	}
}

func TestProbeProviderAggregatesAgentProtocols(t *testing.T) {
	seen := make([]string, 0)
	var seenMu sync.Mutex
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		seenMu.Lock()
		defer seenMu.Unlock()
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
	}, true, false); err != nil {
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
	}, false, false)
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
	result, err := core.SaveProvider(context.Background(), entry, false, false)
	if err != nil || len(result.Reapplied) != 0 || len(result.Failures) != 0 {
		t.Fatalf("metadata-only save reapplied: %#v, err=%v", result, err)
	}
}

func TestDeleteProviderRejectsBoundAgents(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "acme", Name: "Acme", BaseURL: "https://api.acme.test", APIKey: "key",
	}, true, false); err != nil {
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

// Switching a Profile's Provider used to leave every Agent following it bound to
// the old one: the Profile page showed the new Provider while the Agent kept
// sending traffic to the old endpoint, and neither Provider card listed the Agent
// correctly. Asserting the binding, not just that SaveProfile returns no error --
// the pre-fix code passed that.
func TestSaveProfileMigratesBoundAgentsToTheNewProvider(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	// novita is a built-in Provider, so it only needs a key to be usable as a
	// switch target.
	if err := core.providers.SaveKey(context.Background(), "novita", "novita-key"); err != nil {
		t.Fatal(err)
	}
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
	result, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Provider: "novita", Model: "model-b", ConfigMode: "provider",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 0 || !slices.Contains(result.Reapplied, "codex") {
		t.Fatalf("reapply result = %#v", result)
	}
	binding, err := core.profiles.ReadAgentBinding("codex")
	if err != nil || binding == nil {
		t.Fatalf("binding read failed: %v", err)
	}
	if binding.Provider != "novita" || binding.Model != "model-b" {
		t.Fatalf("binding did not follow the Profile: %#v", binding)
	}
	if binding.ProfileRef != "team" {
		t.Fatalf("binding lost its Profile reference: %#v", binding)
	}
	// The Agent's own config has to move too, or the UI reports the new Provider
	// while the Agent still talks to the old endpoint.
	written, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "https://api.novita.ai/openai") {
		t.Fatalf("Agent config kept the old endpoint: %s", written)
	}
}

// A label-only edit must not churn Agent configs: rewriting them would restart
// the Agent's file for no reason and report a reapply the user did not ask for.
func TestSaveProfileLeavesBindingsAloneWhenRoutingIsUnchanged(t *testing.T) {
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
	result, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Label: "Renamed", Provider: "ppio", Model: "model-a", ConfigMode: "provider",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reapplied) != 0 || len(result.Failures) != 0 {
		t.Fatalf("a label edit reapplied Agents: %#v", result)
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
	profiles, listErr := core.profiles.List()
	if listErr != nil || len(profiles) != 1 {
		t.Fatal("in-use Profile was deleted")
	}
}

func TestDeleteProfileStopsOnCorruptAgentBinding(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "team", Provider: "ppio", Model: "model-a", ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	bindingPath, _ := core.profiles.AgentBindingPath("codex")
	if err := os.MkdirAll(filepath.Dir(bindingPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bindingPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := core.DeleteProfile(context.Background(), "team"); err == nil {
		t.Fatal("corrupt Agent binding did not stop Profile deletion")
	}
	profilePath, _ := core.profiles.ProfilePath("team")
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("Profile was deleted: %v", err)
	}
}

func TestDeleteIgnoresStaleBindingWithoutAgentConfig(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "acme", Name: "Acme", BaseURL: "https://api.acme.test", APIKey: "key",
	}, true, false); err != nil {
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

// A built-in Provider's catalogue is mostly not chat models. With an empty probe
// model the live list used to win outright, so a video generator returned first
// became the model the connection test sent a chat payload to -- and the failure
// read as "your key is broken" during first-run setup.
func TestProbeProviderPrefersTheManifestModelOverAVideoModel(t *testing.T) {
	var probed string
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/openai/v1/models" {
			// Ordered as an aggregator really does: generators first.
			return appProviderResponse(http.StatusOK, `{"data":[
				{"id":"wan-ai/wan2.1-t2v-14b"},
				{"id":"kwai/kling-v1-video"},
				{"id":"deepseek/deepseek-v4-flash"},
				{"id":"deepseek/deepseek-v4-pro"}
			]}`), nil
		}
		body, _ := io.ReadAll(request.Body)
		probed = string(body)
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	result, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider: "ppio", APIKey: "key", AgentIDs: []string{"opencode"},
	})
	if err != nil || !result.Primary.OK {
		t.Fatalf("probe = %#v, err=%v", result, err)
	}
	// The manifest's reviewed chat model, not the first survivor of the denylist.
	if !strings.Contains(probed, "deepseek/deepseek-v4-flash") {
		t.Fatalf("probed payload did not use the manifest model: %s", probed)
	}
}

// A model the user typed is their override and must be probed verbatim, even when
// it is one the denylist would reject: a failure they asked for is information.
func TestProbeProviderKeepsAModelTheUserTyped(t *testing.T) {
	var probed string
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/openai/v1/models" {
			t.Fatal("discovery ran even though the user named a model")
		}
		body, _ := io.ReadAll(request.Body)
		probed = string(body)
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	if _, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider: "ppio", APIKey: "key", Model: "kwai/kling-v1-video", AgentIDs: []string{"opencode"},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(probed, "kwai/kling-v1-video") {
		t.Fatalf("user's model was replaced: %s", probed)
	}
}

// Store.Save writes APIKey unconditionally, so an entry with no key blanks
// whatever was on disk. That is right for the Provider editor, where an empty
// field means the user cleared it, and destructive for a settings import of a
// file exported without keys -- which is now the default export.
func TestSaveProviderKeepsStoredKeyOnlyWhenAsked(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	original := provider.Entry{ID: "acme", Name: "Acme", BaseURL: "https://api.acme.test", APIKey: "sk-original"}
	if _, err := core.SaveProvider(context.Background(), original, true, false); err != nil {
		t.Fatal(err)
	}

	// An import restoring a key-less file must leave the credential alone.
	keyless := original
	keyless.APIKey = ""
	saved, err := core.SaveProvider(context.Background(), keyless, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Entry.APIKey != "sk-original" {
		t.Fatalf("keepExistingKey dropped the stored key: %q", saved.Entry.APIKey)
	}
	stored, err := core.GetProvider(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if stored.APIKey != "sk-original" {
		t.Fatalf("stored key after a key-less import = %q", stored.APIKey)
	}

	// The editor's empty field still means "clear it".
	if _, err := core.SaveProvider(context.Background(), keyless, false, false); err != nil {
		t.Fatal(err)
	}
	cleared, err := core.GetProvider(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.APIKey != "" {
		t.Fatalf("an emptied editor field should clear the key, got %q", cleared.APIKey)
	}
}

// A file that does carry a key must still replace the stored one, or importing a
// backup would silently keep a credential the user meant to overwrite.
func TestSaveProviderReplacesKeyWhenSupplied(t *testing.T) {
	home := t.TempDir()
	core := activationCore(t, home, provider.NewClient(nil), "linux")
	entry := provider.Entry{ID: "acme", Name: "Acme", BaseURL: "https://api.acme.test", APIKey: "sk-original"}
	if _, err := core.SaveProvider(context.Background(), entry, true, false); err != nil {
		t.Fatal(err)
	}
	entry.APIKey = "sk-incoming"
	if _, err := core.SaveProvider(context.Background(), entry, false, true); err != nil {
		t.Fatal(err)
	}
	stored, err := core.GetProvider(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if stored.APIKey != "sk-incoming" {
		t.Fatalf("a supplied key must win even with keepExistingKey, got %q", stored.APIKey)
	}
}
