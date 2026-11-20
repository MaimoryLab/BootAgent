package convertproxy

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAnthropicRequest(t *testing.T) {
	got, err := ToChat("anthropic", []byte(`{"model":"m","system":[{"type":"text","text":"s"}],"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"inspect"},{"type":"tool_use","id":"call_1","name":"lookup","input":{"q":"x"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"ok"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]},{"type":"document","source":{"type":"url","url":"https://example.test/a.pdf"}},{"type":"audio","source":{"type":"base64","media_type":"audio/wav","data":"xyz"}}]}],"tools":[{"name":"lookup","description":"find","input_schema":{"type":"object"}}],"tool_choice":{"type":"any"}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `"role":"system"`
	body := string(got)
	if !contains(body, want) || !contains(body, `"reasoning_content":"inspect"`) || !contains(body, `"tool_calls"`) || !contains(body, `"role":"tool"`) || !contains(body, `"image_url"`) || !contains(body, `"type":"file"`) || !contains(body, `"input_audio"`) || !contains(body, `"tool_choice":"required"`) {
		t.Fatalf("missing system: %s", got)
	}
}

func TestAnthropicResponsePreservesThinkingAndToolUse(t *testing.T) {
	got, err := FromChat("anthropic", []byte(`{"id":"chat","model":"m","choices":[{"finish_reason":"tool_calls","message":{"reasoning_content":"inspect","content":[{"type":"text","text":"answer"},{"type":"image_url","image_url":{"url":"https://example.test/out.png"}}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !contains(body, `"type":"thinking"`) || !contains(body, `"type":"tool_use"`) || !contains(body, `"type":"image"`) || !contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("anthropic response = %s", body)
	}
}

func TestResponsesRequestMapsChatInputAndTools(t *testing.T) {
	got, err := ToChat("responses", []byte(`{"model":"client","input":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]},{"type":"reasoning","summary":[{"type":"summary_text","text":"think later"}]},{"type":"function_call","id":"call_1","name":"lookup","arguments":"{}"},{"type":"input_image","image_url":{"url":"https://example.test/a.png"}},{"type":"input_file","file_url":"https://example.test/a.pdf","filename":"a.pdf"},{"type":"input_audio","input_audio":{"data":"xyz","format":"wav"}}],"tools":[{"type":"function","name":"lookup","description":"find","parameters":{"type":"object"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.Contains(body, `"role":"assistant"`) || !strings.Contains(body, `"reasoning_content":"think later"`) || !strings.Contains(body, `"id":"call_1"`) || !strings.Contains(body, `"url":"https://example.test/a.png"`) || !strings.Contains(body, `"type":"file"`) || !strings.Contains(body, `"input_audio"`) || !strings.Contains(body, `"name":"lookup"`) || !strings.Contains(body, `"parameters":{"type":"object"}`) {
		t.Fatalf("responses request = %s", body)
	}
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestModelsEndpoint(t *testing.T) {
	server := &Server{cfg: Config{APIKey: "local", Models: []string{"claude", "gpt"}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer local")
	recorder := httptest.NewRecorder()
	server.handle(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":"claude"`) {
		t.Fatalf("models response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestSetConfigReportsListenError(t *testing.T) {
	server := New(nil)
	if err := server.SetConfig(Config{Enabled: true, Listen: "[invalid"}); err == nil {
		t.Fatal("invalid listen address was accepted")
	}
}

func TestConcurrentSetConfigDoesNotLeakListeners(t *testing.T) {
	addresses := []string{reserveAddress(t), reserveAddress(t)}
	server := New(nil)

	var group sync.WaitGroup
	for _, address := range addresses {
		group.Add(1)
		go func(address string) {
			defer group.Done()
			if err := server.SetConfig(Config{Enabled: true, Listen: address}); err != nil {
				t.Errorf("SetConfig(%q): %v", address, err)
			}
		}(address)
	}
	group.Wait()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Errorf("listener %q was leaked: %v", address, err)
			continue
		}
		_ = listener.Close()
	}
}

func reserveAddress(t *testing.T) string {
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

func TestConvertedResponseDropsStaleBodyHeaders(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1")
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, `{"id":"chat","model":"m","choices":[]}`)
	}))
	defer target.Close()
	proxy := &Server{client: target.Client(), cfg: Config{TargetBaseURL: target.URL, TargetModel: "m"}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"client","input":[]}`))
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Length") != "" || recorder.Header().Get("Content-Encoding") != "" {
		t.Fatalf("converted headers = %v, status = %d", recorder.Header(), recorder.Code)
	}
}

func TestResponsesStreamEmitsCompletion(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer target.Close()
	proxy := &Server{client: target.Client(), cfg: Config{TargetBaseURL: target.URL}}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"m","stream":true,"input":[]}`))
	recorder := httptest.NewRecorder()
	proxy.handle(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "event: response.completed") || !strings.Contains(body, `"delta":"hello"`) || !strings.Contains(body, "event: response.reasoning_summary_text.delta") || !strings.Contains(body, "event: response.function_call_arguments.done") {
		t.Fatalf("responses stream = %d %s", recorder.Code, body)
	}
}

func TestAnthropicStreamEmitsAnthropicEvents(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer target.Close()
	proxy := &Server{client: target.Client(), cfg: Config{TargetBaseURL: target.URL}}
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"m","stream":true,"messages":[]}`))
	recorder := httptest.NewRecorder()
	proxy.handle(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "event: message_start") || !strings.Contains(body, "thinking_delta") || !strings.Contains(body, "tool_use") || !strings.Contains(body, "event: message_stop") {
		t.Fatalf("anthropic stream = %d %s", recorder.Code, body)
	}
}

func TestTargetModelOverridesClientModel(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		_ = json.Unmarshal(body, &request)
		if request["model"] != "provider-model" {
			t.Errorf("target model = %v", request["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chat","model":"provider-model","choices":[]}`)
	}))
	defer target.Close()
	proxy := &Server{client: target.Client(), cfg: Config{TargetBaseURL: target.URL, TargetModel: "provider-model"}}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"client-model","input":[]}`))
	recorder := httptest.NewRecorder()
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("response status = %d", recorder.Code)
	}
}

func TestChatCompletionsForwardsBodyAndTargetKey(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer target-key" {
			t.Errorf("target authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "provider-model" || request["max_tokens"] != float64(123) || request["reasoning_effort"] != "client-effort" {
			t.Errorf("forwarded request = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chat","choices":[]}`)
	}))
	defer target.Close()
	proxy := &Server{client: target.Client(), cfg: Config{TargetBaseURL: target.URL, TargetModel: "provider-model", TargetAPIKey: "target-key", TargetReasoningEffort: "profile-effort"}}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"client-model","max_tokens":123,"reasoning_effort":"client-effort","messages":[]}`))
	request.Header.Set("Authorization", "Bearer local-key")
	recorder := httptest.NewRecorder()
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("response status = %d", recorder.Code)
	}
}

func TestResponsesResponsePreservesReasoningAndToolCalls(t *testing.T) {
	got, err := FromChat("responses", []byte(`{"id":"chat","model":"upstream","choices":[{"finish_reason":"tool_calls","message":{"reasoning_content":"inspect first","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.Contains(body, `"type":"reasoning"`) || !strings.Contains(body, `"type":"function_call"`) || !strings.Contains(body, `"name":"lookup"`) {
		t.Fatalf("responses response = %s", body)
	}
}

func TestResolveRoutesEachLocalKeyToItsOwnTarget(t *testing.T) {
	var got []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Header.Get("Authorization")+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chat","choices":[]}`)
	}))
	defer upstream.Close()
	targets := map[string]Target{
		"key-a": {BaseURL: upstream.URL, Model: "model-a", APIKey: "upstream-a"},
		"key-b": {BaseURL: upstream.URL, Model: "model-b", APIKey: "upstream-b"},
	}
	proxy := &Server{client: upstream.Client(), cfg: Config{Resolve: func(key string) (Target, bool) {
		target, ok := targets[key]
		return target, ok
	}}}
	for _, test := range []struct{ header, value string }{{"Authorization", "Bearer key-a"}, {"x-api-key", "key-b"}} {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"alias","messages":[]}`))
		request.Header.Set(test.header, test.value)
		recorder := httptest.NewRecorder()
		proxy.handle(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d", test.value, recorder.Code)
		}
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "Bearer upstream-a ") || !strings.Contains(got[0], `"model-a"`) ||
		!strings.HasPrefix(got[1], "Bearer upstream-b ") || !strings.Contains(got[1], `"model-b"`) {
		t.Fatalf("upstream requests = %q", got)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer unknown")
	recorder := httptest.NewRecorder()
	proxy.handle(recorder, request)
	if recorder.Code != http.StatusUnauthorized || len(got) != 2 {
		t.Fatalf("unknown key status = %d, upstream requests = %d", recorder.Code, len(got))
	}
}

// heldUpstream answers a streaming request with one chunk and then keeps the
// response open until release is closed: an Agent in the middle of an answer.
func heldUpstream(t *testing.T) (upstream *httptest.Server, started chan struct{}, release chan struct{}) {
	t.Helper()
	started, release = make(chan struct{}, 1), make(chan struct{})
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
		w.(http.Flusher).Flush()
		started <- struct{}{}
		select {
		case <-release:
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		case <-r.Context().Done():
		}
	}))
	// Cut first: a test that fails with the response still held would otherwise
	// hang here instead of reporting.
	t.Cleanup(func() {
		upstream.CloseClientConnections()
		upstream.Close()
	})
	return upstream, started, release
}

// openStream starts a streaming request through the adapter and reports how it
// ended: the whole body, or the error that cut it short.
func openStream(address string) chan error {
	done := make(chan error, 1)
	go func() {
		response, err := http.Post("http://"+address+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"client","stream":true,"messages":[]}`))
		if err != nil {
			done <- err
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err == nil && !strings.Contains(string(body), "[DONE]") {
			err = io.ErrUnexpectedEOF
		}
		done <- err
	}()
	return done
}

// A new alias or a new route keeps the address, and must not wait on -- or
// cut -- an answer another Agent is still receiving.
func TestSetConfigKeepsAnOpenResponseWhenTheAddressIsUnchanged(t *testing.T) {
	upstream, started, release := heldUpstream(t)
	address := reserveAddress(t)
	server := New(nil)
	defer server.Close()
	config := Config{Enabled: true, Listen: address, TargetBaseURL: upstream.URL, Models: []string{"before"}}
	if err := server.SetConfig(config); err != nil {
		t.Fatal(err)
	}
	stream := openStream(address)
	<-started

	config.Models = []string{"after"}
	changed := make(chan error, 1)
	go func() { changed <- server.SetConfig(config) }()
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a change that keeps the address waited on an open response")
	}
	models, err := http.Get("http://" + address + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	listed, _ := io.ReadAll(models.Body)
	_ = models.Body.Close()
	if !strings.Contains(string(listed), `"after"`) {
		t.Fatalf("the new settings did not take effect: %s", listed)
	}
	close(release)
	if err := <-stream; err != nil {
		t.Fatalf("the open response was cut: %v", err)
	}
}

// Stopping is the user's decision. An answer still streaming gets a moment to
// finish and is then closed; it used to fail the stop and leave the listener
// gone with the settings still saying it ran.
func TestSetConfigStopsWhileAResponseIsStillOpen(t *testing.T) {
	grace := shutdownGrace
	shutdownGrace = 50 * time.Millisecond
	defer func() { shutdownGrace = grace }()
	upstream, started, _ := heldUpstream(t)
	address := reserveAddress(t)
	server := New(nil)
	defer server.Close()
	config := Config{Enabled: true, Listen: address, TargetBaseURL: upstream.URL}
	if err := server.SetConfig(config); err != nil {
		t.Fatal(err)
	}
	stream := openStream(address)
	<-started

	if err := server.SetConfig(Config{Enabled: false}); err != nil {
		t.Fatalf("stopping with an open response failed: %v", err)
	}
	select {
	case err := <-stream:
		if err == nil {
			t.Fatal("the response outlived the adapter it came through")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the open response was not closed")
	}
	if err := server.SetConfig(config); err != nil {
		t.Fatalf("the adapter could not start again on its address: %v", err)
	}
}
