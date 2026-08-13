// Package provider contains Provider and protocol rules. Network clients are
// kept separate so validation does not open a request.
package provider

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const (
	ProtocolOpenAI    = catalog.ProtocolOpenAI
	ProtocolAnthropic = catalog.ProtocolAnthropic
	ProtocolResponses = catalog.ProtocolResponses
)

// nonChatModel matches IDs that clearly name a non-chat endpoint. It is defence
// in depth, not the primary guard: a denylist over third-party model IDs always
// lags the vendors, so callers that have a reviewed model for the Provider should
// prefer that (see app.resolveProviderModel). The video, audio and music terms
// were added after a t2v model returned first by an aggregator got probed with a
// chat payload and the failure read as a bad API key.
var nonChatModel = regexp.MustCompile(`(?i)(^|[-_/.])(embed(ding)?s?|rerank(er)?s?|ocr|whisper|asr|tts|speech|voice|audio|music|bark|vl|vision|image|img|sdx?|flux|video|t2v|i2v|v2v|t2i|i2t|t2a|sora|veo|kling|hailuo|seedance|cogvideox?|wan[0-9.]*|guard(rail)?s?|moderation|sql)([-_/.]|$)`)

// ValidateBaseURL accepts an explicit HTTP(S) origin or path and rejects the
// forms that could smuggle credentials or control characters into requests.
func ValidateBaseURL(value string) (string, error) {
	if value == "" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Custom base URL is required")
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return "", oneerrors.New(oneerrors.InvalidRequest, "Custom base URL contains control characters")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Custom base URL must start with http:// or https://")
	}
	if parsed.User != nil {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Custom base URL must not contain credentials")
	}
	return strings.TrimRight(value, "/"), nil
}

// ProviderBase resolves a catalog Provider or validates a custom endpoint.
// Built-in Providers may still receive an explicit override.
func ProviderBase(providerID, customBase string) (string, error) {
	if providerID == "custom" {
		return ValidateBaseURL(customBase)
	}
	if customBase != "" {
		return ValidateBaseURL(customBase)
	}
	meta, ok := catalog.ProviderByID(providerID)
	if !ok {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Provider must be a configured Provider or custom")
	}
	return meta.BaseURL, nil
}

// ProviderConfigBase selects the protocol-specific built-in endpoint. Custom
// endpoints are left untouched because the user owns their protocol contract.
func ProviderConfigBase(providerID, customBase, protocol string) (string, error) {
	base, err := ProviderBase(providerID, customBase)
	if err != nil {
		return "", err
	}
	if protocol == ProtocolAnthropic && customBase == "" && providerID != "custom" {
		meta, ok := catalog.ProviderByID(providerID)
		if ok && meta.AnthropicBaseURL != "" {
			return meta.AnthropicBaseURL, nil
		}
	}
	return base, nil
}

func ProviderHome(providerID string) (string, error) {
	meta, ok := catalog.ProviderByID(providerID)
	if !ok {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Registration is only available for a configured Provider")
	}
	return meta.Home, nil
}

func ProtocolForAdapter(adapter string) string {
	return catalog.ProtocolForAdapter(adapter)
}

func FallbackProbeModel(providerID string) string {
	return catalog.FallbackProbeModel(providerID)
}

func ProtocolLabel(protocol string) string {
	switch protocol {
	case ProtocolOpenAI:
		return "OpenAI Chat Completions"
	case ProtocolAnthropic:
		return "Anthropic Messages"
	case ProtocolResponses:
		return "OpenAI Responses"
	default:
		return protocol
	}
}

func OpenAIBaseURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/models"} {
		if before, ok := strings.CutSuffix(base, suffix); ok {
			base = strings.TrimRight(before, "/")
			break
		}
	}
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func AnthropicMessagesURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	for _, suffix := range []string{"/v1/messages", "/messages"} {
		if before, ok := strings.CutSuffix(base, suffix); ok {
			base = strings.TrimRight(before, "/")
			break
		}
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

// PickChatModel keeps provider ordering while skipping IDs that clearly refer
// to embeddings, rerankers, speech, vision, or other non-chat endpoints.
func PickChatModel(models []string) string {
	for _, model := range models {
		if !nonChatModel.MatchString(model) {
			return model
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func invalidProtocol(protocol string) error {
	return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Unknown inference protocol: %s", protocol))
}
