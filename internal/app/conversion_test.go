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

func TestSaveConversionGeneratesPrivateKeyAndProtocolSpecificEndpoints(t *testing.T) {
	home := t.TempDir()
	listen := availableConversionListen(t)
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")})
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

	got, err := core.SaveConversion(context.Background(), ConversionConfig{
		Enabled: true, Listen: listen, TargetProfile: "upstream-chat",
	})
	defer func() { _ = core.CloseConversion() }()
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasAPIKey {
		t.Fatal("enabled conversion did not report a generated key")
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), `"api_key":`) || strings.Contains(string(wire), "secret") {
		t.Fatalf("conversion DTO leaked a key: %s", wire)
	}

	anthropic, err := core.providers.Get(converterPrefix + "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if anthropic.APIKey == "" || anthropic.APIKey == "upstream-secret" {
		t.Fatal("converter key was not independently generated")
	}
	if anthropic.BaseURL != "http://"+listen+"/v1" || anthropic.AnthropicBaseURL != "http://"+listen {
		t.Fatalf("anthropic converter endpoints = %#v", anthropic)
	}
	for _, kind := range []string{"responses", "chat"} {
		entry, err := core.providers.Get(converterPrefix + kind)
		if err != nil {
			t.Fatal(err)
		}
		if entry.BaseURL != "http://"+listen+"/v1" || entry.APIKey != anthropic.APIKey {
			t.Fatalf("%s converter = %#v", kind, entry)
		}
	}
}

func TestRegenerateConversionAPIKeyRotatesEveryConverterProvider(t *testing.T) {
	home := t.TempDir()
	listen := availableConversionListen(t)
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("macos", "arm64")})
	defer func() { _ = core.CloseConversion() }()
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
	if _, err := core.SaveConversion(context.Background(), ConversionConfig{Enabled: true, Listen: listen, TargetProfile: "upstream-chat"}); err != nil {
		t.Fatal(err)
	}
	before := core.conversionAPIKey()
	got, err := core.RegenerateConversionAPIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after := core.conversionAPIKey()
	if !got.HasAPIKey || before == "" || after == "" || before == after {
		t.Fatalf("key rotation before=%q after=%q result=%#v", before, after, got)
	}
	for _, kind := range []string{"anthropic", "responses", "chat"} {
		entry, err := core.providers.Get(converterPrefix + kind)
		if err != nil || entry.APIKey != after {
			t.Fatalf("%s converter key was not rotated", kind)
		}
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
