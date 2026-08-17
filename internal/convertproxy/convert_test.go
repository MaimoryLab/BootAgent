package convertproxy

import (
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
