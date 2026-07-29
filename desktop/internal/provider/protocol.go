package provider

import (
	"regexp"
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
)

// protocolLabels are the names shown to the user in a probe result.
var protocolLabels = map[string]string{
	catalog.ProtocolOpenAI:    "OpenAI Chat Completions",
	catalog.ProtocolAnthropic: "Anthropic Messages",
	catalog.ProtocolResponses: "OpenAI Responses",
}

// ProtocolLabel names a protocol for display, falling back to the raw id so an
// unrecognised value is visible rather than blank.
func ProtocolLabel(protocol string) string {
	if label, known := protocolLabels[protocol]; known {
		return label
	}
	return protocol
}

// KnownProtocol reports whether this is a protocol OneAgent can probe.
func KnownProtocol(protocol string) bool {
	_, known := protocolLabels[protocol]
	return known
}

// unsupportedMarkers appear in the body when an endpoint means "this model
// cannot serve this protocol". Endpoints answer that in several shapes: 400
// INVALID_REQUEST_BODY "does not support endpoint", 500 "not implemented", and
// plain 404/405 when the route is absent.
var unsupportedMarkers = []string{
	"does not support endpoint",
	"not implemented",
	"unsupported endpoint",
	"unknown endpoint",
}

// UnsupportedProtocol reports whether a response means the model/protocol pair
// can never work.
//
// The distinction matters because these are permanent: reporting them as
// retryable would have the user retry something that cannot succeed, instead of
// choosing a model that serves the protocol.
func UnsupportedProtocol(statusCode int, body string) bool {
	switch statusCode {
	case 404, 405, 501:
		return true
	case 400, 422, 500:
		lowered := strings.ToLower(body)
		for _, marker := range unsupportedMarkers {
			if strings.Contains(lowered, marker) {
				return true
			}
		}
	}
	return false
}

// nonChatModel matches IDs that plainly do not serve chat.
//
// Anchored on separators rather than bare substrings, so "resolver-1" is not
// rejected for containing "sql" and "evolve-chat" is not rejected for
// containing "vl". Go's RE2 accepts this pattern unchanged from the Python one,
// and cross_parity_test.go holds both to the same classifications.
var nonChatModel = regexp.MustCompile(
	`(?i)(?:^|[-_/.])(?:embed(?:ding)?s?|rerank(?:er)?|ocr|whisper|asr|tts|speech|vl|vision|image|sdx?|flux|guard(?:rail)?s?|moderation|sql)(?:[-_/.]|$)`,
)

// PickChatModel returns the first model that plausibly serves chat.
//
// Probing an embedding or speech model fails for a reason unrelated to
// connectivity or the key, which would send the user looking for the wrong
// problem. When every listed model looks non-chat the first entry is returned
// and the probe failure explains itself -- refusing to probe would be worse,
// since the classifier is a heuristic and may be wrong.
func PickChatModel(models []string) string {
	if len(models) == 0 {
		return ""
	}
	for _, model := range models {
		if !nonChatModel.MatchString(model) {
			return model
		}
	}
	return models[0]
}
