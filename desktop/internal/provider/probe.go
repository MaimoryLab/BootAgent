package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
)

// Verdict is one probe's outcome. The field set is the transport contract the
// frontend already consumes, so the JSON names are fixed.
//
// Reachable is separate from OK on purpose: "the endpoint answered and refused
// the key" and "nothing answered" need different advice, and collapsing them into
// one boolean is how a wrong key gets reported as a network problem.
type Verdict struct {
	OK        bool   `json:"ok"`
	Reachable bool   `json:"reachable"`
	Protocol  string `json:"protocol,omitempty"`
	Status    int    `json:"status"`
	Message   string `json:"message"`
	ErrorCode string `json:"error_code"`
	Retryable bool   `json:"retryable"`
	// Models is only set by discovery. A nil slice encodes as null, which is what
	// a protocol probe reports, while an empty slice encodes as [] for a
	// discovery that found none -- the frontend reads those as different states.
	Models []string `json:"models,omitempty"`
}

// ProbeRequest names what to probe. Grouped into a struct because the four
// callers each set a different subset, and positional arguments made it easy to
// pass the key where the model belonged.
type ProbeRequest struct {
	Protocol   string
	Provider   string
	CustomBase string
	APIKey     string
	Model      string
}

// Probe sends one minimal request over a protocol and reports whether the model
// actually serves it.
//
// Each protocol is checked separately because passing Chat Completions does not
// prove a model answers Responses or Anthropic Messages. Writing a config for a
// pair the endpoint refuses only moves the failure into the Agent, where the user
// has no way to see what went wrong.
func (c *Client) Probe(request ProbeRequest) (Verdict, error) {
	if request.APIKey == "" {
		return Verdict{}, oerr.New("INVALID_REQUEST", "API key is required")
	}
	if !KnownProtocol(request.Protocol) {
		return Verdict{}, oerr.Newf("INVALID_REQUEST", "Unknown inference protocol: %s", request.Protocol)
	}
	model := request.Model
	if model == "" {
		model = catalog.FallbackProbeModel(request.Provider)
	}
	httpRequest, err := c.protocolRequest(request, model)
	if err != nil {
		return Verdict{}, err
	}

	label := ProtocolLabel(request.Protocol)
	status, body, err := c.send(httpRequest)
	if err != nil {
		verdict := transportVerdict(err, false)
		verdict.Protocol = request.Protocol
		return verdict, nil
	}

	if status == http.StatusOK || status == http.StatusNoContent {
		return Verdict{
			OK: true, Reachable: true, Protocol: request.Protocol, Status: status,
			Message: label + " connection test passed.",
		}, nil
	}

	// A protocol the endpoint does not serve is not a connectivity failure, and
	// telling the user to check their network for it wastes their time.
	if UnsupportedProtocol(status, body) {
		return Verdict{
			Reachable: true, Protocol: request.Protocol, Status: status,
			Message: fmt.Sprintf(
				"Model %s does not support %s. Choose a model that serves this protocol.",
				pythonRepr(request.Model), label,
			),
			ErrorCode: "PROTOCOL_UNSUPPORTED",
		}, nil
	}

	verdict := statusVerdict(status, false)
	verdict.Protocol = request.Protocol
	return verdict, nil
}

// protocolRequest builds the minimal request for one protocol. The bodies are
// deliberately tiny: this is a reachability and support check, not a completion.
func (c *Client) protocolRequest(request ProbeRequest, model string) (*http.Request, error) {
	var url string
	var body map[string]any
	headers := map[string]string{
		"Authorization": "Bearer " + request.APIKey,
		"Content-Type":  "application/json",
	}

	switch request.Protocol {
	case catalog.ProtocolAnthropic:
		base, err := ConfigBase(request.Provider, request.CustomBase, request.Protocol)
		if err != nil {
			return nil, err
		}
		url = AnthropicMessagesURL(base)
		headers["X-Api-Key"] = request.APIKey
		headers["Anthropic-Version"] = "2023-06-01"
		body = map[string]any{
			"model":      model,
			"messages":   []any{map[string]any{"role": "user", "content": "ping"}},
			"max_tokens": 1,
		}
	default:
		base, err := Base(request.Provider, request.CustomBase)
		if err != nil {
			return nil, err
		}
		v1 := OpenAIBaseURL(base)
		if request.Protocol == catalog.ProtocolResponses {
			url = v1 + "/responses"
			// Reasoning models spend the budget before emitting text, so an
			// over-tight cap fails for a reason unrelated to protocol support.
			body = map[string]any{"model": model, "input": "ping", "max_output_tokens": 16}
		} else {
			url = v1 + "/chat/completions"
			body = map[string]any{
				"model":      model,
				"messages":   []any{map[string]any{"role": "user", "content": "ping"}},
				"max_tokens": 1,
			}
		}
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, oerr.Newf("INTERNAL_ERROR", "Cannot encode the probe request: %v", err)
	}
	httpRequest, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return nil, oerr.Newf("INVALID_REQUEST", "Cannot build a request for %s: %v", url, err)
	}
	for name, value := range headers {
		httpRequest.Header.Set(name, value)
	}
	return httpRequest, nil
}

// ListModels asks the endpoint what it serves.
func (c *Client) ListModels(providerID, customBase, apiKey string) (Verdict, error) {
	if apiKey == "" {
		return Verdict{}, oerr.New("INVALID_REQUEST", "API key is required")
	}
	base, err := Base(providerID, customBase)
	if err != nil {
		return Verdict{}, err
	}
	httpRequest, err := http.NewRequest(http.MethodGet, OpenAIBaseURL(base)+"/models", nil)
	if err != nil {
		return Verdict{}, oerr.Newf("INVALID_REQUEST", "Cannot build a request: %v", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)

	status, body, err := c.send(httpRequest)
	if err != nil {
		return transportVerdict(err, true), nil
	}
	if status != http.StatusOK {
		return statusVerdict(status, true), nil
	}

	var raw any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		// The parser's own wording is left out, the same decision as in the config
		// readers and for the same two reasons. It cannot agree across languages
		// -- Go says `invalid character '<'` where Python says `Expecting value:
		// line 1 column 1` -- and this text reaches the screen while the body it
		// describes came from an endpoint the user named. An error page that
		// echoed a query string would be republished with it.
		return Verdict{
			Reachable: true, Status: http.StatusOK, Models: []string{},
			Message:   "Model list response is not valid JSON.",
			ErrorCode: "MODELS_UNSUPPORTED",
		}, nil
	}

	models := modelIDsFrom(raw)
	verdict := Verdict{
		OK: len(models) > 0, Reachable: true, Status: http.StatusOK, Models: models,
	}
	if len(models) > 0 {
		verdict.Message = fmt.Sprintf("Found %d models.", len(models))
	} else {
		verdict.Message = "No model IDs returned; enter model ID manually."
		verdict.ErrorCode = "MODELS_UNSUPPORTED"
	}
	return verdict, nil
}

// modelIDsFrom pulls the ids out of either shape a provider answers with: a bare
// list, or the OpenAI {"data": [...]} envelope. Entries may be objects with an id
// or plain strings, and anything else is skipped rather than guessed at.
func modelIDsFrom(raw any) []string {
	var items []any
	switch typed := raw.(type) {
	case []any:
		items = typed
	case map[string]any:
		if data, ok := typed["data"].([]any); ok {
			items = data
		}
	}
	models := []string{}
	for _, item := range items {
		switch entry := item.(type) {
		case map[string]any:
			if id, ok := entry["id"].(string); ok && id != "" {
				models = append(models, id)
			}
		case string:
			if entry != "" {
				models = append(models, entry)
			}
		}
	}
	return models
}

// ResolveModel is the model to probe with when the user has not chosen one.
//
// A live listing is preferred because hardcoded ids go stale when a provider
// retires a model. A user-supplied model short-circuits discovery so a probe never
// costs an extra round trip, and an absent key skips it because discovery would
// refuse. Discovery failing is not fatal -- the catalog fallback is the narrow
// last resort.
func (c *Client) ResolveModel(providerID, apiKey, model, customBase string) string {
	if model != "" {
		return model
	}
	if apiKey == "" {
		return catalog.FallbackProbeModel(providerID)
	}
	listing, err := c.ListModels(providerID, customBase, apiKey)
	if err != nil || !listing.OK || len(listing.Models) == 0 {
		return catalog.FallbackProbeModel(providerID)
	}
	return PickChatModel(listing.Models)
}

// statusVerdict classifies an HTTP status the endpoint returned.
func statusVerdict(status int, models bool) Verdict {
	verdict := Verdict{Reachable: true, Status: status}
	if models {
		verdict.Models = []string{}
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		verdict.Message = fmt.Sprintf("API key was rejected (%d).", status)
		if models {
			verdict.Message += " Enter model ID manually."
		}
		verdict.ErrorCode = "API_KEY_REJECTED"
		// Retryable because the user can paste a different key, not because the
		// same request would succeed on a retry.
		verdict.Retryable = true
	case models && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed):
		verdict.Message = fmt.Sprintf(
			"This endpoint does not expose /v1/models (%d); enter model ID manually.", status,
		)
		verdict.ErrorCode = "MODELS_UNSUPPORTED"
	default:
		verdict.Message = fmt.Sprintf("Endpoint returned HTTP %d.", status)
		verdict.ErrorCode = "PROVIDER_UNREACHABLE"
		verdict.Retryable = status >= 500
	}
	return verdict
}

// transportVerdict classifies a failure where nothing answered.
func transportVerdict(err error, models bool) Verdict {
	verdict := Verdict{Status: 0, Retryable: true}
	if models {
		verdict.Models = []string{}
	}
	verdict.Message = "Cannot reach endpoint: " + reason(err)
	verdict.ErrorCode = "PROVIDER_UNREACHABLE"
	if isTimeout(err) {
		verdict.ErrorCode = "TIMEOUT"
	}
	return verdict
}

// pythonRepr renders a string the way Python's %r does, because this text is
// shown to the user and the Python core produced it with an f-string's !r.
//
// Four rules, all of them observable in the corpus this is compared against:
// single quotes normally; double quotes when the value contains a single quote and
// no double quote; an escaped single quote when it contains both; and backslashes
// and control characters escaped either way. The last two are why this is not a
// two-line function -- a model id with a backslash in it is unlikely, but so was
// every other divergence this migration found.
func pythonRepr(value string) string {
	quote := byte('\'')
	if strings.Contains(value, "'") && !strings.Contains(value, `"`) {
		quote = '"'
	}
	var out strings.Builder
	out.WriteByte(quote)
	for _, char := range value {
		switch {
		case char == rune(quote):
			out.WriteByte('\\')
			out.WriteRune(char)
		case char == '\\':
			out.WriteString(`\\`)
		case char == '\n':
			out.WriteString(`\n`)
		case char == '\r':
			out.WriteString(`\r`)
		case char == '\t':
			out.WriteString(`\t`)
		case char < 0x20 || char == 0x7f:
			out.WriteString(fmt.Sprintf(`\x%02x`, char))
		default:
			out.WriteRune(char)
		}
	}
	out.WriteByte(quote)
	return out.String()
}
