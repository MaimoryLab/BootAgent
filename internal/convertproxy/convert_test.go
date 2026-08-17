package convertproxy

import "testing"

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
