package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/mcp"
	"github.com/MaimoryLab/BootAgent/internal/platform"
)

func TestEligibleMCPAllowsMissingUserConfigParent(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	core := NewUseCases(StatusOptions{
		Home: home, Platform: platform.Info{OS: "darwin", Arch: "arm64"},
		Environment: map[string]string{"PATH": bin},
		Lookup:      func(string) (string, bool) { return "", false },
	})
	eligible, err := core.eligibleMCPAgents()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eligible["codex"]; !ok {
		t.Fatalf("codex was excluded before its config directory exists: %#v", eligible)
	}
}

func TestRemoveMCPAgentPrunesEmptyServers(t *testing.T) {
	r := mcp.Registry{Servers: map[string]mcp.ServerFact{
		"shared":     {Variants: []mcp.Variant{{Agents: []string{"codex", "claude"}, Spec: mcp.Spec{Type: "stdio", Command: "echo"}}}},
		"only-codex": {Variants: []mcp.Variant{{Agents: []string{"codex"}, Spec: mcp.Spec{Type: "stdio", Command: "echo"}}}},
	}}
	removeMCPAgent(&r, "codex")
	if len(r.Servers["only-codex"].Variants) != 1 || len(r.Servers["only-codex"].Variants[0].Agents) != 0 {
		t.Fatal("server with no remaining agents was not retained as a draft")
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

func TestCollapseEmptyMCPVariantsKeepsOneDraft(t *testing.T) {
	fact := mcp.ServerFact{Variants: []mcp.Variant{
		{Spec: mcp.Spec{Type: "http", URL: "https://example.test"}},
		{Spec: mcp.Spec{Type: "sse", URL: "https://example.test"}},
	}}
	collapseEmptyMCPVariants(&fact)
	if len(fact.Variants) != 1 || fact.Variants[0].Spec.Type != "http" {
		t.Fatalf("empty variants = %#v", fact.Variants)
	}
}
