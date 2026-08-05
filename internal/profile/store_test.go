package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeProfileFixture(t *testing.T, store Store, id, content string) {
	t.Helper()
	path, err := store.ProfilePath(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestValidateIDAndPathsRejectTraversal(t *testing.T) {
	store := NewStore(t.TempDir(), "linux")
	for _, id := range []string{"", "Upper", "-lead", "a" + string(make([]byte, 65)), "../escape", "a/b"} {
		if err := ValidateID(id); err == nil {
			t.Errorf("ValidateID(%q) unexpectedly succeeded", id)
		}
		if _, err := store.ProfilePath(id); err == nil {
			t.Errorf("ProfilePath(%q) unexpectedly succeeded", id)
		}
	}
	if got, err := store.SecretPath("team_one"); err != nil || filepath.Base(got) != "team_one.env" {
		t.Fatalf("SecretPath() = %q, %v", got, err)
	}
	if got, err := NewStore(t.TempDir(), "windows").SecretPath("team_one"); err != nil || filepath.Base(got) != "team_one.env.ps1" {
		t.Fatalf("Windows SecretPath() = %q, %v", got, err)
	}
}

func TestEmptyStoreAndStableListProjection(t *testing.T) {
	store := NewStore(t.TempDir(), "linux")
	if got := store.List(); len(got) != 0 {
		t.Fatalf("empty List() = %#v", got)
	}
	if result := store.LoadActive(); result.Profile != nil || result.ID != "" || result.Error != "" {
		t.Fatalf("empty LoadActive() = %#v", result)
	}
	writeProfileFixture(t, store, "b-profile", `{"schema_version":2,"id":"b-profile","label":"B","provider":"novita","base_url":null,"model":"m2","config_mode":"provider","agent_ids":["aider"],"created_at":"t2","activated_at":null}`)
	writeProfileFixture(t, store, "a-profile", `{"schema_version":2,"id":"a-profile","label":"A","provider":"ppio","base_url":"https://api.ppio.com/openai","model":"m1","config_mode":"provider","agent_ids":["codex"],"created_at":"t1","activated_at":"t1"}`)
	secret, err := store.SecretPath("a-profile")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(secret), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("export ONEAGENT_API_KEY=hidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profiles := store.List()
	if len(profiles) != 2 || profiles[0].ID != "a-profile" || profiles[1].ID != "b-profile" || !profiles[0].HasKey || profiles[1].HasKey {
		t.Fatalf("stable profile list = %#v", profiles)
	}
	summary := profiles[0].Summary()
	if summary.HasKey != true || summary.BaseURL == nil || *summary.BaseURL != "https://api.ppio.com/openai" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestLoadActiveV2PreservesPointerAndStripsSecrets(t *testing.T) {
	store := NewStore(t.TempDir(), "linux")
	writeProfileFixture(t, store, "team", `{"schema_version":2,"id":"team","label":"Team","provider":"ppio","base_url":"https://api.ppio.com/openai","model":"m","config_mode":"provider","agent_ids":["codex"],"created_at":"created","activated_at":"active","api_key":"must-not-escape"}`)
	if err := os.MkdirAll(store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.PointerPath(), []byte(`{"schema_version":2,"active":"team"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := store.LoadActive()
	if result.Error != "" || result.ID != "team" || result.Profile == nil {
		t.Fatalf("active result = %#v", result)
	}
	if result.Environment["api_key"] != nil {
		t.Fatalf("environment exposed a secret: %#v", result.Environment)
	}
	if result.Environment["provider"] != "ppio" || result.Environment["model"] != "m" {
		t.Fatalf("environment projection = %#v", result.Environment)
	}
}

func TestListSkipsCorruptAndSecretContentsNeverEnterSummary(t *testing.T) {
	store := NewStore(t.TempDir(), "linux")
	if err := os.MkdirAll(store.ProfilesPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.ProfilesPath(), "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.ProfilesPath(), "noid.json"), []byte(`{"schema_version":2,"provider":"ppio"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeProfileFixture(t, store, "real", `{"schema_version":2,"id":"real","provider":"ppio","model":"m"}`)
	profiles := store.List()
	if len(profiles) != 1 || profiles[0].ID != "real" {
		t.Fatalf("filtered profiles = %#v", profiles)
	}
}
