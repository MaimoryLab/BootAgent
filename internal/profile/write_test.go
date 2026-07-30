package profile

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

func testStore(t *testing.T, home, osID string) Store {
	t.Helper()
	filesystem := securefs.New(securefs.Options{OS: osID, Now: fixedProfileClock})
	return NewStoreWithDependencies(home, osID, filesystem, fixedProfileClock)
}

func fixedProfileClock() time.Time {
	return time.Date(2026, time.July, 30, 13, 14, 15, 0, time.UTC)
}

func TestSaveProfileIsolatesSecretAndPreservesHistory(t *testing.T) {
	home := t.TempDir()
	store := testStore(t, home, "linux")
	secret := "sk-quoted'value"
	profile, err := store.Save(context.Background(), SaveRequest{
		ID:       "ppio-deepseek",
		Label:    "Team PPIO",
		Provider: "ppio",
		Model:    "deepseek-v3",
		AgentIDs: []string{"opencode", "codex", "codex"},
		APIKey:   secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != "ppio-deepseek" || !profile.HasKey || !reflect.DeepEqual(profile.AgentIDs, []string{"codex", "opencode"}) {
		t.Fatalf("saved profile = %#v", profile)
	}
	profilePath, _ := store.ProfilePath("ppio-deepseek")
	profileData, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(profileData), secret) || strings.Contains(string(profileData), "api_key") {
		t.Fatalf("profile file leaked secret material: %s", profileData)
	}
	secretPath, _ := store.SecretPath("ppio-deepseek")
	secretData, err := os.ReadFile(secretPath)
	if err != nil || !strings.Contains(string(secretData), "ONEAGENT_API_KEY") {
		t.Fatalf("secret file = %q, err=%v", secretData, err)
	}
	if got, err := store.ReadSecret(context.Background(), "ppio-deepseek"); err != nil || got != secret {
		t.Fatalf("ReadSecret() = %q, %v", got, err)
	}
	if got := store.List(); len(got) != 1 || !got[0].HasKey {
		t.Fatalf("List() = %#v", got)
	}
	created := profile.CreatedAt
	updated, err := store.Save(context.Background(), SaveRequest{
		ID:       "ppio-deepseek",
		Provider: "ppio",
		Model:    "model-b",
		AgentIDs: []string{"codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Label != "Team PPIO" || updated.CreatedAt != created || updated.Model == nil || *updated.Model != "model-b" || !updated.HasKey {
		t.Fatalf("updated profile = %#v", updated)
	}
	assertProfileMode(t, profilePath, 0o600)
	assertProfileMode(t, secretPath, 0o600)
}

func TestSaveProfileValidatesInputAndCustomBase(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	for _, request := range []SaveRequest{
		{ID: "../bad", Provider: "ppio", Model: "m", AgentIDs: []string{"codex"}},
		{ID: "ok", Provider: "nope", Model: "m", AgentIDs: []string{"codex"}},
		{ID: "ok", Provider: "ppio", Model: "", AgentIDs: []string{"codex"}},
		{ID: "ok", Provider: "ppio", Model: "m", AgentIDs: nil},
	} {
		if _, err := store.Save(context.Background(), request); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
			t.Errorf("invalid request %#v returned %v", request, err)
		}
	}
	profile, err := store.Save(context.Background(), SaveRequest{
		ID:       "custom-local",
		Provider: "custom",
		BaseURL:  "http://127.0.0.1:9000/",
		Model:    "m",
		AgentIDs: []string{"codex"},
	})
	if err != nil || profile.BaseURL == nil || *profile.BaseURL != "http://127.0.0.1:9000" {
		t.Fatalf("custom profile = %#v, err=%v", profile, err)
	}
}

func TestWriteActiveMergesAgentsAndSupportsExistingAccount(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	if _, err := store.WriteActive(context.Background(), ActiveRequest{
		Agents: []string{"codex"}, Configure: true, Provider: "ppio", Model: "model-a", APIKey: "sk-a",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteActive(context.Background(), ActiveRequest{
		Agents: []string{"opencode"}, Configure: true, Provider: "ppio", Model: "model-a",
	}); err != nil {
		t.Fatal(err)
	}
	active := store.LoadActive()
	if active.Error != "" || active.Profile == nil || !reflect.DeepEqual(active.Profile.AgentIDs, []string{"codex", "opencode"}) {
		t.Fatalf("merged active profile = %#v", active)
	}
	if active.Profile.BaseURL == nil || *active.Profile.BaseURL != "https://api.ppio.com/openai" {
		t.Fatalf("active base URL = %#v", active.Profile.BaseURL)
	}
	if got, err := store.ReadSecret(context.Background(), "default"); err != nil || got != "sk-a" {
		t.Fatalf("preserved active secret = %q, %v", got, err)
	}
	if _, err := store.WriteActive(context.Background(), ActiveRequest{
		Agents: []string{"aider"}, Configure: true, Provider: "novita", Model: "model-b",
	}); err != nil {
		t.Fatal(err)
	}
	active = store.LoadActive()
	if active.Profile.Provider != "novita" || !reflect.DeepEqual(active.Profile.AgentIDs, []string{"aider"}) {
		t.Fatalf("replaced active profile = %#v", active.Profile)
	}
	if _, err := store.WriteActive(context.Background(), ActiveRequest{Agents: []string{"codex"}, Configure: false}); err != nil {
		t.Fatal(err)
	}
	active = store.LoadActive()
	if active.Profile.ConfigMode != "existing-account" || active.Profile.Provider != "existing-account" || active.Profile.Model != nil || active.Profile.BaseURL != nil {
		t.Fatalf("existing account profile = %#v", active.Profile)
	}
	var pointer map[string]any
	data, _ := os.ReadFile(store.PointerPath())
	if err := json.Unmarshal(data, &pointer); err != nil || pointer["active"] != "default" || pointer["schema_version"] != float64(2) {
		t.Fatalf("active pointer = %s, %v", data, err)
	}
}

func TestProfileWritesHonorCancellation(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Save(ctx, SaveRequest{ID: "team", Provider: "ppio", Model: "m", AgentIDs: []string{"codex"}})
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("cancelled Save() = %v", err)
	}
	if _, err := store.ReadSecret(ctx, "team"); err == nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("cancelled ReadSecret() = %v", err)
	}
}

func TestWindowsSecretUsesPowerShellQuoting(t *testing.T) {
	store := testStore(t, t.TempDir(), "windows")
	// Replace the default securefs command runner with a no-op runner while
	// preserving the Windows ACL argument construction.
	filesystem := securefs.New(securefs.Options{
		OS:       "windows",
		Username: "tester",
		Run:      func(context.Context, []string) error { return nil },
		Now:      fixedProfileClock,
	})
	store = NewStoreWithDependencies(store.Home, "windows", filesystem, fixedProfileClock)
	if _, err := store.Save(context.Background(), SaveRequest{ID: "win", Provider: "ppio", Model: "m", AgentIDs: []string{"codex"}, APIKey: "key'value"}); err != nil {
		t.Fatal(err)
	}
	path, _ := store.SecretPath("win")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "$env:ONEAGENT_API_KEY = 'key''value'") {
		t.Fatalf("PowerShell secret = %q", data)
	}
	if got, err := store.ReadSecret(context.Background(), "win"); err != nil || got != "key'value" {
		t.Fatalf("Windows ReadSecret() = %q, %v", got, err)
	}
}

func assertProfileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
