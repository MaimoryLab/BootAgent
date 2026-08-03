package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
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

// NewClientWithLimits is useful for integration tests and keeps operational
// limits explicit at the one place where requests are created.
func NewClientWithLimits(doer HTTPDoer, timeout time.Duration, maxBody int64) *Client {
	client := NewClient(doer)
	if timeout > 0 {
		client.timeout = timeout
	}
	if maxBody > 0 {
		client.maxBody = maxBody
	}
	return client
}

type ProbeResult struct {
	OK        bool    `json:"ok"`
	Reachable bool    `json:"reachable"`
	Status    int     `json:"status"`
	Message   string  `json:"message"`
	ErrorCode *string `json:"error_code"`
	Retryable bool    `json:"retryable"`
	Protocol  *string `json:"protocol"`
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
	response, cancelRequest, err := c.do(request)
	defer cancelRequest()
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		result := transportResult(err)
		result.Protocol = new(protocol)
		return result, nil
	}
	if response == nil {
		result := transportResult(errors.New("Provider transport returned no response"))
		result.Protocol = new(protocol)
		return result, nil
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		// Keep the connection reusable without allowing a successful endpoint to
		// stream an unbounded response into the process.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, c.maxBody))
		return ProbeResult{
			OK:        response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNoContent,
			Reachable: true,
			Status:    response.StatusCode,
			Message:   fmt.Sprintf("%s connection test passed.", ProtocolLabel(protocol)),
			Retryable: false,
			Protocol:  new(protocol),
		}, nil
	}
	body, _, _ := c.readBody(response.Body)
	return classifyHTTPProbe(response.StatusCode, string(body), protocol, requestModel), nil
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
	models := modelIDs(raw)
	result := ModelsResult{Reachable: true, Status: response.StatusCode, Models: models}
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
	timeout := c.timeout
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

func modelIDs(raw any) []string {
	data := raw
	if object, ok := raw.(map[string]any); ok {
		data = object["data"]
	}
	items, ok := data.([]any)
	if !ok {
		return []string{}
	}
	models := make([]string, 0, len(items))
	for _, item := range items {
		switch value := item.(type) {
		case string:
			models = append(models, value)
		case map[string]any:
			if id, ok := value["id"]; ok && id != nil {
				models = append(models, fmt.Sprint(id))
			}
		}
	}
	return models
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
