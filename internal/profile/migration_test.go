package profile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

func TestLoadActiveMigratesV1WithBackupAndPublicProjection(t *testing.T) {
	home := t.TempDir()
	store := testStore(t, home, "linux")
	if err := os.MkdirAll(store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"schema_version":1,"provider":"ppio","base_url":"https://api.ppio.com/openai","model":"legacy-model","config_mode":"provider","agent_ids":["opencode","codex"],"activated_at":"2026-07-22T00:00:00Z","api_key":"must-not-copy"}`
	if err := os.WriteFile(store.PointerPath(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	result := store.LoadActiveContext(context.Background())
	if result.Error != "" || result.Profile == nil || result.ID != "default" {
		t.Fatalf("migration result = %#v", result)
	}
	if result.Profile.CreatedAt != "2026-07-22T00:00:00Z" || result.Environment["model"] != "legacy-model" {
		t.Fatalf("migrated projection = %#v", result)
	}
	var pointer map[string]any
	data, err := os.ReadFile(store.PointerPath())
	if err != nil || json.Unmarshal(data, &pointer) != nil || pointer["active"] != "default" || pointer["schema_version"] != float64(2) {
		t.Fatalf("migrated pointer = %s, %v", data, err)
	}
	profilePath, _ := store.ProfilePath("default")
	profileData, err := os.ReadFile(profilePath)
	if err != nil || strings.Contains(string(profileData), "must-not-copy") {
		t.Fatalf("migrated profile = %s, %v", profileData, err)
	}
	backups, _ := filepath.Glob(store.PointerPath() + ".backup-*")
	if len(backups) != 1 {
		t.Fatalf("legacy backups = %v", backups)
	}
	if listed := store.List(); len(listed) != 1 || listed[0].ID != "default" {
		t.Fatalf("migrated list = %#v", listed)
	}
	// A second read follows the v2 pointer and does not create another backup.
	if result := store.LoadActive(); result.Error != "" || result.ID != "default" {
		t.Fatalf("second migrated read = %#v", result)
	}
	backups, _ = filepath.Glob(store.PointerPath() + ".backup-*")
	if len(backups) != 1 {
		t.Fatalf("second read changed backups = %v", backups)
	}
}

func TestLegacyMigrationFailureLeavesOriginalPointer(t *testing.T) {
	home := t.TempDir()
	filesystem := securefs.New(securefs.Options{
		OS: "linux",
		Secure: func(path string, directory bool) error {
			if directory && strings.HasSuffix(path, "profiles") {
				return oneerrors.New(oneerrors.ConfigWriteFailed, "profiles are read-only")
			}
			return nil
		},
	})
	store := NewStoreWithDependencies(home, "linux", filesystem, fixedProfileClock)
	if err := os.MkdirAll(store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"schema_version":1,"provider":"ppio","model":"m"}`)
	if err := os.WriteFile(store.PointerPath(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := store.LoadActive()
	if result.Profile != nil || !strings.Contains(result.Error, "Cannot migrate legacy profile") {
		t.Fatalf("migration failure = %#v", result)
	}
	data, err := os.ReadFile(store.PointerPath())
	if err != nil || string(data) != string(original) {
		t.Fatalf("original pointer changed: %s, %v", data, err)
	}
}
