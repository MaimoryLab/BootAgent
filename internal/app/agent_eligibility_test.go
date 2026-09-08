package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/platform"
)

func TestSafeUserPathRequiresStrictDescendant(t *testing.T) {
	home := t.TempDir()
	for _, test := range []struct {
		path string
		want bool
	}{
		{home, false},
		{filepath.Dir(home), false},
		{filepath.Join(home, "..", "outside"), false},
		{filepath.Join(home, "x"), true},
		{filepath.Join(home, ".claude.json"), true},
		{"", false},
	} {
		if got := safeUserPath(home, test.path); got != test.want {
			t.Errorf("safeUserPath(%q, %q) = %v, want %v", home, test.path, got, test.want)
		}
	}
	if safeUserPath("", filepath.Join(home, "x")) {
		t.Fatal("empty home was accepted")
	}
}

func TestEligibleMCPAllowsConfigDirectlyInHome(t *testing.T) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	core := NewUseCases(StatusOptions{
		Home: t.TempDir(), Platform: platform.For("linux", "amd64"),
		Lookup: func(string) (string, bool) { return "/fake/claude", true },
	})
	agent, ok := manifest.Agents["claude-code"]
	if !ok {
		t.Fatal("Claude Code is missing from the catalog")
	}
	if !core.eligibleMCPAgent(context.Background(), "claude-code", agent) {
		t.Fatal("installed Claude was excluded because its MCP config is directly in home")
	}
}

func TestAgentEligibilityRejectsSymlinkedPaths(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	core := NewUseCases(StatusOptions{
		Home: home, Platform: platform.For("linux", "amd64"),
		Lookup: func(string) (string, bool) { return "/fake/codex", true },
	})
	agent := manifest.Agents["codex"]
	agent.MCPConfigPath = "linked/missing/config.toml"
	agent.SkillsPath = "linked/missing/skills"
	if core.eligibleMCPAgent(context.Background(), "codex", agent) {
		t.Error("MCP accepted a missing parent below a symlink")
	}
	if core.eligibleSkillAgent(context.Background(), "codex", agent) {
		t.Error("Skills accepted a missing root below a symlink")
	}
}
