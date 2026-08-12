package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateIDRejectsPathAndReservedNames(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../x", `a\b`, "/tmp/x", "CON", "a\x00b", strings.Repeat("a", 129)} {
		if ValidateID(id) == nil {
			t.Errorf("ValidateID(%q) accepted", id)
		}
	}
	for _, id := range []string{"Review", "team.one", "a_b-2", "A1"} {
		if err := ValidateID(id); err != nil {
			t.Errorf("ValidateID(%q): %v", id, err)
		}
	}
}

func TestHashTreeIsStableAcrossCreationOrder(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeSkill(t, first, map[string]string{"SKILL.md": "---\nname: Review\ndescription: Check code\n---\nbody\n", "z.txt": "z", "nested/a.txt": "a"})
	writeSkill(t, second, map[string]string{"nested/a.txt": "a", "z.txt": "z", "SKILL.md": "---\nname: Review\ndescription: Check code\n---\nbody\n"})
	a, err := HashTree(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashTree(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash || a.Files != 3 || a.Bytes == 0 {
		t.Fatalf("hash/stats = %#v %#v", a, b)
	}
}

func TestHashTreeRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, map[string]string{"SKILL.md": "body"})
	if err := os.Symlink(filepath.Join(root, "SKILL.md"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := HashTree(context.Background(), root); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestReadMetadataBoundsAndFallback(t *testing.T) {
	root := t.TempDir()
	if name, description, diagnostic := ReadMetadata(context.Background(), root, "fallback"); name != "fallback" || description != "" || diagnostic == "" {
		t.Fatalf("missing metadata = %q %q %q", name, description, diagnostic)
	}
	content := "---\nname: " + strings.Repeat("n", 400) + "\ndescription: " + strings.Repeat("d", 1200) + "\n---\nbody"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	name, description, diagnostic := ReadMetadata(context.Background(), root, "fallback")
	if len(name) != 256 || len(description) != 1024 || diagnostic != "" {
		t.Fatalf("bounded metadata = %d %d %q", len(name), len(description), diagnostic)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: [broken\n---"), 0600); err != nil {
		t.Fatal(err)
	}
	name, _, diagnostic = ReadMetadata(context.Background(), root, "fallback")
	if name != "fallback" || diagnostic == "" || strings.Contains(diagnostic, root) {
		t.Fatalf("malformed metadata = %q %q", name, diagnostic)
	}
}

func TestReadMetadataBoundsMultibyteTextByBytes(t *testing.T) {
	root := t.TempDir()
	content := "---\nname: " + strings.Repeat("名", 200) + "\ndescription: " + strings.Repeat("描", 500) + "\n---\nbody"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	name, description, diagnostic := ReadMetadata(context.Background(), root, "fallback")
	if diagnostic != "" || len([]byte(name)) > 256 || len([]byte(description)) > 1024 {
		t.Fatalf("metadata bounds = name %d description %d diagnostic %q", len([]byte(name)), len([]byte(description)), diagnostic)
	}
}

func writeSkill(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
