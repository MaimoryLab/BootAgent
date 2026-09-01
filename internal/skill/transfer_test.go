package skill

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

func TestExtractArchiveValidatesManifestAndSafePaths(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("demo"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := zipTree(context.Background(), source, ExportManifest{Format: "bootagent-skill", Version: 1, ID: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "skill")
	manifest, err := ExtractArchive(context.Background(), archive, dest)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "demo" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
}

func TestExportSkillProducesPortableArchive(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home, securefs.New(securefs.Options{OS: "linux"}))
	source := filepath.Join(home, "source")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "notes.txt"), []byte("portable"), 0o600); err != nil {
		t.Fatal(err)
	}
	stats, err := HashTree(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	root := store.VariantPath("demo", stats.Hash)
	if err := CopyTree(context.Background(), source, root); err != nil {
		t.Fatal(err)
	}
	registry := Registry{SchemaVersion: RegistrySchemaVersion, Skills: map[string]Fact{"demo": {Name: "Demo", Description: "test", Variants: []Variant{{Hash: stats.Hash, Stored: true}}}}}
	if err := store.Save(context.Background(), registry); err != nil {
		t.Fatal(err)
	}
	archive, err := store.Export(context.Background(), "demo", stats.Hash)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(strings.NewReader(string(archive)), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range reader.File {
		seen[entry.Name] = true
	}
	for _, name := range []string{"manifest.json", "SKILL.md", "nested/notes.txt"} {
		if !seen[name] {
			t.Errorf("archive missing %s", name)
		}
	}
	var manifest ExportManifest
	for _, entry := range reader.File {
		if entry.Name != "manifest.json" {
			continue
		}
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewDecoder(file).Decode(&manifest); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
	}
	if manifest.ID != "demo" || manifest.Variant.Hash != stats.Hash {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestExportSkillRejectsUnknownOrUnstoredVariant(t *testing.T) {
	store := NewStore(t.TempDir(), securefs.New(securefs.Options{OS: "linux"}))
	if err := store.Save(context.Background(), Registry{SchemaVersion: RegistrySchemaVersion, Skills: map[string]Fact{"demo": {Variants: []Variant{{Hash: strings.Repeat("a", 64), Stored: false}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Export(context.Background(), "demo", strings.Repeat("a", 64)); err == nil {
		t.Fatal("unstored variant exported")
	}
	if _, err := store.Export(context.Background(), "missing", strings.Repeat("a", 64)); err == nil {
		t.Fatal("unknown skill exported")
	}
}
