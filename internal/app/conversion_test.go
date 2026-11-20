package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

func availableConversionListen(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

// legacySharedConverter recreates the shared converter entries an older
// release wrote, which Agents may still be pointed at.
func legacySharedConverter(t *testing.T, core *UseCases, listen string) string {
	t.Helper()
	key, err := newConversionAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	for kind, protocol := range map[string]string{"anthropic": provider.ProtocolAnthropic, "responses": provider.ProtocolResponses, "chat": provider.ProtocolOpenAI} {
		if err := core.saveConverterEntry(context.Background(), converterPrefix+kind, "BootAgent Converter "+kind, protocol, key, "alias", listen); err != nil {
			t.Fatal(err)
		}
	}
	return key
}

func upstreamChatCore(t *testing.T) *UseCases {
	t.Helper()
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("macos", "arm64")})
	t.Cleanup(func() { _ = core.CloseConversion() })
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "upstream", Name: "Upstream", BaseURL: "https://upstream.example/v1", APIKey: "upstream-secret",
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "upstream-chat", Label: "Upstream Chat", Provider: "upstream", Model: "model-a",
		Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	return core
}

// Turning compatibility mode on is no longer what creates converter entries:
// each Agent gets its own when it is pointed at the adapter.
func TestSaveConversionStartsTheListenerWithoutSharedEntries(t *testing.T) {
	core := upstreamChatCore(t)
	listen := availableConversionListen(t)

	got, err := core.SaveConversion(context.Background(), ConversionConfig{Enabled: true, Listen: listen})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.HasAPIKey {
		t.Fatalf("conversion = %#v", got)
	}
	if _, err := core.providers.Get(converterPrefix + "anthropic"); err == nil {
		t.Fatal("enabling created the shared converter entries")
	}
	conn, err := net.Dial("tcp", listen)
	if err != nil {
		t.Fatalf("adapter is not listening: %v", err)
	}
	_ = conn.Close()
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), `"api_key":`) || strings.Contains(string(wire), "secret") {
		t.Fatalf("conversion DTO leaked a key: %s", wire)
	}
}

func TestSaveConversionCarriesTheListenAddressToSharedEntries(t *testing.T) {
	core := upstreamChatCore(t)
	key := legacySharedConverter(t, core, availableConversionListen(t))
	listen := availableConversionListen(t)

	if _, err := core.SaveConversion(context.Background(), ConversionConfig{Enabled: true, Listen: listen, TargetProfile: "upstream-chat"}); err != nil {
		t.Fatal(err)
	}
	anthropic, err := core.providers.Get(converterPrefix + "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if anthropic.APIKey != key || anthropic.BaseURL != "http://"+listen+"/v1" || anthropic.AnthropicBaseURL != "http://"+listen {
		t.Fatalf("anthropic converter = %#v", anthropic)
	}
	for _, kind := range []string{"responses", "chat"} {
		entry, err := core.providers.Get(converterPrefix + kind)
		if err != nil || entry.BaseURL != "http://"+listen+"/v1" || entry.APIKey != key {
			t.Fatalf("%s converter = %#v, err=%v", kind, entry, err)
		}
	}
}

func TestRegenerateConversionAPIKeyRotatesEveryConverterProvider(t *testing.T) {
	core := upstreamChatCore(t)
	listen := availableConversionListen(t)
	shared := legacySharedConverter(t, core, listen)
	if _, err := core.SaveConversion(context.Background(), ConversionConfig{Enabled: true, Listen: listen, TargetProfile: "upstream-chat"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := core.ensureConversionRoute(context.Background(), "codex", "upstream-chat"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := core.ensureConversionRoute(context.Background(), "claude-code", "upstream-chat"); err != nil {
		t.Fatal(err)
	}
	routeKey := func(agentID string) string {
		entry, err := core.providers.Get(converterRouteID(agentID))
		if err != nil {
			t.Fatal(err)
		}
		return entry.APIKey
	}
	codexBefore, claudeBefore := routeKey("codex"), routeKey("claude-code")

	got, err := core.RegenerateConversionAPIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after := core.conversionAPIKey()
	if !got.HasAPIKey || after == "" || after == shared {
		t.Fatalf("shared key rotation before=%q after=%q result=%#v", shared, after, got)
	}
	for _, kind := range []string{"anthropic", "responses", "chat"} {
		entry, err := core.providers.Get(converterPrefix + kind)
		if err != nil || entry.APIKey != after {
			t.Fatalf("%s converter key was not rotated", kind)
		}
	}
	codexAfter, claudeAfter := routeKey("codex"), routeKey("claude-code")
	if codexAfter == codexBefore || claudeAfter == claudeBefore || codexAfter == claudeAfter || codexAfter == after {
		t.Fatalf("route keys were not rotated independently: codex %q→%q, claude %q→%q", codexBefore, codexAfter, claudeBefore, claudeAfter)
	}
}

func TestSaveConversionRejectsNonLoopbackListener(t *testing.T) {
	core := NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("macos", "arm64")})
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "upstream", Name: "Upstream", BaseURL: "https://upstream.example/v1", APIKey: "upstream-secret",
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "upstream-chat", Label: "Upstream Chat", Provider: "upstream", Model: "model-a", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveConversion(context.Background(), ConversionConfig{
		Enabled: true, Listen: "0.0.0.0:8787", TargetProfile: "upstream-chat",
	}); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback listener error = %v", err)
	}
}

func TestSaveConversionRejectsEphemeralPort(t *testing.T) {
	if err := validateConversionListen("127.0.0.1:0"); err == nil {
		t.Fatal("ephemeral port was accepted even though it cannot be persisted as the actual listener")
	}
}

func TestSaveConversionRollsBackWhenThePortIsUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")})
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "upstream", Name: "Upstream", BaseURL: "https://upstream.example/v1", APIKey: "upstream-secret",
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{
		ID: "upstream-chat", Label: "Upstream Chat", Provider: "upstream", Model: "model-a", Protocol: provider.ProtocolOpenAI, ConfigMode: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	providersBefore, err := os.ReadFile(core.providers.Path())
	if err != nil {
		t.Fatal(err)
	}
	_, err = core.SaveConversion(context.Background(), ConversionConfig{
		Enabled: true, Listen: listener.Addr().String(), TargetProfile: "upstream-chat",
	})
	if err == nil || oneerrors.As(err).Code != oneerrors.ConversionPortUnavailable {
		t.Fatalf("occupied port error = %#v", oneerrors.As(err))
	}
	providersAfter, readErr := os.ReadFile(core.providers.Path())
	if readErr != nil || string(providersAfter) != string(providersBefore) {
		t.Fatalf("providers changed after failed start: %v", readErr)
	}
	for _, id := range []string{converterPrefix + "anthropic", converterPrefix + "responses", converterPrefix + "chat"} {
		path, _ := core.profiles.ProfilePath(id)
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("failed start left %s: %v", id, statErr)
		}
	}
}
