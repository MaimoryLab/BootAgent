package mcp

import (
	"context"
	"encoding/json"
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

func TestCodexUsesHTTPHeadersNativeKey(t *testing.T) {
	a := NewCodexAdapter()
	current := []byte("[mcp_servers]\n[mcp_servers.x]\nurl = \"https://example.test/mcp\"\n")
	out, _, err := a.Apply(context.Background(), "config.toml", current, map[string]*Spec{"x": {Type: "http", URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "Bearer key"}}})
	if err != nil || strings.Contains(string(out), "headers =") || !strings.Contains(string(out), "http_headers") {
		t.Fatalf("unexpected codex output: %s (%v)", out, err)
	}
	path := t.TempDir() + "/config.toml"
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := a.Read(context.Background(), path)
	if err != nil || observed.Servers["x"].Spec.Headers["Authorization"] != "Bearer key" {
		t.Fatalf("unexpected decoded headers: %#v (%v)", observed, err)
	}
}

func TestCodexPreservesCommentsAndUnmanagedMCPKeys(t *testing.T) {
	a := NewCodexAdapter()
	current := []byte("# keep this comment\nmodel = \"gpt-5\"\n\n[mcp_servers.x]\ncommand = \"echo\"\nstartup_timeout_sec = 45\n")
	out, _, err := a.Apply(context.Background(), "config.toml", current, map[string]*Spec{"x": {Type: "stdio", Command: "printf"}})
	if err != nil || !strings.Contains(string(out), "# keep this comment") || !strings.Contains(string(out), "startup_timeout_sec = 45") || !strings.Contains(string(out), `model = "gpt-5"`) {
		t.Fatalf("Codex TOML lost user content: %s (%v)", out, err)
	}
}

func TestOpenCodeUsesNativeMCPShape(t *testing.T) {
	a := NewOpenCodeAdapter()
	current := []byte(`{"mcp":{}}`)
	out, _, err := a.Apply(context.Background(), "opencode.json", current, map[string]*Spec{
		"codegraph": {Type: "stdio", Command: "codegraph", Args: []string{"serve", "--mcp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		MCP map[string]map[string]any `json:"mcp"`
	}
	if err := json.Unmarshal(out, &document); err != nil {
		t.Fatal(err)
	}
	native := document.MCP["codegraph"]
	if native["type"] != "local" || native["enabled"] != true {
		t.Fatalf("invalid OpenCode native shape: %s", out)
	}
	path := t.TempDir() + "/opencode.json"
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := a.Read(context.Background(), path)
	if err != nil || observed.Servers["codegraph"].Spec.Command != "codegraph" || len(observed.Servers["codegraph"].Spec.Args) != 2 {
		t.Fatalf("OpenCode decode: %#v %v", observed, err)
	}
}

func TestOpenCodePreservesEnvironment(t *testing.T) {
	a := NewOpenCodeAdapter()
	out, _, err := a.Apply(context.Background(), "opencode.json", []byte(`{"mcp":{}}`), map[string]*Spec{"x": {Type: "stdio", Command: "server", Env: map[string]string{"TOKEN": "secret"}}})
	if err != nil || !strings.Contains(string(out), "environment") || !strings.Contains(string(out), "TOKEN") {
		t.Fatalf("OpenCode environment missing: %s (%v)", out, err)
	}
}

func parseJSONBytes(data []byte, section string) (Observed, error) {
	v, err := parseHUJSON(data)
	if err != nil {
		return Observed{}, err
	}
	if section == "mcp" {
		return readJSONSection(v.Find("/"+section), decodeOpenCode)
	}
	return readJSONSection(v.Find("/"+section), nil)
}

func parseHUJSON(data []byte) (hujson.Value, error) { return hujson.Parse(data) }

func writeTestFile(path string, data []byte) error { return os.WriteFile(path, data, 0o600) }
