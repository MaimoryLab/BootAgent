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
		valid := Registry{SchemaVersion: RegistrySchemaVersion, Skills: map[string]Fact{}}
		if err := s.Save(context.Background(), valid); err == nil {
			t.Errorf("overwrote %s registry", name)
		}
		got, err := os.ReadFile(s.RegistryPath())
		if err != nil || string(got) != data {
			t.Fatalf("%s registry changed: %q err=%v", name, got, err)
		}
	}
	if err := os.Remove(s.RegistryPath()); err != nil {
		t.Fatal(err)
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

func TestStoreBoundsFactMetadataAndImportSources(t *testing.T) {
	s := testStore(t)
	hash := strings.Repeat("a", 64)
	for name, fact := range map[string]Fact{
		"name":        {Name: strings.Repeat("n", 257), Variants: []Variant{{Hash: hash}}},
		"description": {Description: strings.Repeat("d", 1025), Variants: []Variant{{Hash: hash}}},
		"source":      {Variants: []Variant{{Hash: hash, ImportSources: []string{"remote"}}}},
	} {
		registry := Registry{SchemaVersion: RegistrySchemaVersion, Skills: map[string]Fact{"review": fact}}
		if err := s.Save(context.Background(), registry); err == nil {
			t.Errorf("accepted invalid %s", name)
		}
	}

	if err := os.MkdirAll(filepath.Dir(s.RegistryPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.RegistryPath(), []byte(strings.Repeat(" ", 9<<20)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("unbounded registry read: %v", err)
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

func TestSaveVariantHashesPrivateSnapshot(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "review")
	stats := writeStoredSkill(t, source, "review")
	mutated := false
	s := NewStore(home, securefs.New(securefs.Options{OS: "linux", Secure: func(string, bool) error {
		if !mutated {
			mutated = true
			return os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("changed"), 0600)
		}
		return nil
	}}))
	if err := s.SaveVariant(context.Background(), "review", source, stats); err == nil {
		t.Fatal("published a source changed during snapshot")
	}
	if _, err := os.Stat(s.VariantPath("review", stats.Hash)); !os.IsNotExist(err) {
		t.Fatalf("changed snapshot was published: %v", err)
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
	staleHash := strings.Repeat("e", 64)
	writeStoredSkill(t, s.VariantPath("review", staleHash), "stale")
	if _, err := s.RestoreBackup(context.Background(), backups[0].BackupID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.VariantPath("review", staleHash)); !os.IsNotExist(err) {
		t.Fatalf("restore retained stale variant: %v", err)
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

func TestCreateBackupRejectsIncompleteStoredVariants(t *testing.T) {
	s := testStore(t)
	missing := strings.Repeat("a", 64)
	fact := Fact{Variants: []Variant{{Hash: missing, Stored: true}}}
	if _, err := s.CreateBackup(context.Background(), "review", fact); err == nil {
		t.Fatal("backed up missing stored variant")
	}
	entries, err := os.ReadDir(s.BackupRoot())
	if os.IsNotExist(err) {
		entries, err = nil, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("incomplete backup retained: %v", entries)
	}

	source := filepath.Join(t.TempDir(), "review")
	stats := writeStoredSkill(t, source, "review")
	if err := s.SaveVariant(context.Background(), "review", source, stats); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.VariantPath("review", stats.Hash), "SKILL.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	fact = Fact{Variants: []Variant{{Hash: stats.Hash, Stored: true}}}
	if _, err := s.CreateBackup(context.Background(), "review", fact); err == nil {
		t.Fatal("backed up changed stored variant")
	}
}

func TestCreateBackupValidatesCopiedSnapshot(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "review")
	stats := writeStoredSkill(t, source, "review")
	s := NewStore(home, securefs.New(securefs.Options{OS: "linux"}))
	if err := s.SaveVariant(context.Background(), "review", source, stats); err != nil {
		t.Fatal(err)
	}
	s.fs = securefs.New(securefs.Options{OS: "linux", Secure: func(path string, directory bool) error {
		if !directory && strings.Contains(path, ".oneagent-backup-pending-") && filepath.Base(path) == "SKILL.md" {
			return os.WriteFile(path, []byte("changed"), 0600)
		}
		return nil
	}})
	if _, err := s.CreateBackup(context.Background(), "review", Fact{Variants: []Variant{{Hash: stats.Hash, Stored: true}}}); err == nil {
		t.Fatal("completed backup from changed snapshot")
	}
}

func TestListBackupsSkipsPendingAndInvalidEntries(t *testing.T) {
	s := testStore(t)
	if err := os.MkdirAll(filepath.Join(s.BackupRoot(), ".oneagent-backup-pending-test"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.BackupRoot(), "invalid"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "review")
	stats := writeStoredSkill(t, source, "review")
	if err := s.SaveVariant(context.Background(), "review", source, stats); err != nil {
		t.Fatal(err)
	}
	valid, err := s.CreateBackup(context.Background(), "review", Fact{Variants: []Variant{{Hash: stats.Hash, Stored: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.BackupRoot(), valid.BackupID, "content", "variants", stats.Hash, "SKILL.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	backups, err := s.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("listed invalid backups: %#v", backups)
	}
}

func TestStoreRejectsSymlinkedPrivateRoots(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("symlink permissions are environment-dependent on Windows")
	}
	ctx := context.Background()
	for _, operation := range []string{"save", "remove", "backup"} {
		t.Run(operation, func(t *testing.T) {
			s := testStore(t)
			outside := t.TempDir()
			if err := os.MkdirAll(filepath.Join(s.home, ".oneagent"), 0700); err != nil {
				t.Fatal(err)
			}
			root := s.SkillsRoot()
			if operation == "backup" {
				root = s.BackupRoot()
			}
			if err := os.Symlink(outside, root); err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "save":
				source := filepath.Join(t.TempDir(), "review")
				stats := writeStoredSkill(t, source, "review")
				err = s.SaveVariant(ctx, "review", source, stats)
			case "remove":
				if err = os.MkdirAll(filepath.Join(outside, "review"), 0700); err == nil {
					err = s.RemoveSkill(ctx, "review")
				}
			case "backup":
				err = func() error {
					_, backupErr := s.CreateBackup(ctx, "review", Fact{})
					return backupErr
				}()
			}
			if err == nil {
				t.Fatalf("%s accepted symlinked root", operation)
			}
			if _, statErr := os.Stat(outside); statErr != nil {
				t.Fatalf("outside root changed: %v", statErr)
			}
		})
	}
}

func TestRemoveSkillRejectsSymlinkedID(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("symlink permissions are environment-dependent on Windows")
	}
	s := testStore(t)
	outside := t.TempDir()
	marker := filepath.Join(outside, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.SkillsRoot(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.SkillsRoot(), "review")); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveSkill(context.Background(), "review"); err == nil {
		t.Fatal("removed symlinked Skill ID")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("outside content changed: %v", err)
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
