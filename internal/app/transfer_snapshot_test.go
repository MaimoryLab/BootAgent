package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/skill"
	"github.com/MaimoryLab/BootAgent/internal/transfer"
)

func TestTransferFileSnapshotRestoresTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profiles")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "before.json"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "before.json"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "after.json"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.restore(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "nested", "before.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before" {
		t.Fatalf("restored content = %q, want before", data)
	}
	if _, err := os.Stat(filepath.Join(root, "after.json")); !os.IsNotExist(err) {
		t.Fatalf("new file still exists after restore: %v", err)
	}
}

func TestImportTransferV2RestoresConfigWhenSkillHashFails(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	var skillArchive bytes.Buffer
	zw := zip.NewWriter(&skillArchive)
	manifest, _ := zw.Create("manifest.json")
	manifestData, _ := json.Marshal(skill.ExportManifest{Format: "bootagent-skill", Version: 1, ID: "broken", Name: "Broken", Variant: skill.ExportVariant{Hash: "0000000000000000000000000000000000000000000000000000000000000000"}})
	_, _ = manifest.Write(manifestData)
	content, _ := zw.Create("SKILL.md")
	_, _ = content.Write([]byte("# Broken"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(map[string]any{
		"providers": []map[string]any{{"id": "custom-provider", "name": "Custom", "base_url": "https://api.example.com/v1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := transfer.Build(map[string][]byte{"config.json": config, "skills/broken.skill.zip": skillArchive.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.ImportTransferV2(context.Background(), bundle); err == nil {
		t.Fatal("hash-mismatched Skill import unexpectedly succeeded")
	}
	if _, err := os.Stat(core.providers.Path()); !os.IsNotExist(err) {
		t.Fatalf("provider written before Skill failure was not rolled back: %v", err)
	}
}

func TestImportTransferV2RestoresConfigWhenMCPFails(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	config, err := json.Marshal(map[string]any{
		"providers": []map[string]any{{"id": "custom-provider", "name": "Custom", "base_url": "https://api.example.com/v1"}},
		"mcp":       map[string]any{"schema_version": 999, "servers": map[string]any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := transfer.Build(map[string][]byte{"config.json": config})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.ImportTransferV2(context.Background(), bundle); err == nil {
		t.Fatal("invalid MCP import unexpectedly succeeded")
	}
	if _, err := os.Stat(core.providers.Path()); !os.IsNotExist(err) {
		t.Fatalf("provider written before MCP failure was not rolled back: %v", err)
	}
}

func TestImportTransferV2RestoresConfigWhenLaterProfileFails(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	config, err := json.Marshal(map[string]any{
		"providers": []map[string]any{{"id": "custom-provider", "name": "Custom", "base_url": "https://api.example.com/v1"}},
		"profiles":  []map[string]any{{"id": "broken", "label": "Broken", "provider": "custom-provider"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := transfer.Build(map[string][]byte{"config.json": config})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.ImportTransferV2(context.Background(), bundle); err == nil {
		t.Fatal("invalid profile import unexpectedly succeeded")
	}
	if _, err := os.Stat(core.providers.Path()); !os.IsNotExist(err) {
		t.Fatalf("provider written before profile failure was not rolled back: %v", err)
	}
}

func TestTransferFileSnapshotRestoresAbsentPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing.json")
	snapshot, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("restored absent path unexpectedly exists: %v", err)
	}
}
