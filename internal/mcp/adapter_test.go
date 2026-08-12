package mcp

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestJSONAdapterReadAndPatchPreservesJSONC(t *testing.T) {
	a := NewOpenCodeAdapter()
	path := t.TempDir() + "/opencode.jsonc"
	// The adapter reads from disk for discovery and patches the supplied current bytes for Apply.
	if _, err := a.Read(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	current := []byte("{\n  // keep this comment\n  \"name\": \"demo\",\n  \"mcp\": {\n    \"old\": { \"command\": \"old\" }, // trailing comma\n  },\n}\n")
	out, secret, err := a.Apply(context.Background(), path, current, map[string]*Spec{
		"new": {Command: "node", Args: []string{"server"}, Env: map[string]string{"TOKEN": "secret"}},
		"old": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !secret || !strings.Contains(string(out), "keep this comment") || strings.Contains(string(out), `"old"`) {
		t.Fatalf("unexpected patch result: secret=%v output=%s", secret, out)
	}
	observed, err := parseJSONBytes(out, "mcp")
	if err != nil || observed.Servers["new"].Spec.Command != "node" {
		t.Fatalf("patched output did not decode: %#v %v", observed, err)
	}
}

func TestStructuredAdaptersRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		adapter Adapter
		data    []byte
	}{
		{"claude", NewClaudeAdapter(), []byte(`{"settings":true,"mcpServers":{"x":{"command":"echo","args":["hi"]}}}`)},
		{"codex", NewCodexAdapter(), []byte("model = \"x\"\n[mcp_servers.x]\ncommand = \"echo\"\nargs = [\"hi\"]\n")},
		{"hermes", NewHermesAdapter(), []byte("name: demo\nmcp_servers:\n  x:\n    command: echo\n    args: [hi]\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/config"
			if err := writeTestFile(path, tc.data); err != nil {
				t.Fatal(err)
			}
			got, err := tc.adapter.Read(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if got.Servers["x"].Spec.Command != "echo" {
				t.Fatalf("unexpected observation: %#v", got)
			}
			out, _, err := tc.adapter.Apply(context.Background(), path, tc.data, map[string]*Spec{"x": {Command: "printf", Args: []string{"ok"}}})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), "printf") {
				t.Fatalf("patch missing command: %s", out)
			}
		})
	}
}

func parseJSONBytes(data []byte, section string) (Observed, error) {
	v, err := parseHUJSON(data)
	if err != nil {
		return Observed{}, err
	}
	return readJSONSection(v.Find("/" + section))
}

func parseHUJSON(data []byte) (hujson.Value, error) { return hujson.Parse(data) }

func writeTestFile(path string, data []byte) error { return os.WriteFile(path, data, 0o600) }
