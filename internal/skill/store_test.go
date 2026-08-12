package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

func testStore(t *testing.T) Store {
	t.Helper()
	return NewStore(t.TempDir(), securefs.New(securefs.Options{OS: "linux"}))
}

func writeStoredSkill(t *testing.T, root, body string) TreeStats {
	t.Helper()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	stats, err := HashTree(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func TestStoreLoadAndValidation(t *testing.T) {
	s := testStore(t)
	r, err := s.Load()
	if err != nil || r.SchemaVersion != RegistrySchemaVersion || r.Skills == nil {
		t.Fatalf("registry=%#v err=%v", r, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.RegistryPath()), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"malformed":  `{`,
		"new schema": `{"schema_version":2,"skills":{}}`,
	} {
		if err := os.WriteFile(s.RegistryPath(), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(); err == nil {
			t.Errorf("accepted %s registry", name)
		}
	}
	hash := strings.Repeat("a", 64)
	valid := Registry{SchemaVersion: RegistrySchemaVersion, Skills: map[string]Fact{"review": {Variants: []Variant{{Hash: hash, ObservedAgents: []string{"a"}}}}}}
	if err := s.Save(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, s.RegistryPath()); mode != 0600 {
		t.Fatalf("registry mode=%o", mode)
	}
	valid.Skills["review"] = Fact{Variants: []Variant{{Hash: hash, ObservedAgents: []string{"b", "a"}}}}
	if err := s.Save(context.Background(), valid); err == nil {
		t.Fatal("accepted unsorted associations")
	}
	valid.Skills["review"] = Fact{Variants: []Variant{{Hash: strings.Repeat("A", 64)}}}
	if err := s.Save(context.Background(), valid); err == nil {
		t.Fatal("accepted invalid hash")
	}
}

func TestSaveVariantUsesComputedPath(t *testing.T) {
	s := testStore(t)
	source := filepath.Join(t.TempDir(), "review")
	stats := writeStoredSkill(t, source, "review")
	if err := os.Chmod(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(source, "SKILL.md"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveVariant(context.Background(), "review", source, stats); err != nil {
		t.Fatal(err)
	}
	variant := s.VariantPath("review", stats.Hash)
	if _, err := os.Stat(filepath.Join(variant, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, variant); mode != 0700 {
		t.Fatalf("variant mode=%o", mode)
	}
	if mode := fileMode(t, filepath.Join(variant, "SKILL.md")); mode != 0600 {
		t.Fatalf("Skill file mode=%o", mode)
	}
	bad := stats
	bad.Hash = strings.Repeat("0", 64)
	if err := s.SaveVariant(context.Background(), "review", source, bad); err == nil {
		t.Fatal("accepted stale hash")
	}
}

func TestBackupRestoreAndRetention(t *testing.T) {
	s := testStore(t)
	source := filepath.Join(t.TempDir(), "review")
	stats := writeStoredSkill(t, source, "review")
	if err := s.SaveVariant(context.Background(), "review", source, stats); err != nil {
		t.Fatal(err)
	}
	fact := Fact{Name: "Review", Variants: []Variant{
		{Hash: stats.Hash, Stored: true},
		{Hash: strings.Repeat("f", 64), ObservedAgents: []string{"codex"}},
	}}
	var first BackupSummary
	for i := 0; i < 21; i++ {
		backup, err := s.CreateBackup(context.Background(), "review", fact)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = backup
		}
	}
	backups, err := s.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 20 {
		t.Fatalf("backups=%d", len(backups))
	}
	if backups[0].BackupID == first.BackupID {
		t.Fatal("oldest backup was retained")
	}
	if err := s.RemoveSkill(context.Background(), "review"); err != nil {
		t.Fatal(err)
	}
	restored, err := s.RestoreBackup(context.Background(), backups[0].BackupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Variants) != 2 || restored.Variants[0].Hash != stats.Hash {
		t.Fatalf("restored=%#v", restored)
	}
	if _, err := os.Stat(filepath.Join(s.VariantPath("review", stats.Hash), "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.BackupRoot(), backups[0].BackupID, "content", "variants", stats.Hash, "SKILL.md"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreBackup(context.Background(), backups[0].BackupID); err == nil {
		t.Fatal("restored tampered backup")
	}
	for _, id := range []string{".", "..", "../outside"} {
		if _, err := s.RestoreBackup(context.Background(), id); err == nil {
			t.Errorf("accepted backup ID %q", id)
		}
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
