package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/skill"
)

func TestScanSkillsDoesNotCreateAgentRootAndReportsUnmanagedCandidate(t *testing.T) {
	home := t.TempDir()
	core := skillTestCore(home)
	if _, err := core.ScanSkills(context.Background()); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(home, ".claude", "skills")
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("scan created missing root %q", missing)
	}

	root := filepath.Join(home, ".codex", "skills")
	writeTestSkill(t, root, "review", "first")
	result, err := core.ScanSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].ID != "review" || result.Candidates[0].Stored {
		t.Fatalf("candidates = %#v", result.Candidates)
	}
	registry, err := core.skillStore().Load()
	if err != nil {
		t.Fatal(err)
	}
	variant := registry.Skills["review"].Variants[0]
	if variant.Stored || !slices.Equal(variant.ObservedAgents, []string{"codex"}) {
		t.Fatalf("variant = %#v", variant)
	}
	if _, err := os.Stat(core.skillStore().VariantPath("review", variant.Hash)); !os.IsNotExist(err) {
		t.Fatalf("scan imported unmanaged content: %v", err)
	}
}

func TestPreviewFolderAndApplySelectedProjection(t *testing.T) {
	home := t.TempDir()
	core := skillTestCore(home)
	for _, root := range []string{filepath.Join(home, ".codex", "skills"), filepath.Join(home, ".claude", "skills")} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(t.TempDir(), "review")
	writeTestSkillAt(t, source, "first")
	preview, err := core.PreviewSkillImport(context.Background(), SkillImportRequest{Source: "folder"}, source)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Token == "" || len(preview.Candidates) != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	result := core.ApplySkills(context.Background(), SkillApplyRequest{PreviewToken: preview.Token, Changes: []SkillChange{{
		ID: "review", VariantHash: preview.Candidates[0].Hash, Targets: []string{"codex"}, ImportSource: "folder",
	}}})
	if len(result.Results) != 1 || result.Results[0].Error != "" || !result.Results[0].TargetUpdated || !result.Results[0].RegistryUpdated {
		t.Fatalf("apply = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "review", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "review")); !os.IsNotExist(err) {
		t.Fatalf("unselected target exists: %v", err)
	}
	if _, err := core.skillStore().Load(); err != nil {
		t.Fatal(err)
	}
}

func TestApplySkillsKeepsPartialSuccess(t *testing.T) {
	home := t.TempDir()
	core := skillTestCore(home)
	for _, root := range []string{filepath.Join(home, ".codex", "skills"), filepath.Join(home, ".claude", "skills")} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(t.TempDir(), "review")
	writeTestSkillAt(t, source, "first")
	preview, err := core.PreviewSkillImport(context.Background(), SkillImportRequest{Source: "folder"}, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "skills", "review"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := core.ApplySkills(context.Background(), SkillApplyRequest{PreviewToken: preview.Token, Changes: []SkillChange{{
		ID: "review", VariantHash: preview.Candidates[0].Hash, Targets: []string{"claude-code", "codex"}, ImportSource: "folder",
	}}})
	if len(result.Results) != 2 {
		t.Fatalf("results = %#v", result.Results)
	}
	var succeeded, failed bool
	for _, item := range result.Results {
		succeeded = succeeded || item.Agent == "codex" && item.Error == "" && item.RegistryUpdated
		failed = failed || item.Agent == "claude-code" && item.Error != ""
	}
	if !succeeded || !failed {
		t.Fatalf("partial result = %#v", result.Results)
	}
}

func TestApplySkillsRefusesToDeleteChangedManagedTarget(t *testing.T) {
	home := t.TempDir()
	core := skillTestCore(home)
	root := filepath.Join(home, ".codex", "skills")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "review")
	writeTestSkillAt(t, source, "first")
	preview, _ := core.PreviewSkillImport(context.Background(), SkillImportRequest{Source: "folder"}, source)
	first := core.ApplySkills(context.Background(), SkillApplyRequest{PreviewToken: preview.Token, Changes: []SkillChange{{ID: "review", VariantHash: preview.Candidates[0].Hash, Targets: []string{"codex"}, ImportSource: "folder"}}})
	if first.Results[0].Error != "" {
		t.Fatalf("initial apply = %#v", first)
	}
	if err := os.WriteFile(filepath.Join(root, "review", "local.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := core.ApplySkills(context.Background(), SkillApplyRequest{Changes: []SkillChange{{ID: "review", VariantHash: preview.Candidates[0].Hash, Delete: true}}})
	if len(result.Results) != 1 || result.Results[0].Error == "" {
		t.Fatalf("delete result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "review", "local.txt")); err != nil {
		t.Fatalf("changed target was deleted: %v", err)
	}
}

func TestUninstallSkillBacksUpBeforeRemovingContent(t *testing.T) {
	home := t.TempDir()
	core := skillTestCore(home)
	root := filepath.Join(home, ".codex", "skills")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "review")
	writeTestSkillAt(t, source, "first")
	preview, _ := core.PreviewSkillImport(context.Background(), SkillImportRequest{Source: "folder"}, source)
	core.ApplySkills(context.Background(), SkillApplyRequest{PreviewToken: preview.Token, Changes: []SkillChange{{ID: "review", VariantHash: preview.Candidates[0].Hash, Targets: []string{"codex"}, ImportSource: "folder"}}})
	result := core.UninstallSkill(context.Background(), "review")
	if result.Error != "" || result.BackupID == "" || !result.RegistryUpdated {
		t.Fatalf("uninstall = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "review")); !os.IsNotExist(err) {
		t.Fatalf("managed projection remains: %v", err)
	}
	backups, err := core.ListSkillBackups(context.Background())
	if err != nil || len(backups) != 1 || backups[0].BackupID != result.BackupID {
		t.Fatalf("backups = %#v, %v", backups, err)
	}
	if _, err := core.GetSkill(context.Background(), "review"); err == nil {
		t.Fatal("uninstalled Skill remains in registry")
	}
}

func TestDraftStateCombinesMCPAndSkills(t *testing.T) {
	core := &UseCases{}
	core.SetMCPDraftState(true, "en")
	core.SetSkillDraftState(true, "zh")
	if dirty, locale := core.DraftState(); !dirty || locale != "zh" {
		t.Fatalf("combined draft = %v, %q", dirty, locale)
	}
	core.SetMCPDraftState(false, "")
	if dirty, locale := core.MCPDraftState(); dirty || locale != "zh" {
		t.Fatalf("MCP draft = %v, %q", dirty, locale)
	}
	if dirty, locale := core.SkillDraftState(); !dirty || locale != "zh" {
		t.Fatalf("Skill draft = %v, %q", dirty, locale)
	}
}

func skillTestCore(home string) *UseCases {
	return NewUseCases(StatusOptions{
		Home: home, Platform: platform.Info{OS: "darwin", Arch: "arm64"},
		Lookup: func(command string) (string, bool) { return "/fake/" + command, true },
	})
}

func writeTestSkill(t *testing.T, root, id, body string) {
	t.Helper()
	writeTestSkillAt(t, filepath.Join(root, id), body)
}

func writeTestSkillAt(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: Review\ndescription: Review code\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.HashTree(context.Background(), root); err != nil {
		t.Fatal(err)
	}
}
