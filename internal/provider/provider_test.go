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

// The generator IDs the aggregators OneAgent ships actually return. Before the
// video and audio terms were added, every one of these was treated as a chat
// model, so an aggregator listing one first had it probed with a chat payload.
func TestPickChatModelSkipsGeneratorModels(t *testing.T) {
	generators := []string{
		"wan-ai/wan2.1-t2v-14b", "kwai/kling-v1-video", "sora-2", "veo-3.0-generate-001",
		"zai-org/cogvideox-5b", "tencent/hunyuan-video", "bytedance/seedance-1-0-pro",
		"minimaxai/minimax-hailuo-02", "stabilityai/stable-video-diffusion", "suno/bark",
		"black-forest-labs/flux-1-schnell", "qwen/qwen-image-edit", "minimaxai/minimax-speech-02",
	}
	for _, id := range generators {
		if got := PickChatModel([]string{id, "deepseek/deepseek-v4-pro"}); got != "deepseek/deepseek-v4-pro" {
			t.Errorf("PickChatModel picked the generator %q", got)
		}
	}
}

// The other half of the denylist's contract, and the reason it cannot simply be
// made broader: a term like "wan" or "veo" that matched real chat model families
// would push the probe onto models[0] and reintroduce the bug from the other side.
func TestPickChatModelKeepsRealChatModels(t *testing.T) {
	for _, id := range []string{
		"deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-flash", "gpt-5.6-terra", "claude-fable-5",
		"qwen/qwen3-235b-a22b", "moonshotai/kimi-k2", "meta-llama/llama-4-maverick",
		"zai-org/glm-4.6", "minimaxai/minimax-m2", "openai/gpt-oss-120b",
	} {
		if got := PickChatModel([]string{id}); got != id {
			t.Errorf("chat model %q was classified as non-chat", id)
		}
	}
}
