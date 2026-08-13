package skill

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestScanAgentRootFindsImmediateSkillDirectories(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "review"), map[string]string{"SKILL.md": "body"})
	writeSkill(t, filepath.Join(root, "nested", "ignored"), map[string]string{"SKILL.md": "body"})
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ScanAgentRoot(context.Background(), root, "claude-code")
	if err != nil || len(got) != 1 || got[0].ID != "review" {
		t.Fatalf("candidates = %#v, err=%v", got, err)
	}
}

func TestDiscoverFolderFindsNestedSkillDirectories(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "pack", "review"), map[string]string{"SKILL.md": "body"})
	writeSkill(t, filepath.Join(root, "pack", "lint"), map[string]string{"SKILL.md": "body2"})
	got, err := DiscoverFolder(context.Background(), root)
	if err != nil || len(got) != 2 {
		t.Fatalf("candidates = %#v, err=%v", got, err)
	}
}

func TestDiscoverZIPRejectsTraversalDuplicateAndSymlink(t *testing.T) {
	for _, entries := range [][]zipTestEntry{
		{{name: "../SKILL.md", data: "x"}},
		{{name: "a/SKILL.md", data: "x"}, {name: "a/./SKILL.md", data: "y"}},
		{{name: "a/SKILL.md", mode: os.ModeSymlink, data: "target"}},
		{{name: "a/", mode: os.ModeNamedPipe}},
		{{name: "C:foo/SKILL.md", data: "x"}},
	} {
		zipPath := makeZip(t, entries)
		if _, err := DiscoverZIP(context.Background(), zipPath, t.TempDir()); err == nil {
			t.Fatalf("accepted invalid archive %#v", entries)
		}
	}
}

func TestCleanupCandidatesUsesExactZIPStagingRoot(t *testing.T) {
	stagingParent := t.TempDir()
	zipPath := makeZip(t, []zipTestEntry{{
		name: ".oneagent-skill-zip-nested/SKILL.md",
		data: "body",
	}})
	candidates, err := DiscoverZIP(context.Background(), zipPath, stagingParent)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates = %#v, err=%v", candidates, err)
	}
	if err := CleanupCandidates(candidates); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(stagingParent); err != nil || len(entries) != 0 {
		t.Fatalf("staging leak = %v, err=%v", entries, err)
	}
}

func TestDiscoverZIPFindsCandidates(t *testing.T) {
	stagingParent := t.TempDir()
	zipPath := makeZip(t, []zipTestEntry{
		{name: "review/SKILL.md", data: "---\nname: Review\n---\nbody"},
		{name: "review/rules.txt", data: "rules"},
	})
	got, err := DiscoverZIP(context.Background(), zipPath, stagingParent)
	if err != nil || len(got) != 1 || got[0].ID != "review" {
		t.Fatalf("candidates = %#v, err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(got[0].Path, "SKILL.md")); err != nil {
		t.Fatalf("candidate was removed before use: %v", err)
	}
	if err := CleanupCandidates(got); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(stagingParent); err != nil || len(entries) != 0 {
		t.Fatalf("staging leak = %v, err=%v", entries, err)
	}
}

func TestDiscoverZIPRejectsEntryAndExpandedLimits(t *testing.T) {
	entries := make([]zipTestEntry, maxZIPEntries+1)
	for i := range entries {
		entries[i] = zipTestEntry{name: filepath.Join("skill", "file-"+strconv.Itoa(i)), data: "x"}
	}
	if _, err := DiscoverZIP(context.Background(), makeZip(t, entries), t.TempDir()); err == nil {
		t.Fatal("entry limit was not enforced")
	}
}

func TestCopyTreeRejectsSymlinkAndPreservesFiles(t *testing.T) {
	source, destination := t.TempDir(), filepath.Join(t.TempDir(), "copy")
	writeSkill(t, source, map[string]string{"SKILL.md": "body", "nested/rules.txt": "rules"})
	if err := CopyTree(context.Background(), source, destination); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(destination, "nested", "rules.txt"))
	if err != nil || string(b) != "rules" {
		t.Fatalf("copied file = %q, err=%v", b, err)
	}
	if err := os.Symlink(filepath.Join(source, "SKILL.md"), filepath.Join(source, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := CopyTree(context.Background(), source, filepath.Join(t.TempDir(), "bad")); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestPublishTreeRestoresOriginalOnPublicationFailure(t *testing.T) {
	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "skill")
	writeSkill(t, source, map[string]string{"SKILL.md": "new"})
	writeSkill(t, destination, map[string]string{"SKILL.md": "old"})
	originalRename := renamePath
	defer func() { renamePath = originalRename }()
	renames := 0
	renamePath = func(oldPath, newPath string) error {
		renames++
		if renames == 2 {
			return os.ErrPermission
		}
		return os.Rename(oldPath, newPath)
	}
	if err := PublishTree(context.Background(), source, destination); err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	b, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	if err != nil || string(b) != "old" {
		t.Fatalf("restored file = %q, err=%v", b, err)
	}
}

func TestPublishTreeReportsRollbackRestorationFailure(t *testing.T) {
	source := t.TempDir()
	parent := t.TempDir()
	destination := filepath.Join(parent, "skill")
	writeSkill(t, source, map[string]string{"SKILL.md": "new"})
	writeSkill(t, destination, map[string]string{"SKILL.md": "old"})
	originalRename := renamePath
	defer func() { renamePath = originalRename }()
	renames := 0
	renamePath = func(oldPath, newPath string) error {
		renames++
		if renames >= 2 {
			return os.ErrPermission
		}
		return os.Rename(oldPath, newPath)
	}
	err := PublishTree(context.Background(), source, destination)
	if err == nil || !strings.Contains(err.Error(), "restore") {
		t.Fatalf("error = %v", err)
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil {
		t.Fatal(readErr)
	}
	rollbackFound := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".oneagent-rollback-") {
			rollbackFound = true
			if b, err := os.ReadFile(filepath.Join(parent, entry.Name(), "SKILL.md")); err != nil || string(b) != "old" {
				t.Fatalf("rollback content = %q, err=%v", b, err)
			}
		}
	}
	if !rollbackFound {
		t.Fatal("rollback directory was removed")
	}
}

type zipTestEntry struct {
	name string
	data string
	mode os.FileMode
}

func makeZip(t *testing.T, entries []zipTestEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		header.SetMode(entry.mode)
		writer, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if entry.data != "" {
			if _, err := writer.Write([]byte(entry.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoveryDiagnosticsDoNotLeakSourcePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(strings.Repeat("x", maxMetadataBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverFolder(context.Background(), root)
	if err != nil || len(got) != 1 || strings.Contains(got[0].Diagnostic, root) {
		t.Fatalf("candidate = %#v, err=%v", got, err)
	}
}
