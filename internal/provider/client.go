package provider

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const (
	defaultTimeout    = 10 * time.Second
	defaultMaxBody    = 1 << 20
	unsupportedStatus = 0
)

// HTTPDoer is deliberately smaller than *http.Client so tests and callers can
// provide a transport without opening a real network connection.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	doer    HTTPDoer
	timeout time.Duration
	maxBody int64
}

func NewClient(doer HTTPDoer) *Client {
	if doer == nil {
		doer = &http.Client{}
	}
	return &Client{doer: doer, timeout: defaultTimeout, maxBody: defaultMaxBody}
}

type ProbeResult struct {
	OK        bool    `json:"ok"`
	Reachable bool    `json:"reachable"`
	Status    int     `json:"status"`
	Message   string  `json:"message"`
	ErrorCode *string `json:"error_code"`
	Retryable bool    `json:"retryable"`
	Protocol  *string `json:"protocol"`
	// Detail is the provider's own explanation of a refused request, cleaned
	// and bounded. Empty on success and on transport failures.
	Detail string `json:"detail,omitempty"`
}

type ModelsResult struct {
	OK        bool     `json:"ok"`
	Reachable bool     `json:"reachable"`
	Status    int      `json:"status"`
	Message   string   `json:"message"`
	ErrorCode *string  `json:"error_code"`
	Retryable bool     `json:"retryable"`
	Protocol  *string  `json:"protocol"`
	Models    []string `json:"models"`
	// Entries carries what each listed model declares about itself, in the same
	// order as Models. Most callers only need the IDs; multimodal detection reads
	// the modality lists.
	Entries []ModelEntry `json:"entries,omitempty"`
}

// ModelEntry is one /models item. The modality lists are nil when the
// Provider did not send the field at all, and empty when it sent an empty
// list: only the second is a declaration.
type ModelEntry struct {
	ID               string   `json:"id"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// Declared reports whether the Provider said anything about this model's
// modalities.
func (e ModelEntry) Declared() bool {
	return e.InputModalities != nil || e.OutputModalities != nil
}

func (c *Client) Probe(ctx context.Context, protocol, providerID, apiKey, model, customBase string) (ProbeResult, error) {
	if apiKey == "" {
		return ProbeResult{}, oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	if protocol != ProtocolOpenAI && protocol != ProtocolAnthropic && protocol != ProtocolResponses {
		return ProbeResult{}, invalidProtocol(protocol)
	}
	requestModel := model
	if requestModel == "" {
		requestModel = FallbackProbeModel(providerID)
	}
	request, err := protocolRequest(ctx, protocol, providerID, customBase, apiKey, requestModel)
	if err != nil {
		return ProbeResult{}, err
	}
	return c.sendProbe(request, protocol, apiKey, requestModel, 0), nil
}

// probePNG is a 32x32 opaque grey PNG. Some vision endpoints refuse images
// smaller than 10 pixels a side, so the design's 8x8 would fail on them for a
// reason that says nothing about the model; 32x32 is still a few tokens.
const probePNG = "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAAAAABWESUoAAAAFklEQVR42mNoIAAYRhWMKhhVMFIVAABCAAAfN5c2AAAAAABJRU5ErkJggg=="

// ProbeVision sends one Chat Completions request carrying a small image, with
// max_tokens 1. It is a billed request, so only a user's click may send it.
// The answer is classified exactly like a protocol probe: a model that cannot
// take images is refused with a 4xx, which reads as "refused", not as a key or
// network problem.
func (c *Client) ProbeVision(ctx context.Context, apiKey, model, customBase string) (ProbeResult, error) {
	if apiKey == "" {
		return ProbeResult{}, oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	if strings.TrimSpace(model) == "" {
		return ProbeResult{}, oneerrors.New(oneerrors.InvalidRequest, "A model is required")
	}
	base, err := ProviderBase("custom", customBase)
	if err != nil {
		return ProbeResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
				{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + probePNG}},
			},
		}},
		"max_tokens": 1,
	})
	if err != nil {
		return ProbeResult{}, oneerrors.New(oneerrors.InternalError, "Unable to build Provider request", oneerrors.WithCause(err))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAIBaseURL(base)+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ProbeResult{}, oneerrors.New(oneerrors.InvalidRequest, "Provider endpoint is invalid", oneerrors.WithCause(err))
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	return c.sendProbe(request, ProtocolOpenAI, apiKey, model, 0), nil
}

// Probe timeouts for the generation and audio routes. A chat probe answers in
// seconds; producing one image can take a minute.
const (
	audioProbeTimeout = 60 * time.Second
	imageProbeTimeout = 180 * time.Second
)

// ProbeTranscription sends half a second of silent WAV to
// /audio/transcriptions. Billed by audio length, so it costs next to nothing,
// but it is still a billed request: only a user's click may send it.
func (c *Client) ProbeTranscription(ctx context.Context, apiKey, model, customBase string) (ProbeResult, error) {
	base, err := probeTarget(apiKey, model, customBase)
	if err != nil {
		return ProbeResult{}, err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("model", model); err != nil {
		return ProbeResult{}, probeBuildError(err)
	}
	part, err := form.CreateFormFile("file", "probe.wav")
	if err != nil {
		return ProbeResult{}, probeBuildError(err)
	}
	if _, err := part.Write(silentWAV()); err != nil {
		return ProbeResult{}, probeBuildError(err)
	}
	if err := form.Close(); err != nil {
		return ProbeResult{}, probeBuildError(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAIBaseURL(base)+"/audio/transcriptions", &body)
	if err != nil {
		return ProbeResult{}, oneerrors.New(oneerrors.InvalidRequest, "Provider endpoint is invalid", oneerrors.WithCause(err))
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", form.FormDataContentType())
	return c.sendProbe(request, ProtocolOpenAI, apiKey, model, audioProbeTimeout), nil
}

// ProbeSpeech asks /audio/speech to say "ok". The voice is OpenAI's default;
// a gateway whose model wants another voice refuses, and its message says so.
func (c *Client) ProbeSpeech(ctx context.Context, apiKey, model, customBase string) (ProbeResult, error) {
	return c.postJSONProbe(ctx, apiKey, model, customBase, "/audio/speech", map[string]any{
		"model": model, "input": "ok", "voice": "alloy",
	}, audioProbeTimeout)
}

// ProbeImageGeneration asks /images/generations for one image. That is a
// billed image, so the UI asks the user before calling it. No size is sent:
// the valid sizes differ by model, and a size the model rejects would fail the
// probe for a reason that says nothing about the endpoint.
func (c *Client) ProbeImageGeneration(ctx context.Context, apiKey, model, customBase string) (ProbeResult, error) {
	return c.postJSONProbe(ctx, apiKey, model, customBase, "/images/generations", map[string]any{
		"model": model, "prompt": "a plain grey square", "n": 1,
	}, imageProbeTimeout)
}

func (c *Client) postJSONProbe(ctx context.Context, apiKey, model, customBase, path string, body map[string]any, timeout time.Duration) (ProbeResult, error) {
	base, err := probeTarget(apiKey, model, customBase)
	if err != nil {
		return ProbeResult{}, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return ProbeResult{}, probeBuildError(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAIBaseURL(base)+path, bytes.NewReader(payload))
	if err != nil {
		return ProbeResult{}, oneerrors.New(oneerrors.InvalidRequest, "Provider endpoint is invalid", oneerrors.WithCause(err))
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	return c.sendProbe(request, ProtocolOpenAI, apiKey, model, timeout), nil
}

func probeTarget(apiKey, model, customBase string) (string, error) {
	if apiKey == "" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	if strings.TrimSpace(model) == "" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "A model is required")
	}
	return ProviderBase("custom", customBase)
}

func probeBuildError(err error) error {
	return oneerrors.New(oneerrors.InternalError, "Unable to build Provider request", oneerrors.WithCause(err))
}

// silentWAV is half a second of 16 kHz mono 16-bit silence.
func silentWAV() []byte {
	const rate, samples = 16000, 8000
	data := samples * 2
	out := make([]byte, 44+data)
	copy(out[0:], "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(36+data))
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], 1)
	binary.LittleEndian.PutUint32(out[24:], rate)
	binary.LittleEndian.PutUint32(out[28:], rate*2)
	binary.LittleEndian.PutUint16(out[32:], 2)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(data))
	return out
}

// sendProbe runs one probe request and classifies the answer. It never
// returns an error: every outcome, transport failures included, is a verdict.
// A zero timeout uses the client's own.
func (c *Client) sendProbe(request *http.Request, protocol, apiKey, requestModel string, timeout time.Duration) ProbeResult {
	response, cancelRequest, err := c.doWithin(request, timeout)
	defer cancelRequest()
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		result := transportResult(err)
		result.Protocol = new(protocol)
		return result
	}
	if response == nil {
		result := transportResult(errors.New("Provider transport returned no response"))
		result.Protocol = new(protocol)
		return result
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		// Keep the connection reusable without allowing a successful endpoint to
		// stream an unbounded response into the process.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, c.maxBody))
		return ProbeResult{
			// Every 2xx response proves that the endpoint accepted the protocol
			// request. Restricting this to 200/204 produced a contradictory result
			// for proxies returning 201/202: the message said "connection test
			// passed" while OK=false made the UI render a failure state.
			OK:        response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices,
			Reachable: true,
			Status:    response.StatusCode,
			Message:   fmt.Sprintf("%s connection test passed.", ProtocolLabel(protocol)),
			Retryable: false,
			Protocol:  new(protocol),
		}
	}
	body, _, _ := c.readBody(response.Body)
	result := classifyHTTPProbe(response.StatusCode, string(body), protocol, requestModel)
	result.Detail = upstreamDetail(body, apiKey)
	return result
}

// maxDetailRunes bounds the provider's own explanation carried back to the UI:
// enough for "model X does not exist or you do not have access", not a page.
const maxDetailRunes = 300

// upstreamDetail is the provider's explanation of a refused request, which is
// the part of a failure the user can act on. "HTTP 404" does not say whether
// the model, the key, or the endpoint was refused; the provider's message
// usually does.
//
// The key is scrubbed even though the body comes from the provider, because
// some gateways echo the request back into the error.
func upstreamDetail(body []byte, apiKey string) string {
	text := ""
	var parsed struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  string          `json:"detail"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		var nested struct {
			Message string `json:"message"`
		}
		var flat string
		switch {
		case json.Unmarshal(parsed.Error, &nested) == nil && nested.Message != "":
			text = nested.Message
		case json.Unmarshal(parsed.Error, &flat) == nil && flat != "":
			text = flat
		case parsed.Message != "":
			text = parsed.Message
		case parsed.Detail != "":
			text = parsed.Detail
		}
	}
	if text == "" && !bytes.HasPrefix(bytes.TrimSpace(body), []byte("<")) {
		// Plain text, but not an HTML error page, which says nothing useful.
		text = string(body)
	}
	if strings.TrimSpace(apiKey) != "" {
		text = strings.ReplaceAll(text, apiKey, "[redacted]")
	}
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > maxDetailRunes {
		text = string(runes[:maxDetailRunes]) + "…"
	}
	return text
}

func (c *Client) ListModels(ctx context.Context, providerID, apiKey, customBase string) (ModelsResult, error) {
	if apiKey == "" {
		return ModelsResult{}, oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	base, err := ProviderBase(providerID, customBase)
	if err != nil {
		return ModelsResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, OpenAIBaseURL(base)+"/models", nil)
	if err != nil {
		return ModelsResult{}, oneerrors.New(oneerrors.InvalidRequest, "Provider endpoint is invalid", oneerrors.WithCause(err))
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	response, cancelRequest, err := c.do(request)
	defer cancelRequest()
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		result := transportModelsResult(err)
		return result, nil
	}
	if response == nil {
		return transportModelsResult(errors.New("Provider transport returned no response")), nil
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return classifyHTTPModels(response.StatusCode), nil
	}
	body, tooLarge, readErr := c.readBody(response.Body)
	if readErr != nil {
		return transportModelsResult(readErr), nil
	}
	if tooLarge {
		return modelsFailure("Model list response is too large; enter model ID manually."), nil
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ModelsResult{
			Reachable: true,
			Status:    response.StatusCode,
			Message:   fmt.Sprintf("Model list response is not valid JSON: %v", err),
			ErrorCode: new(oneerrors.ModelsUnsupported),
			Models:    []string{},
		}, nil
	}
	entries := modelEntries(raw)
	models := make([]string, len(entries))
	for index, entry := range entries {
		models[index] = entry.ID
	}
	result := ModelsResult{Reachable: true, Status: response.StatusCode, Models: models, Entries: entries}
	if len(models) > 0 {
		result.OK = true
		result.Message = fmt.Sprintf("Found %d models.", len(models))
	} else {
		result.Message = "No model IDs returned; enter model ID manually."
		result.ErrorCode = new(oneerrors.ModelsUnsupported)
	}
	return result, nil
}

// ResolveProbeModel prefers a live chat-capable ID but keeps discovery failure
// non-fatal so callers can still use the provider fallback model.
func (c *Client) ResolveProbeModel(ctx context.Context, providerID, apiKey, model, customBase string) (string, error) {
	if model != "" || apiKey == "" {
		if model != "" {
			return model, nil
		}
		return FallbackProbeModel(providerID), nil
	}
	listing, err := c.ListModels(ctx, providerID, apiKey, customBase)
	if err != nil {
		return "", err
	}
	if listing.OK && len(listing.Models) > 0 {
		return PickChatModel(listing.Models), nil
	}
	return FallbackProbeModel(providerID), nil
}

func protocolRequest(ctx context.Context, protocol, providerID, customBase, apiKey, model string) (*http.Request, error) {
	var (
		endpoint string
		body     any
	)
	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}
	if protocol == ProtocolAnthropic {
		base, err := ProviderConfigBase(providerID, customBase, protocol)
		if err != nil {
			return nil, err
		}
		endpoint = AnthropicMessagesURL(base)
		headers["X-Api-Key"] = apiKey
		headers["Anthropic-Version"] = "2023-06-01"
		body = map[string]any{
			"model":      model,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
			"max_tokens": 1,
		}
	} else {
		base, err := ProviderBase(providerID, customBase)
		if err != nil {
			return nil, err
		}
		v1 := OpenAIBaseURL(base)
		if protocol == ProtocolResponses {
			endpoint = v1 + "/responses"
			body = map[string]any{"model": model, "input": "ping", "max_output_tokens": 16}
		} else {
			endpoint = v1 + "/chat/completions"
			body = map[string]any{
				"model":      model,
				"messages":   []map[string]string{{"role": "user", "content": "ping"}},
				"max_tokens": 1,
			}
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Unable to build Provider request", oneerrors.WithCause(err))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "Provider endpoint is invalid", oneerrors.WithCause(err))
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return request, nil
}

func (c *Client) do(request *http.Request) (*http.Response, context.CancelFunc, error) {
	return c.doWithin(request, 0)
}

func (c *Client) doWithin(request *http.Request, timeout time.Duration) (*http.Response, context.CancelFunc, error) {
	if timeout <= 0 {
		timeout = c.timeout
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	response, err := c.doer.Do(request.WithContext(ctx))
	return response, cancel, err
}

func (c *Client) readBody(reader io.Reader) ([]byte, bool, error) {
	limit := c.maxBody
	if limit <= 0 {
		limit = defaultMaxBody
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func classifyHTTPProbe(status int, body, protocol, model string) ProbeResult {
	label := ProtocolLabel(protocol)
	if unsupportedProtocol(status, body) {
		return ProbeResult{
			Reachable: true,
			Status:    status,
			Message:   fmt.Sprintf("Model %q does not support %s. Choose a model that serves this protocol.", model, label),
			ErrorCode: new(oneerrors.ProtocolUnsupported),
			Protocol:  new(protocol),
		}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ProbeResult{
			Reachable: true,
			Status:    status,
			Message:   fmt.Sprintf("API key was rejected (%d).", status),
			ErrorCode: new(oneerrors.APIKeyRejected),
			Retryable: true,
			Protocol:  new(protocol),
		}
	}
	return ProbeResult{
		Reachable: true,
		Status:    status,
		Message:   fmt.Sprintf("Endpoint returned HTTP %d.", status),
		ErrorCode: new(oneerrors.ProviderUnreachable),
		Retryable: status >= 500,
		Protocol:  new(protocol),
	}
}

func classifyHTTPModels(status int) ModelsResult {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ModelsResult{
			Reachable: true,
			Status:    status,
			Message:   fmt.Sprintf("API key was rejected (%d). Enter model ID manually.", status),
			ErrorCode: new(oneerrors.APIKeyRejected),
			Retryable: true,
			Models:    []string{},
		}
	}
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		return ModelsResult{
			Reachable: true,
			Status:    status,
			Message:   fmt.Sprintf("This endpoint does not expose /v1/models (%d); enter model ID manually.", status),
			ErrorCode: new(oneerrors.ModelsUnsupported),
			Models:    []string{},
		}
	}
	return ModelsResult{
		Reachable: true,
		Status:    status,
		Message:   fmt.Sprintf("Endpoint returned HTTP %d.", status),
		ErrorCode: new(oneerrors.ProviderUnreachable),
		Retryable: status >= 500,
		Models:    []string{},
	}
}

func transportResult(err error) ProbeResult {
	code := transportCode(err)
	return ProbeResult{
		Status:    unsupportedStatus,
		Message:   fmt.Sprintf("Cannot reach endpoint: %s", err),
		ErrorCode: &code,
		Retryable: true,
	}
}

func transportModelsResult(err error) ModelsResult {
	code := transportCode(err)
	return ModelsResult{
		Status:    unsupportedStatus,
		Message:   fmt.Sprintf("Cannot reach endpoint: %s", err),
		ErrorCode: &code,
		Retryable: true,
		Models:    []string{},
	}
}

func transportCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return oneerrors.Timeout
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return oneerrors.Timeout
	}
	if strings.Contains(strings.ToLower(err.Error()), "timed out") {
		return oneerrors.Timeout
	}
	return oneerrors.ProviderUnreachable
}

func modelsFailure(message string) ModelsResult {
	return ModelsResult{
		Reachable: true,
		Status:    http.StatusOK,
		Message:   message,
		ErrorCode: new(oneerrors.ModelsUnsupported),
		Models:    []string{},
	}
}

func modelEntries(raw any) []ModelEntry {
	data := raw
	if object, ok := raw.(map[string]any); ok {
		data = object["data"]
	}
	items, ok := data.([]any)
	if !ok {
		return []ModelEntry{}
	}
	entries := make([]ModelEntry, 0, len(items))
	for _, item := range items {
		switch value := item.(type) {
		case string:
			entries = append(entries, ModelEntry{ID: value})
		case map[string]any:
			if id, ok := value["id"]; ok && id != nil {
				entries = append(entries, ModelEntry{
					ID:               fmt.Sprint(id),
					InputModalities:  modalityList(value, "input_modalities"),
					OutputModalities: modalityList(value, "output_modalities"),
				})
			}
		}
	}
	return entries
}

// modalityList keeps the absent/empty distinction ModelEntry documents, and
// drops anything that is not a string: the listing is third-party data.
func modalityList(item map[string]any, key string) []string {
	raw, ok := item[key]
	if !ok || raw == nil {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	list := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			list = append(list, strings.ToLower(strings.TrimSpace(text)))
		}
	}
	return list
}

func unsupportedProtocol(status int, body string) bool {
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented {
		return true
	}
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity || status == http.StatusInternalServerError {
		lowered := strings.ToLower(body)
		for _, marker := range []string{"does not support endpoint", "not implemented", "unsupported endpoint", "unknown endpoint"} {
			if strings.Contains(lowered, marker) {
				return true
			}
		}
	}
	return false
}
