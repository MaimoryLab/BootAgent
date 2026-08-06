package profile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentBindingWriteReadAndPreserveProfileReference(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	binding, err := store.WriteAgentBinding(context.Background(), "codex", BindingWriteRequest{
		Provider:   "ppio",
		BaseURL:    "https://api.ppio.com/responses",
		Model:      "model-a",
		ProfileRef: "team",
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.AgentID != "codex" || binding.ProfileRef != "team" {
		t.Fatalf("binding = %#v", binding)
	}
	updated, err := store.WriteAgentBinding(context.Background(), "codex", BindingWriteRequest{
		Provider: "novita",
		BaseURL:  "https://api.novita.ai/openai",
		Model:    "model-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ProfileRef != "team" || updated.CreatedAt != binding.CreatedAt || updated.UpdatedAt == "" {
		t.Fatalf("updated binding = %#v", updated)
	}
	read, err := store.ReadAgentBinding("codex")
	if err != nil || read == nil || read.Provider != "novita" {
		t.Fatalf("ReadAgentBinding() = %#v, %v", read, err)
	}
	wire, err := json.Marshal(read)
	if err != nil || strings.Contains(string(wire), "api_key") || strings.Contains(string(wire), "secret") {
		t.Fatalf("binding projection leaked unknown fields: %s (%v)", wire, err)
	}
	assertProfileMode(t, func() string { path, _ := store.AgentBindingPath("codex"); return path }(), 0o600)
}

func TestAgentBindingListRejectsCorruptFiles(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	if err := os.MkdirAll(store.AgentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		"codex.json":    "{",
		"claude.json":   `{"schema_version":9,"agent_id":"claude"}`,
		"Bad-Name.json": `{"schema_version":1,"agent_id":"Bad-Name"}`,
		"openai.json":   `{"schema_version":1,"agent_id":"other","provider":"ppio"}`,
	}
	for name, content := range fixtures {
		if err := os.WriteFile(filepath.Join(store.AgentsPath(), name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.WriteAgentBinding(context.Background(), "opencode", BindingWriteRequest{Provider: "ppio", BaseURL: "https://api.ppio.com/openai", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListAgentBindings(); err == nil {
		t.Fatal("corrupt Agent binding was ignored")
	}
	if _, err := store.ReadAgentBinding("../escape"); err == nil {
		t.Fatal("traversal Agent ID unexpectedly accepted")
	}
}

func TestAgentBindingWriteDoesNotOverwriteCorruptFile(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	path, _ := store.AgentBindingPath("codex")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteAgentBinding(context.Background(), "codex", BindingWriteRequest{Provider: "p", BaseURL: "x", Model: "m"}); err == nil {
		t.Fatal("write overwrote corrupt Agent binding")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "{" {
		t.Fatalf("binding changed to %q, err=%v", data, err)
	}
}

func TestAgentBindingRejectsInvalidInputAndHonorsCancellation(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	for _, request := range []BindingWriteRequest{{Provider: "", BaseURL: "x", Model: "m"}, {Provider: "p", BaseURL: "", Model: "m"}, {Provider: "p", BaseURL: "x", Model: ""}} {
		if _, err := store.WriteAgentBinding(context.Background(), "codex", request); err == nil {
			t.Errorf("invalid binding %#v succeeded", request)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.WriteAgentBinding(ctx, "codex", BindingWriteRequest{Provider: "p", BaseURL: "x", Model: "m"}); err == nil {
		t.Fatal("cancelled binding write succeeded")
	}
	if got, err := store.ListAgentBindings(); err != nil || len(got) != 0 {
		t.Fatalf("cancelled write changed bindings = %#v, %v", got, err)
	}
}
