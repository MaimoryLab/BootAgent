package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
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

func TestProbeTreatsEverySuccessfulHTTPStatusAsPassed(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent, http.StatusPartialContent, 299} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
				return fakeResponse(status, `{}`), nil
			}))
			result, err := client.Probe(context.Background(), ProtocolAnthropic, "custom", "key", "model-a", "https://proxy.test/v1")
			if err != nil {
				t.Fatal(err)
			}
			if !result.OK {
				t.Fatalf("HTTP %d was reported as failed: %#v", status, result)
			}
			if !strings.Contains(result.Message, "connection test passed") {
				t.Fatalf("HTTP %d message = %q, want passed message", status, result.Message)
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
	large := &Client{doer: fakeDoer(func(*http.Request) (*http.Response, error) {
		return fakeResponse(http.StatusOK, strings.Repeat("x", 32)), nil
	}), timeout: time.Second, maxBody: 8}
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

// The provider's explanation is what makes a refused probe actionable, but it is
// untrusted text bound for the UI: it is extracted from every error shape
// gateways use, an HTML error page is dropped as saying nothing, control
// characters cannot reach the screen, the key is scrubbed, and length is bounded.
func TestUpstreamDetailExtractsCleansAndBounds(t *testing.T) {
	long := strings.Repeat("x", maxDetailRunes+50)
	for _, test := range []struct {
		name, body, want string
	}{
		{"anthropic nested", `{"type":"error","error":{"type":"not_found_error","message":"model: m does not exist"}}`, "model: m does not exist"},
		{"flat string error", `{"error":"model not found"}`, "model not found"},
		{"top-level message", `{"message":"quota exceeded"}`, "quota exceeded"},
		{"fastapi detail", `{"detail":"Not Found"}`, "Not Found"},
		{"plain text", "upstream says no\n", "upstream says no"},
		{"html page", "<html><body>502 Bad Gateway</body></html>", ""},
		{"control characters", "{\"error\":{\"message\":\"bad\\u001b[31m model\\u0007\"}}", "bad [31m model"},
		{"key echoed back", `{"error":{"message":"key sk-live-123 is not allowed"}}`, "key [redacted] is not allowed"},
		{"bounded", `{"error":{"message":"` + long + `"}}`, strings.Repeat("x", maxDetailRunes) + "…"},
		{"empty", ``, ""},
	} {
		if got := upstreamDetail([]byte(test.body), "sk-live-123"); got != test.want {
			t.Errorf("%s: upstreamDetail = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestListModelsKeepsDeclaredModalities(t *testing.T) {
	client := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
		return fakeResponse(http.StatusOK, `{"data":[
			{"id":"vision-a","input_modalities":["text","Image"],"output_modalities":["text"]},
			{"id":"silent","input_modalities":[],"output_modalities":null},
			{"id":"bare"},
			{"id":"odd","input_modalities":"image"},
			"plain"
		]}`), nil
	}))
	result, err := client.ListModels(context.Background(), "custom", "key", "https://proxy.test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Models, []string{"vision-a", "silent", "bare", "odd", "plain"}) {
		t.Fatalf("models = %v", result.Models)
	}
	if len(result.Entries) != len(result.Models) {
		t.Fatalf("entries = %#v", result.Entries)
	}
	if got := result.Entries[0]; !got.Declared() || !reflect.DeepEqual(got.InputModalities, []string{"text", "image"}) {
		t.Fatalf("declared entry = %#v", got)
	}
	// An explicitly empty list is a declaration of "nothing"; an absent field
	// says nothing at all, and neither does a value of the wrong shape.
	if !result.Entries[1].Declared() {
		t.Fatalf("empty list should count as declared: %#v", result.Entries[1])
	}
	for _, index := range []int{2, 3, 4} {
		if result.Entries[index].Declared() {
			t.Fatalf("entry %d should not be declared: %#v", index, result.Entries[index])
		}
	}
}

func TestProbeVisionSendsOneImageWithOneToken(t *testing.T) {
	var seen *http.Request
	var body map[string]any
	client := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
		seen = request
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return fakeResponse(http.StatusOK, `{}`), nil
	}))
	result, err := client.ProbeVision(context.Background(), "key", "vision-a", "https://proxy.test/openai")
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || seen.URL.String() != "https://proxy.test/openai/v1/chat/completions" {
		t.Fatalf("result = %#v, url = %v", result, seen.URL)
	}
	if body["max_tokens"] != float64(1) || body["model"] != "vision-a" {
		t.Fatalf("body = %#v", body)
	}
	encoded, _ := json.Marshal(body["messages"])
	if !strings.Contains(string(encoded), `"type":"image_url"`) || !strings.Contains(string(encoded), "data:image/png;base64,") {
		t.Fatalf("messages = %s", encoded)
	}
}

func TestProbeVisionClassifiesRefusalAndScrubsKey(t *testing.T) {
	client := NewClient(fakeDoer(func(*http.Request) (*http.Response, error) {
		return fakeResponse(http.StatusBadRequest, `{"error":{"message":"model text-only does not support image input (key sk-secret)"}}`), nil
	}))
	result, err := client.ProbeVision(context.Background(), "sk-secret", "text-only", "https://proxy.test")
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !result.Reachable || result.Status != http.StatusBadRequest {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(result.Detail, "sk-secret") || !strings.Contains(result.Detail, "does not support image input") {
		t.Fatalf("detail = %q", result.Detail)
	}
	if _, err := client.ProbeVision(context.Background(), "", "m", "https://proxy.test"); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := client.ProbeVision(context.Background(), "k", " ", "https://proxy.test"); err == nil {
		t.Fatal("missing model accepted")
	}
}

func TestProbePNGDecodes(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(probePNG)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width < 10 || config.Height < 10 {
		t.Fatalf("probe image is %dx%d; some endpoints refuse images under 10px a side", config.Width, config.Height)
	}
}

func TestProbeTranscriptionSendsSilentWAV(t *testing.T) {
	var seen *http.Request
	var model string
	var audio []byte
	client := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
		seen = request
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		model = request.FormValue("model")
		file, _, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		audio, _ = io.ReadAll(file)
		return fakeResponse(http.StatusOK, `{"text":""}`), nil
	}))
	result, err := client.ProbeTranscription(context.Background(), "key", "whisper-1", "https://proxy.test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || seen.URL.Path != "/v1/audio/transcriptions" || model != "whisper-1" {
		t.Fatalf("result = %#v, path = %s, model = %q", result, seen.URL.Path, model)
	}
	if len(audio) != 44+16000 || string(audio[0:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		t.Fatalf("probe audio is not half a second of 16 kHz WAV: %d bytes", len(audio))
	}
}

func TestProbeSpeechAndImagesSendMinimalBodies(t *testing.T) {
	bodies := map[string]map[string]any{}
	client := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies[request.URL.Path] = body
		return fakeResponse(http.StatusOK, `{}`), nil
	}))
	if result, err := client.ProbeSpeech(context.Background(), "key", "tts-1", "https://proxy.test"); err != nil || !result.OK {
		t.Fatalf("speech = %#v, %v", result, err)
	}
	if result, err := client.ProbeImageGeneration(context.Background(), "key", "gpt-image-1", "https://proxy.test"); err != nil || !result.OK {
		t.Fatalf("image = %#v, %v", result, err)
	}
	speech := bodies["/v1/audio/speech"]
	if speech["input"] != "ok" || speech["model"] != "tts-1" || speech["voice"] == nil {
		t.Fatalf("speech body = %#v", speech)
	}
	image := bodies["/v1/images/generations"]
	if image["n"] != float64(1) || image["model"] != "gpt-image-1" {
		t.Fatalf("image body = %#v", image)
	}
	// Valid sizes differ by model; sending one could fail the probe for a
	// reason unrelated to the endpoint.
	if _, ok := image["size"]; ok {
		t.Fatalf("image probe sent a size: %#v", image)
	}
	for _, probe := range []func(context.Context, string, string, string) (ProbeResult, error){client.ProbeSpeech, client.ProbeImageGeneration, client.ProbeTranscription} {
		if _, err := probe(context.Background(), "", "m", "https://proxy.test"); err == nil {
			t.Fatal("probe without a key accepted")
		}
		if _, err := probe(context.Background(), "k", "", "https://proxy.test"); err == nil {
			t.Fatal("probe without a model accepted")
		}
	}
}

func TestSlowProbesOutlastTheChatTimeout(t *testing.T) {
	client := NewClient(fakeDoer(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) < 2*defaultTimeout {
			t.Errorf("%s deadline = %v away, want well over the %v chat timeout", request.URL.Path, time.Until(deadline), defaultTimeout)
		}
		return fakeResponse(http.StatusOK, `{}`), nil
	}))
	_, _ = client.ProbeImageGeneration(context.Background(), "k", "m", "https://proxy.test")
	_, _ = client.ProbeSpeech(context.Background(), "k", "m", "https://proxy.test")
}
