package convertproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicRequest(t *testing.T) {
	got, err := ToChat("anthropic", []byte(`{"model":"m","system":"s","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `"role":"system"`
	if !contains(string(got), want) {
		t.Fatalf("missing system: %s", got)
	}
}

func TestResponsesRequestMapsChatInputAndTools(t *testing.T) {
	got, err := ToChat("responses", []byte(`{"model":"client","input":"hello","tools":[{"type":"function","name":"lookup","description":"find","parameters":{"type":"object"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.Contains(body, `"role":"user"`) || !strings.Contains(body, `"name":"lookup"`) || !strings.Contains(body, `"parameters":{"type":"object"}`) {
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

func TestResponsesStreamEmitsCompletion(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer target.Close()
	proxy := &Server{client: target.Client(), cfg: Config{TargetBaseURL: target.URL}}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"m","stream":true,"input":[]}`))
	recorder := httptest.NewRecorder()
	proxy.handle(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "event: response.completed") || !strings.Contains(body, `"delta":"hello"`) {
		t.Fatalf("responses stream = %d %s", recorder.Code, body)
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
