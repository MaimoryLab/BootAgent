package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/desktopapp"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

func TestAssessClaudeDesktopProfileRequiresSuccessfulChatBeforeOfferingConversion(t *testing.T) {
	home := t.TempDir()
	var requestCount atomic.Int32
	client := provider.NewClient(appProviderDoer(func(request *http.Request) (*http.Response, error) {
		requestCount.Add(1)
		switch request.URL.Path {
		case "/v1/messages":
			return appProviderResponse(http.StatusNotFound, `{"error":{"message":"unsupported protocol"}}`), nil
		case "/v1/chat/completions":
			return appProviderResponse(http.StatusNoContent, ""), nil
		default:
			t.Fatalf("unexpected probe path %s", request.URL.Path)
			return nil, nil
		}
	}))
	core := NewUseCasesWithProviderClient(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")}, client)
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "chat-only", Name: "Chat only", BaseURL: "https://chat-only.example/v1", APIKey: "provider-secret",
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "chat-profile", Label: "Chat", Provider: "chat-only", Model: "model-a", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}

	assessment, err := core.AssessDesktopAgentProfile(context.Background(), desktopapp.ClaudeDesktopID, "chat-profile")
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Compatibility != DesktopProfileConvertible || !assessment.UpstreamVerified {
		t.Fatalf("assessment = %#v", assessment)
	}
	assessment, err = core.AssessDesktopAgentProfile(context.Background(), desktopapp.ClaudeDesktopID, "chat-profile")
	if err != nil || assessment.Compatibility != DesktopProfileConvertible {
		t.Fatalf("cached assessment = %#v, err=%v", assessment, err)
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("two assessments sent %d requests, want one two-protocol probe", got)
	}
}

func TestAssessClaudeDesktopProfileReportsNativeAndUnusable(t *testing.T) {
	for _, test := range []struct {
		name          string
		protocol      string
		status        int
		body          string
		compatibility string
		errorCode     string
	}{
		{name: "native Anthropic", protocol: provider.ProtocolAnthropic, status: http.StatusNoContent, compatibility: DesktopProfileNative},
		{name: "rejected key", protocol: provider.ProtocolOpenAI, status: http.StatusUnauthorized, body: `{"error":{"message":"invalid api key"}}`, compatibility: DesktopProfileUnusable, errorCode: "API_KEY_REJECTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := provider.NewClient(appProviderDoer(func(request *http.Request) (*http.Response, error) {
				return appProviderResponse(test.status, test.body), nil
			}))
			core := NewUseCasesWithProviderClient(StatusOptions{Home: t.TempDir(), Platform: platform.For("macos", "arm64")}, client)
			if _, err := core.SaveProvider(context.Background(), provider.Entry{
				ID: "target", Name: "Target", BaseURL: "https://target.example/v1", APIKey: "provider-secret",
			}, true, false); err != nil {
				t.Fatal(err)
			}
			if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
				ID: "target-profile", Label: "Target", Provider: "target", Model: "claude-test", Protocol: test.protocol, ConfigMode: "provider",
			}); err != nil {
				t.Fatal(err)
			}
			assessment, err := core.AssessDesktopAgentProfile(context.Background(), desktopapp.ClaudeDesktopID, "target-profile")
			if err != nil {
				t.Fatal(err)
			}
			if assessment.Compatibility != test.compatibility || assessment.ErrorCode != test.errorCode {
				t.Fatalf("assessment = %#v", assessment)
			}
		})
	}
}

func TestConfigureClaudeDesktopWithConversionCompletesEndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := listener.Addr().String()
	_ = listener.Close()

	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")})
	defer func() { _ = core.CloseConversion() }()
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "chat-upstream", Name: "Chat upstream", BaseURL: upstream.URL, APIKey: "upstream-secret",
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "chat-upstream", Label: "Chat upstream", Provider: "chat-upstream", Model: "model-a", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveConversion(context.Background(), ConversionConfig{Listen: listen, TargetProfile: "chat-upstream"}); err != nil {
		t.Fatal(err)
	}

	result, err := core.ConfigureDesktopAgentWithConversion(context.Background(), desktopapp.ClaudeDesktopID, "chat-upstream")
	if err != nil {
		t.Fatal(err)
	}
	if !result.ConversionRunning || !result.LocalAuthVerified || !result.UpstreamVerified || !result.AgentConfigured || !result.EndToEndVerified {
		t.Fatalf("conversion result = %#v", result)
	}
	data, err := os.ReadFile(result.Config)
	if err != nil {
		t.Fatal(err)
	}
	var configured map[string]any
	if err := json.Unmarshal(data, &configured); err != nil {
		t.Fatal(err)
	}
	if configured["inferenceGatewayBaseUrl"] != "http://"+listen {
		t.Fatalf("Claude Desktop converter base = %#v", configured["inferenceGatewayBaseUrl"])
	}
	binding, err := core.profiles.ReadAgentBinding(desktopapp.ClaudeDesktopID)
	if err != nil || binding == nil || binding.ProfileRef != converterPrefix+"anthropic" {
		t.Fatalf("Claude Desktop binding = %#v, err=%v", binding, err)
	}
}

func TestConfigureClaudeDesktopWithConversionRollsBackAfterConfigFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := listener.Addr().String()
	_ = listener.Close()

	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")})
	defer func() { _ = core.CloseConversion() }()
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "chat-upstream", Name: "Chat upstream", BaseURL: upstream.URL, APIKey: "upstream-secret",
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "chat-upstream", Label: "Chat upstream", Provider: "chat-upstream", Model: "model-a", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveConversion(context.Background(), ConversionConfig{Listen: listen, TargetProfile: "chat-upstream"}); err != nil {
		t.Fatal(err)
	}
	providersBefore, err := os.ReadFile(core.providers.Path())
	if err != nil {
		t.Fatal(err)
	}
	brokenPath := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	if err := os.MkdirAll(filepath.Dir(brokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brokenPath, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := core.ConfigureDesktopAgentWithConversion(context.Background(), desktopapp.ClaudeDesktopID, "chat-upstream"); err == nil {
		t.Fatal("invalid Claude Desktop config unexpectedly succeeded")
	}
	providersAfter, err := os.ReadFile(core.providers.Path())
	if err != nil || string(providersAfter) != string(providersBefore) {
		t.Fatalf("providers were not restored: err=%v\nbefore=%s\nafter=%s", err, providersBefore, providersAfter)
	}
	for _, id := range []string{converterPrefix + "anthropic", converterPrefix + "responses", converterPrefix + "chat"} {
		path, _ := core.profiles.ProfilePath(id)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("rollback left converter Profile %s: %v", id, err)
		}
	}
	config, err := core.Conversion(context.Background())
	if err != nil || config.Enabled {
		t.Fatalf("conversion state after rollback = %#v, err=%v", config, err)
	}
	data, err := os.ReadFile(brokenPath)
	if err != nil || string(data) != "[]" {
		t.Fatalf("Claude Desktop config was not preserved: %q, err=%v", data, err)
	}
}

func TestDesktopAgentStatusIsUnsupportedOutsideDesktopPlatforms(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	status, err := core.DesktopAgentStatus(context.Background(), desktopapp.ChatGPTDesktopID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed || status.Supported || status.ID != desktopapp.ChatGPTDesktopID {
		t.Fatalf("status = %#v", status)
	}
	if status.ConfigPath != filepath.Join(home, ".codex", "config.toml") || status.ConfigSharedWith != "Codex" {
		t.Fatalf("shared config = %q with %q", status.ConfigPath, status.ConfigSharedWith)
	}
	if status.ProfileAgentID != "codex" || status.ProfileID != nil {
		t.Fatalf("profile projection = %#v", status)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("status probe touched shared Codex config: %v", err)
	}
}

func TestConfigureDesktopAgentAcceptsAnyProfileWithAnAPIMode(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "workbuddy", Label: "WorkBuddy", Provider: "ppio", Model: "model-a", ConfigMode: "provider", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfigureDesktopAgent(context.Background(), "workbuddy", "workbuddy"); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "workbuddy-own", Label: "WorkBuddy", Provider: "ppio", Model: "model-a", ConfigMode: "provider", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := core.ConfigureDesktopAgent(context.Background(), "workbuddy", "workbuddy-own")
	if err != nil || result.ProfileAgentID != "workbuddy" || result.ProfileID != "workbuddy-own" {
		t.Fatalf("configure result = %#v, err=%v", result, err)
	}
	binding, err := core.ListAgentBindings(context.Background())
	if err != nil || binding["workbuddy"].ProfileRef != "workbuddy-own" {
		t.Fatalf("desktop profile binding = %#v, err=%v", binding, err)
	}
}

func TestConfigureWorkBuddyWritesModelsJSONFromProvider(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")})
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "workbuddy-profile", Label: "WorkBuddy", Provider: "ppio", APIKey: "provider-secret",
		Model: "model-a", ConfigMode: "provider", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := core.ConfigureDesktopAgent(context.Background(), desktopapp.WorkBuddyID, "workbuddy-profile")
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(home, ".workbuddy", "models.json")
	if result.Config != wantPath || result.ProfileAgentID != desktopapp.WorkBuddyID || result.Restart == "" {
		t.Fatalf("WorkBuddy configure result = %#v", result)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	var models []map[string]any
	if err := json.Unmarshal(data, &models); err != nil || len(models) != 1 {
		t.Fatalf("WorkBuddy models = %s, err=%v", data, err)
	}
	if models[0]["id"] != "model-a" || models[0]["url"] != "https://api.ppio.com/openai" || models[0]["apiKey"] != "provider-secret" {
		t.Fatalf("WorkBuddy model = %#v", models[0])
	}
	info, err := os.Stat(wantPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("WorkBuddy config mode = %v, err=%v", info.Mode().Perm(), err)
	}
	reapplied, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "ppio", Name: "PPIO", BaseURL: "https://relay.example/openai", APIKey: "rotated-key",
	}, false, false)
	if err != nil || len(reapplied.Failures) != 0 || len(reapplied.Reapplied) != 1 || reapplied.Reapplied[0] != desktopapp.WorkBuddyID {
		t.Fatalf("WorkBuddy Provider reapply = %#v, err=%v", reapplied, err)
	}
	data, err = os.ReadFile(wantPath)
	if err != nil || json.Unmarshal(data, &models) != nil || models[0]["url"] != "https://relay.example/openai" || models[0]["apiKey"] != "rotated-key" {
		t.Fatalf("reapplied WorkBuddy models = %s, err=%v", data, err)
	}
	binding, err := core.profiles.ReadAgentBinding(desktopapp.WorkBuddyID)
	if err != nil || binding == nil || binding.BaseURL != "https://relay.example/openai" {
		t.Fatalf("reapplied WorkBuddy binding = %#v, err=%v", binding, err)
	}
	// A desktop Agent has to follow a Profile edit too, and it takes a different
	// branch than a managed CLI Agent -- WorkBuddy writes models.json through the
	// config adapter rather than through activation.
	profileEdit, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "workbuddy-profile", Label: "WorkBuddy", Provider: "ppio",
		Model: "model-b", ConfigMode: "provider", Protocol: "openai",
	})
	if err != nil || len(profileEdit.Failures) != 0 || len(profileEdit.Reapplied) != 1 {
		t.Fatalf("WorkBuddy Profile reapply = %#v, err=%v", profileEdit, err)
	}
	data, err = os.ReadFile(wantPath)
	if err != nil || json.Unmarshal(data, &models) != nil {
		t.Fatalf("WorkBuddy models unreadable after Profile edit: %s, err=%v", data, err)
	}
	// The adapter registers models rather than replacing the list, so assert the
	// new one arrived instead of asserting it is the only one.
	if !slices.ContainsFunc(models, func(model map[string]any) bool { return model["id"] == "model-b" }) {
		t.Fatalf("Profile edit did not reach WorkBuddy models: %s", data)
	}
	if binding, err := core.profiles.ReadAgentBinding(desktopapp.WorkBuddyID); err != nil || binding == nil || binding.Model != "model-b" {
		t.Fatalf("WorkBuddy binding did not follow the Profile: %#v, err=%v", binding, err)
	}
}

func TestConfigureClaudeDesktopUsesAnthropicProviderEndpoint(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")})
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "claude-desktop-profile", Label: "Claude Desktop", Provider: "jiekou", APIKey: "provider-secret",
		Model: "claude-sonnet-5", ConfigMode: "provider", Protocol: "anthropic", Context1M: true,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := core.ConfigureDesktopAgent(context.Background(), desktopapp.ClaudeDesktopID, "claude-desktop-profile")
	if err != nil {
		t.Fatal(err)
	}
	if result.ProfileAgentID != desktopapp.ClaudeDesktopID || result.Restart != "Restart Claude Desktop" || !strings.Contains(result.Config, filepath.Join("Claude-3p", "configLibrary")) {
		t.Fatalf("Claude Desktop configure result = %#v", result)
	}
	data, err := os.ReadFile(result.Config)
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	models := profile["inferenceModels"].([]any)
	if profile["inferenceGatewayBaseUrl"] != "https://api.highwayapi.ai/anthropic" || models[0].(map[string]any)["supports1m"] != true {
		t.Fatalf("Claude Desktop profile = %#v", profile)
	}
	binding, err := core.profiles.ReadAgentBinding(desktopapp.ClaudeDesktopID)
	if err != nil || binding == nil || binding.BaseURL != "https://api.highwayapi.ai/anthropic" {
		t.Fatalf("Claude Desktop binding = %#v, err=%v", binding, err)
	}
}

func TestConfigureDesktopAgentDoesNotLetBindingOverrideExplicitProfileOwner(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "codex-owned", Label: "Codex", Provider: "ppio", Model: "model-a", ConfigMode: "provider", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.profiles.WriteAgentBinding(context.Background(), "workbuddy", profileStore.BindingWriteRequest{
		Provider: "ppio", BaseURL: "https://api.ppio.com/openai", Model: "model-a", ProfileRef: "codex-owned",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfigureDesktopAgent(context.Background(), "workbuddy", "codex-owned"); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopAgentStatusDoesNotClaimCodexSharingForOtherApps(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	status := core.publicDesktopAgentStatus(desktopapp.Status{ID: "workbuddy", Name: "WorkBuddy"})
	if status.ProfileAgentID != "workbuddy" || status.ConfigSharedWith != "" || status.ConfigPath != "" {
		t.Fatalf("non-shared desktop projection = %#v", status)
	}
}

func TestInstallDesktopAgentDoesNotWriteSharedCodexConfig(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	_, err := core.InstallDesktopAgent(context.Background(), desktopapp.ChatGPTDesktopID, nil)
	if err == nil {
		t.Fatal("unsupported platform install unexpectedly succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(statErr) {
		t.Fatalf("install action touched shared Codex config: %v", statErr)
	}
}
