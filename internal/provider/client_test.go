package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

type fakeDoer func(*http.Request) (*http.Response, error)

func (fake fakeDoer) Do(request *http.Request) (*http.Response, error) {
	return fake(request)
}

func fakeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

type contextAwareBody struct {
	ctx  context.Context
	data *strings.Reader
}

func (body *contextAwareBody) Read(target []byte) (int, error) {
	if err := body.ctx.Err(); err != nil {
		return 0, err
	}
	return body.data.Read(target)
}

func (body *contextAwareBody) Close() error { return nil }

func TestProbeBuildsProtocolSpecificRequests(t *testing.T) {
	tests := []struct {
		protocol string
		path     string
		bodyKey  string
	}{
		{ProtocolOpenAI, "/v1/chat/completions", "messages"},
		{ProtocolResponses, "/v1/responses", "input"},
		{ProtocolAnthropic, "/v1/messages", "messages"},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			var seen *http.Request
			client := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
				seen = request
				return fakeResponse(http.StatusOK, `{}`), nil
			}))
			result, err := client.Probe(context.Background(), test.protocol, "custom", "key", "model-a", "https://proxy.test/v1")
			if err != nil {
				t.Fatal(err)
			}
			if !result.OK || result.Protocol == nil || *result.Protocol != test.protocol {
				t.Fatalf("probe result = %#v", result)
			}
			if seen == nil || seen.URL.Path != test.path {
				t.Fatalf("request path = %v, want %s", seen.URL, test.path)
			}
			var body map[string]any
			if err := json.NewDecoder(seen.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, ok := body[test.bodyKey]; !ok {
				t.Fatalf("request body %#v does not contain %q", body, test.bodyKey)
			}
			if seen.Header.Get("Authorization") != "Bearer key" {
				t.Fatal("Authorization header was not set")
			}
			if test.protocol == ProtocolAnthropic {
				if seen.Header.Get("X-Api-Key") != "key" || seen.Header.Get("Anthropic-Version") != "2023-06-01" {
					t.Fatal("Anthropic headers were not set")
				}
			} else if seen.Header.Get("X-Api-Key") != "" {
				t.Fatal("OpenAI request unexpectedly sent X-Api-Key")
			}
		})
	}
}

func TestProbeClassifiesUnsupportedAndTransientResponses(t *testing.T) {
	unsupported := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
		return fakeResponse(http.StatusBadRequest, `{"message":"model does not support endpoint"}`), nil
	}))
	result, err := unsupported.Probe(context.Background(), ProtocolResponses, "custom", "key", "model-a", "https://proxy.test/v1")
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode == nil || *result.ErrorCode != oneerrors.ProtocolUnsupported || result.Retryable || result.Reachable != true {
		t.Fatalf("unsupported result = %#v", result)
	}

	transient := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
		return fakeResponse(http.StatusServiceUnavailable, `{"error":"busy"}`), nil
	}))
	result, err = transient.Probe(context.Background(), ProtocolResponses, "custom", "key", "model-a", "https://proxy.test/v1")
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode == nil || *result.ErrorCode == oneerrors.ProtocolUnsupported || !result.Retryable || result.Protocol == nil {
		t.Fatalf("transient result = %#v", result)
	}
}

func TestProbeTransportAndInputErrors(t *testing.T) {
	client := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	}))
	result, err := client.Probe(context.Background(), ProtocolOpenAI, "ppio", "key", "model", "")
	if err != nil || result.ErrorCode == nil || *result.ErrorCode != oneerrors.ProviderUnreachable || result.Reachable || !result.Retryable {
		t.Fatalf("transport result = %#v, err=%v", result, err)
	}
	if _, err := client.Probe(context.Background(), ProtocolOpenAI, "ppio", "", "model", ""); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("missing key error = %v", err)
	}
	if _, err := client.Probe(context.Background(), "grpc", "ppio", "key", "model", ""); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("unknown protocol error = %v", err)
	}
}

func TestListModelsShapesAndErrors(t *testing.T) {
	responses := []string{
		`["model-a", {"id":"model-b"}, {"ignored":true}]`,
		`{"data":[{"id":"model-c"}]}`,
	}
	for _, body := range responses {
		client := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
			return fakeResponse(http.StatusOK, body), nil
		}))
		result, err := client.ListModels(context.Background(), "ppio", "key", "")
		if err != nil || !result.OK || !reflect.DeepEqual(result.Models, func() []string {
			if strings.Contains(body, "model-c") {
				return []string{"model-c"}
			}
			return []string{"model-a", "model-b"}
		}()) {
			t.Fatalf("ListModels(%s) = %#v, err=%v", body, result, err)
		}
	}
	invalid := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
		return fakeResponse(http.StatusOK, "not-json"), nil
	}))
	result, err := invalid.ListModels(context.Background(), "ppio", "key", "")
	if err != nil || result.ErrorCode == nil || *result.ErrorCode != oneerrors.ModelsUnsupported || len(result.Models) != 0 {
		t.Fatalf("invalid JSON result = %#v, err=%v", result, err)
	}
	missing := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/openai/v1/models" {
			t.Errorf("model URL path = %s", request.URL.Path)
		}
		return fakeResponse(http.StatusNotFound, ""), nil
	}))
	result, err = missing.ListModels(context.Background(), "ppio", "key", "")
	if err != nil || result.ErrorCode == nil || *result.ErrorCode != oneerrors.ModelsUnsupported || result.Retryable || len(result.Models) != 0 {
		t.Fatalf("missing model endpoint result = %#v, err=%v", result, err)
	}
}

func TestListModelsTransportTimeoutAndSizeLimit(t *testing.T) {
	timeoutClient := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: request.URL.String(), Err: context.DeadlineExceeded}
	}))
	result, err := timeoutClient.ListModels(context.Background(), "ppio", "key", "")
	if err != nil || result.ErrorCode == nil || *result.ErrorCode != oneerrors.Timeout {
		t.Fatalf("timeout result = %#v, err=%v", result, err)
	}
	large := NewClientWithLimits(fakeDoer(func(*http.Request) (*http.Response, error) {
		return fakeResponse(http.StatusOK, strings.Repeat("x", 32)), nil
	}), time.Second, 8)
	result, err = large.ListModels(context.Background(), "ppio", "key", "")
	if err != nil || result.ErrorCode == nil || *result.ErrorCode != oneerrors.ModelsUnsupported {
		t.Fatalf("large response result = %#v, err=%v", result, err)
	}
}

func TestClientKeepsRequestContextAliveWhileReadingBody(t *testing.T) {
	client := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: &contextAwareBody{
				ctx:  request.Context(),
				data: strings.NewReader(`{"data":[{"id":"chat-model"}]}`),
			},
			Header: make(http.Header),
		}, nil
	}))
	result, err := client.ListModels(context.Background(), "ppio", "key", "")
	if err != nil || !result.OK || !reflect.DeepEqual(result.Models, []string{"chat-model"}) {
		t.Fatalf("context-aware body result = %#v, err=%v", result, err)
	}
}

func TestResolveProbeModelUsesDiscoveryOnlyWhenNeeded(t *testing.T) {
	called := false
	client := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
		called = true
		return fakeResponse(http.StatusOK, `{"data":[{"id":"text-embedding-3-small"},{"id":"chat-model"} ]}`), nil
	}))
	model, err := client.ResolveProbeModel(context.Background(), "ppio", "key", "", "")
	if err != nil || model != "chat-model" || !called {
		t.Fatalf("discovered model = %q, err=%v, called=%v", model, err, called)
	}
	called = false
	model, err = client.ResolveProbeModel(context.Background(), "ppio", "", "", "")
	if err != nil || model != FallbackProbeModel("ppio") || called {
		t.Fatalf("fallback model = %q, err=%v, called=%v", model, err, called)
	}
	model, err = client.ResolveProbeModel(context.Background(), "ppio", "key", "chosen", "")
	if err != nil || model != "chosen" || called {
		t.Fatalf("chosen model = %q, err=%v, called=%v", model, err, called)
	}
}

func TestClientPropagatesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	}))
	result, err := client.ListModels(ctx, "ppio", "key", "")
	if err != nil || result.ErrorCode == nil || *result.ErrorCode != oneerrors.Timeout {
		t.Fatalf("cancelled result = %#v, err=%v", result, err)
	}
}

type failingBody struct{ err error }

func (body failingBody) Read([]byte) (int, error) { return 0, body.err }
func (failingBody) Close() error                  { return nil }

func TestListModelsMapsBodyReadCancellationToTimeout(t *testing.T) {
	client := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       failingBody{err: context.Canceled},
			Header:     make(http.Header),
		}, nil
	}))
	result, err := client.ListModels(context.Background(), "ppio", "key", "")
	if err != nil || result.ErrorCode == nil || *result.ErrorCode != oneerrors.Timeout || result.Reachable {
		t.Fatalf("body cancellation result = %#v, err=%v", result, err)
	}
}
