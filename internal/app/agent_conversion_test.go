package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// chatOnlyUpstream answers Chat Completions and refuses every other route, the
// shape of a Provider that compatibility mode exists for.
func chatOnlyUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func freeLoopbackListen(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := listener.Addr().String()
	_ = listener.Close()
	return listen
}

// chatProfileCore is a core with one Chat Completions Profile on a chat-only
// upstream, and the adapter set to listen on a free port.
func chatProfileCore(t *testing.T, upstream string) (*UseCases, string, string) {
	t.Helper()
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Lookup: func(string) (string, bool) { return "", false }})
	t.Cleanup(func() { _ = core.CloseConversion() })
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "chat-upstream", Name: "Chat upstream", BaseURL: upstream, APIKey: "upstream-secret",
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "chat-upstream", Label: "Chat upstream", Provider: "chat-upstream", Model: "model-a", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	listen := freeLoopbackListen(t)
	if _, err := core.SaveConversion(context.Background(), ConversionConfig{Listen: listen, TargetProfile: "chat-upstream"}); err != nil {
		t.Fatal(err)
	}
	return core, home, listen
}

func TestConfigureCodexWithConversionPointsItAtTheAdapter(t *testing.T) {
	upstream := chatOnlyUpstream(t)
	core, home, listen := chatProfileCore(t, upstream.URL)

	result, err := core.ConfigureAgentWithConversion(context.Background(), "codex", "chat-upstream")
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpstreamVerified || !result.EndToEndVerified || result.AgentID != "codex" {
		t.Fatalf("result = %#v", result)
	}
	config, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "http://"+listen+"/v1") {
		t.Fatalf("Codex config does not point at the adapter:\n%s", config)
	}
	if strings.Contains(string(config), upstream.URL) {
		t.Fatalf("Codex config points straight at the upstream:\n%s", config)
	}
	binding, err := core.profiles.ReadAgentBinding("codex")
	if err != nil || binding == nil || binding.ProfileRef != converterRouteID("codex") {
		t.Fatalf("Codex binding = %#v, err=%v", binding, err)
	}
	conversion, err := core.Conversion(context.Background())
	if err != nil || !conversion.Enabled || conversion.Routes["codex"] != "chat-upstream" {
		t.Fatalf("conversion = %#v, err=%v", conversion, err)
	}
}

// recordingUpstream is a chat-only upstream that remembers the key each
// request carried, so a test can tell which Provider a request reached.
func recordingUpstream(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	keys := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		keys = append(keys, r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	return server, &keys
}

// The single shared target this replaces sent every Agent's requests to the
// last model chosen: pointing Claude Code at a second service silently moved
// Codex there too.
func TestTwoAgentsUseTwoModelServicesThroughCompatibilityMode(t *testing.T) {
	first, firstKeys := recordingUpstream(t)
	second, secondKeys := recordingUpstream(t)
	core, home, listen := chatProfileCore(t, first.URL)
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "second-upstream", Name: "Second", BaseURL: second.URL, APIKey: "second-secret",
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "second-chat", Label: "Second", Provider: "second-upstream", Model: "model-b", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := core.ConfigureAgentWithConversion(context.Background(), "codex", "chat-upstream"); err != nil {
		t.Fatal(err)
	}
	codexConfig, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfigureAgentWithConversion(context.Background(), "claude-code", "second-chat"); err != nil {
		t.Fatal(err)
	}

	// Claude Code joining did not touch Codex: neither its file nor its route.
	if after, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml")); err != nil || string(after) != string(codexConfig) {
		t.Fatalf("Codex config changed when Claude Code was configured: err=%v", err)
	}
	conversion, err := core.Conversion(context.Background())
	if err != nil || conversion.Routes["codex"] != "chat-upstream" || conversion.Routes["claude-code"] != "second-chat" {
		t.Fatalf("routes = %#v, err=%v", conversion.Routes, err)
	}

	send := func(agentID string) {
		t.Helper()
		key, err := core.providers.Get(converterRouteID(agentID))
		if err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest(http.MethodPost, "http://"+listen+"/v1/chat/completions", strings.NewReader(`{"model":"alias","messages":[{"role":"user","content":"ping"}]}`))
		request.Header.Set("Authorization", "Bearer "+key.APIKey)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s request status = %d", agentID, response.StatusCode)
		}
	}
	firstBefore, secondBefore := len(*firstKeys), len(*secondKeys)
	send("codex")
	send("claude-code")
	if got := (*firstKeys)[firstBefore:]; len(got) != 1 || got[0] != "Bearer upstream-secret" {
		t.Fatalf("Codex reached the first service with %v", got)
	}
	if got := (*secondKeys)[secondBefore:]; len(got) != 1 || got[0] != "Bearer second-secret" {
		t.Fatalf("Claude Code reached the second service with %v", got)
	}

	// An unknown local key is refused, not sent to some default.
	request, _ := http.NewRequest(http.MethodPost, "http://"+listen+"/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	request.Header.Set("Authorization", "Bearer not-a-route")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown key status = %d", response.StatusCode)
	}
}

func TestChangingOneAgentsTargetKeepsItsKeyAndLeavesOthersAlone(t *testing.T) {
	upstream := chatOnlyUpstream(t)
	core, home, _ := chatProfileCore(t, upstream.URL)
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "other-chat", Label: "Other", Provider: "chat-upstream", Model: "model-b", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	for _, agentID := range []string{"codex", "claude-code"} {
		if _, err := core.ConfigureAgentWithConversion(context.Background(), agentID, "chat-upstream"); err != nil {
			t.Fatal(err)
		}
	}
	keyBefore, _ := core.providers.Get(converterRouteID("codex"))
	claudeBefore, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := core.ConfigureAgentWithConversion(context.Background(), "codex", "other-chat"); err != nil {
		t.Fatal(err)
	}
	conversion, _ := core.Conversion(context.Background())
	if conversion.Routes["codex"] != "other-chat" || conversion.Routes["claude-code"] != "chat-upstream" {
		t.Fatalf("routes = %#v", conversion.Routes)
	}
	if keyAfter, _ := core.providers.Get(converterRouteID("codex")); keyAfter.APIKey != keyBefore.APIKey {
		t.Fatal("changing Codex's target rotated its local key")
	}
	if after, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); err != nil || string(after) != string(claudeBefore) {
		t.Fatalf("Claude Code settings changed when Codex's target did: err=%v", err)
	}
}

func TestDeletingARouteTargetIsRefusedWhileAnAgentUsesIt(t *testing.T) {
	upstream := chatOnlyUpstream(t)
	core, _, _ := chatProfileCore(t, upstream.URL)
	if _, err := core.ConfigureAgentWithConversion(context.Background(), "codex", "chat-upstream"); err != nil {
		t.Fatal(err)
	}
	err := core.DeleteProfile(context.Background(), "chat-upstream")
	if err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("delete error = %v", err)
	}
}

func TestConfigureAgentWithConversionRollsBackWhenTheAgentConfigCannotBeWritten(t *testing.T) {
	upstream := chatOnlyUpstream(t)
	core, home, _ := chatProfileCore(t, upstream.URL)
	// Saved by SaveConversion above while it was still disabled.
	before, err := core.Conversion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(broken), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("invalid = ["), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := core.ConfigureAgentWithConversion(context.Background(), "codex", "chat-upstream"); err == nil {
		t.Fatal("an unparseable Codex config unexpectedly succeeded")
	}
	data, err := os.ReadFile(broken)
	if err != nil || string(data) != "invalid = [" {
		t.Fatalf("Codex config was not preserved: %q, err=%v", data, err)
	}
	after, err := core.Conversion(context.Background())
	if err != nil || after.Enabled != before.Enabled {
		t.Fatalf("conversion after rollback = %#v (before %#v), err=%v", after, before, err)
	}
	for _, id := range []string{converterPrefix + "anthropic", converterPrefix + "responses", converterPrefix + "chat"} {
		path, _ := core.profiles.ProfilePath(id)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("rollback left converter Profile %s: %v", id, err)
		}
	}
	if binding, err := core.profiles.ReadAgentBinding("codex"); err != nil || binding != nil {
		t.Fatalf("rollback left a Codex binding: %#v, err=%v", binding, err)
	}
}

func TestConfigureAgentWithConversionRefusesWhatItCannotHelp(t *testing.T) {
	upstream := chatOnlyUpstream(t)
	core, _, _ := chatProfileCore(t, upstream.URL)
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "responses-profile", Label: "Responses", Provider: "chat-upstream", Model: "model-a", Protocol: provider.ProtocolResponses, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, agent, profile string
	}{
		{name: "Agent already speaks Chat Completions", agent: "opencode", profile: "chat-upstream"},
		{name: "target is not a Chat Completions Profile", agent: "codex", profile: "responses-profile"},
		{name: "unknown Agent", agent: "no-such-agent", profile: "chat-upstream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := core.ConfigureAgentWithConversion(context.Background(), test.agent, test.profile); err == nil {
				t.Fatal("unexpectedly succeeded")
			}
		})
	}
}

func TestProbeProviderReportsAProtocolCompatibilityModeCanServe(t *testing.T) {
	for _, test := range []struct {
		name        string
		responses   int
		chat        int
		convertible []string
		chatProbed  bool
	}{
		{name: "Responses missing, Chat Completions works", responses: http.StatusNotFound, chat: http.StatusNoContent, convertible: []string{provider.ProtocolResponses}, chatProbed: true},
		{name: "both missing", responses: http.StatusNotFound, chat: http.StatusNotFound, chatProbed: true},
		{name: "rejected key is not a protocol problem", responses: http.StatusUnauthorized},
		{name: "server error is not a protocol problem", responses: http.StatusBadGateway},
		{name: "Responses works", responses: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
				mu.Lock()
				paths = append(paths, request.URL.Path)
				mu.Unlock()
				switch request.URL.Path {
				case "/v1/responses":
					return appProviderResponse(test.responses, ""), nil
				case "/v1/chat/completions":
					return appProviderResponse(test.chat, ""), nil
				}
				t.Fatalf("unexpected probe path %s", request.URL.Path)
				return nil, nil
			}))
			if _, err := core.SaveProvider(context.Background(), provider.Entry{
				ID: "upstream", Name: "Upstream", BaseURL: "https://upstream.example/v1", APIKey: "provider-secret",
			}, true, false); err != nil {
				t.Fatal(err)
			}
			result, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{Provider: "upstream", Model: "model-a", AgentIDs: []string{"codex"}})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(result.Convertible, test.convertible) {
				t.Fatalf("convertible = %v, want %v", result.Convertible, test.convertible)
			}
			if probed := slices.Contains(paths, "/v1/chat/completions"); probed != test.chatProbed {
				t.Fatalf("Chat Completions probed = %v, want %v (paths %v)", probed, test.chatProbed, paths)
			}
		})
	}
}

// leaveCompatibilityMode applies an ordinary Profile to an Agent, which is the
// only way an Agent leaves compatibility mode.
func leaveCompatibilityMode(t *testing.T, core *UseCases, agentID string) {
	t.Helper()
	protocol, err := conversionAgentProtocol(agentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.providers.Get("native-upstream"); err != nil {
		if _, err := core.SaveProvider(context.Background(), provider.Entry{
			ID: "native-upstream", Name: "Native upstream", BaseURL: "https://native.example/v1", AnthropicBaseURL: "https://native.example", APIKey: "native-secret",
		}, true, false); err != nil {
			t.Fatal(err)
		}
	}
	profileID := "native-" + agentID
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: profileID, Label: profileID, Provider: "native-upstream", Model: "model-n", Protocol: protocol, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{
		AgentID: agentID, Provider: "native-upstream", Model: "model-n", ProfileID: profileID,
	}); err != nil {
		t.Fatal(err)
	}
}

func adapterListening(listen string) bool {
	connection, err := net.Dial("tcp", listen)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

// Compatibility mode has no switch of its own to remember. It ends where it
// began, on the Agent: one that moves to an ordinary Profile gives up its
// route, and the adapter stops when the last one has.
func TestSettleConversionDropsTheRoutesOfAgentsThatLeftAndStopsWhenNoneIsLeft(t *testing.T) {
	upstream := chatOnlyUpstream(t)
	core, _, listen := chatProfileCore(t, upstream.URL)
	for _, agentID := range []string{"codex", "claude-code"} {
		if _, err := core.ConfigureAgentWithConversion(context.Background(), agentID, "chat-upstream"); err != nil {
			t.Fatal(err)
		}
	}

	leaveCompatibilityMode(t, core, "codex")
	if err := core.SettleConversion(context.Background()); err != nil {
		t.Fatal(err)
	}
	conversion, err := core.Conversion(context.Background())
	if err != nil || !conversion.Enabled || len(conversion.Routes) != 1 || conversion.Routes["claude-code"] != "chat-upstream" {
		t.Fatalf("after Codex left: %#v, err=%v", conversion, err)
	}
	if !adapterListening(listen) {
		t.Fatal("the adapter stopped while Claude Code still used it")
	}

	leaveCompatibilityMode(t, core, "claude-code")
	if err := core.SettleConversion(context.Background()); err != nil {
		t.Fatal(err)
	}
	conversion, err = core.Conversion(context.Background())
	if err != nil || conversion.Enabled || len(conversion.Routes) != 0 {
		t.Fatalf("after the last Agent left: %#v, err=%v", conversion, err)
	}
	if adapterListening(listen) {
		t.Fatal("the adapter kept running with no Agent pointed at it")
	}
}

// An Agent that left compatibility mode used to leave its route behind, and a
// route whose Profile was then deleted failed every later save: the adapter
// could not be stopped, no other Agent could be switched to it, and its keys
// could not be regenerated.
func TestALeftoverRouteDoesNotBlockCompatibilityModeForOtherAgents(t *testing.T) {
	upstream := chatOnlyUpstream(t)
	core, _, listen := chatProfileCore(t, upstream.URL)
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "short-lived", Label: "Short-lived", Provider: "chat-upstream", Model: "model-b", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfigureAgentWithConversion(context.Background(), "codex", "short-lived"); err != nil {
		t.Fatal(err)
	}
	// Not settled: this is the state an earlier version left on disk.
	leaveCompatibilityMode(t, core, "codex")
	if err := core.DeleteProfile(context.Background(), "short-lived"); err != nil {
		t.Fatal(err)
	}
	if conversion, _ := core.Conversion(context.Background()); conversion.Routes["codex"] != "short-lived" {
		t.Fatalf("the leftover route this test is about is missing: %#v", conversion.Routes)
	}

	if _, err := core.ConfigureAgentWithConversion(context.Background(), "claude-code", "chat-upstream"); err != nil {
		t.Fatalf("another Agent could not use compatibility mode: %v", err)
	}
	if _, err := core.RegenerateConversionAPIKey(context.Background()); err != nil {
		t.Fatalf("local keys could not be regenerated: %v", err)
	}
	if _, err := core.SetConversionEnabled(context.Background(), false); err != nil {
		t.Fatalf("compatibility mode could not be stopped: %v", err)
	}
	if adapterListening(listen) {
		t.Fatal("the adapter kept running after it was stopped")
	}
	// And the next settle clears it away, without stopping what is in use.
	if _, err := core.SetConversionEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := core.SettleConversion(context.Background()); err != nil {
		t.Fatal(err)
	}
	conversion, err := core.Conversion(context.Background())
	if err != nil || !conversion.Enabled || len(conversion.Routes) != 1 || conversion.Routes["claude-code"] != "chat-upstream" {
		t.Fatalf("after settling: %#v, err=%v", conversion, err)
	}
}

// The adapter only converts into Chat Completions. Changing what the target
// Profile speaks would have every request of the Agent behind it refused, so
// it is refused at the edit, like deleting the Profile is.
func TestChangingTheProtocolOfARouteTargetIsRefusedWhileAnAgentUsesIt(t *testing.T) {
	upstream := chatOnlyUpstream(t)
	core, _, _ := chatProfileCore(t, upstream.URL)
	if _, err := core.ConfigureAgentWithConversion(context.Background(), "codex", "chat-upstream"); err != nil {
		t.Fatal(err)
	}
	edit := SaveProfileOptions{ID: "chat-upstream", Label: "Chat upstream", Provider: "chat-upstream", Model: "model-a", Protocol: provider.ProtocolResponses, ConfigMode: "provider"}
	if _, err := core.SaveProfile(context.Background(), edit); err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("protocol change error = %v", err)
	}
	if stored := core.profileByID("chat-upstream"); stored.Protocol != provider.ProtocolOpenAI {
		t.Fatalf("the refused edit was stored: protocol = %q", stored.Protocol)
	}
	// Any other edit still goes through.
	edit.Protocol, edit.Model = provider.ProtocolOpenAI, "model-b"
	if _, err := core.SaveProfile(context.Background(), edit); err != nil {
		t.Fatal(err)
	}

	leaveCompatibilityMode(t, core, "codex")
	if err := core.SettleConversion(context.Background()); err != nil {
		t.Fatal(err)
	}
	edit.Protocol = provider.ProtocolResponses
	if _, err := core.SaveProfile(context.Background(), edit); err != nil {
		t.Fatalf("the Profile could not be changed once nothing was forwarded to it: %v", err)
	}
}
