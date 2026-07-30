package provider

import (
	"reflect"
	"testing"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

func TestValidateBaseURL(t *testing.T) {
	for _, value := range []string{"", "ftp://example.com", "https:///missing-host", "https://user:pass@example.com", "https://example.com\n"} {
		if _, err := ValidateBaseURL(value); err == nil {
			t.Errorf("ValidateBaseURL(%q) unexpectedly succeeded", value)
		}
	}
	got, err := ValidateBaseURL("https://example.com///")
	if err != nil || got != "https://example.com" {
		t.Fatalf("ValidateBaseURL() = %q, %v", got, err)
	}
}

func TestProviderResolution(t *testing.T) {
	tests := []struct {
		provider, custom, protocol, want string
	}{
		{"ppio", "", ProtocolOpenAI, "https://api.ppio.com/openai"},
		{"ppio", "", ProtocolAnthropic, "https://api.ppio.com/anthropic"},
		{"novita", "", ProtocolAnthropic, "https://api.novita.ai/anthropic"},
		{"custom", "http://127.0.0.1:9000", ProtocolAnthropic, "http://127.0.0.1:9000"},
		{"ppio", "https://override.example/", ProtocolAnthropic, "https://override.example"},
	}
	for _, test := range tests {
		got, err := ProviderConfigBase(test.provider, test.custom, test.protocol)
		if err != nil || got != test.want {
			t.Errorf("ProviderConfigBase(%q, %q, %q) = %q, %v; want %q", test.provider, test.custom, test.protocol, got, err, test.want)
		}
	}
	if _, err := ProviderBase("custom", ""); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("missing custom URL error = %v", err)
	}
	if _, err := ProviderHome("custom"); err == nil {
		t.Fatal("custom registration unexpectedly allowed")
	}
}

func TestEndpointNormalizers(t *testing.T) {
	openAI := map[string]string{
		"https://example.com/v1/responses":        "https://example.com/v1",
		"https://example.com/v1/models":           "https://example.com/v1",
		"https://example.com/v1/chat/completions": "https://example.com/v1",
		"https://example.com":                     "https://example.com/v1",
	}
	for input, want := range openAI {
		if got := OpenAIBaseURL(input); got != want {
			t.Errorf("OpenAIBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
	anthropic := map[string]string{
		"https://api.ppio.com/anthropic":  "https://api.ppio.com/anthropic/v1/messages",
		"https://proxy.test/v1":           "https://proxy.test/v1/messages",
		"https://proxy.test/v1/messages":  "https://proxy.test/v1/messages",
		"https://proxy.test/messages":     "https://proxy.test/v1/messages",
		"https://proxy.test/anthropic///": "https://proxy.test/anthropic/v1/messages",
	}
	for input, want := range anthropic {
		if got := AnthropicMessagesURL(input); got != want {
			t.Errorf("AnthropicMessagesURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestProtocolAndModelProjection(t *testing.T) {
	if ProtocolForAdapter("codex") != ProtocolResponses || ProtocolForAdapter("claude-code") != ProtocolAnthropic || ProtocolForAdapter("new") != ProtocolOpenAI {
		t.Fatal("adapter protocol projection diverged")
	}
	if ProtocolLabel(ProtocolResponses) != "OpenAI Responses" || ProtocolLabel("future") != "future" {
		t.Fatal("protocol labels diverged")
	}
	got := []string{
		PickChatModel([]string{"whisper-large-v3", "bge-reranker-v2", "deepseek-v3", "gpt-5.6-terra"}),
		PickChatModel([]string{"resolver-1", "evolve-chat"}),
	}
	if !reflect.DeepEqual(got, []string{"deepseek-v3", "resolver-1"}) {
		t.Fatalf("PickChatModel() = %v", got)
	}
	if PickChatModel([]string{"text-embedding-3-small", "whisper-1"}) != "text-embedding-3-small" {
		t.Fatal("all non-chat models should fall back to the first ID")
	}
}

func TestInvalidProtocolErrorIsStable(t *testing.T) {
	err := invalidProtocol("grpc")
	if oneerrors.As(err).Code != oneerrors.InvalidRequest || err.Error() != "Unknown inference protocol: grpc" {
		t.Fatalf("invalid protocol error = %v", err)
	}
}
