package app

import (
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/mcp"
)

func TestRemoveMCPAgentPrunesEmptyServers(t *testing.T) {
	r := mcp.Registry{Servers: map[string]mcp.ServerFact{
		"shared":     {Variants: []mcp.Variant{{Agents: []string{"codex", "claude"}, Spec: mcp.Spec{Type: "stdio", Command: "echo"}}}},
		"only-codex": {Variants: []mcp.Variant{{Agents: []string{"codex"}, Spec: mcp.Spec{Type: "stdio", Command: "echo"}}}},
	}}
	removeMCPAgent(&r, "codex")
	if _, ok := r.Servers["only-codex"]; ok {
		t.Fatal("server with no remaining agents was not pruned")
	}
	if got := r.Servers["shared"].Variants[0].Agents; len(got) != 1 || got[0] != "claude" {
		t.Fatalf("remaining agents = %#v", got)
	}
}

func TestMCPTargetAgentsIncludesDeselectedAssociations(t *testing.T) {
	eligible := map[string]catalog.Agent{"codex": {}, "claude": {}}
	fact := mcp.ServerFact{Variants: []mcp.Variant{{Agents: []string{"codex", "claude"}}}}
	got := mcpTargetAgents(fact, []string{"codex"}, eligible)
	if len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Fatalf("target agents = %#v", got)
	}
}
