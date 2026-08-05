package profile

import (
	"context"
	"encoding/json"
	"os"
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
		Protocol: "openai",
		APIKey:   secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != "ppio-deepseek" || !profile.HasKey || profile.Protocol != "openai" {
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
	if got := store.List(); len(got) != 1 || !got[0].HasKey {
		t.Fatalf("List() = %#v", got)
	}
	created := profile.CreatedAt
	updated, err := store.Save(context.Background(), SaveRequest{
		ID:       "ppio-deepseek",
		Provider: "ppio",
		Model:    "model-b",
		Protocol: "openai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Label != "Team PPIO" || updated.CreatedAt != created || updated.Model == nil || *updated.Model != "model-b" || !updated.HasKey {
		t.Fatalf("updated profile = %#v", updated)
	}
	if _, err := store.Save(context.Background(), SaveRequest{
		ID: "ppio-deepseek", Provider: "novita", Model: "model-b", Protocol: "openai",
	}); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("provider change without a new key returned %v", err)
	}
	assertProfileMode(t, profilePath, 0o600)
	assertProfileMode(t, secretPath, 0o600)
}

func TestSaveProfileValidatesInputAndCustomBase(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	for _, request := range []SaveRequest{
		{ID: "../bad", Provider: "ppio", Model: "m"},
		{ID: "ok", Provider: "nope", Model: "m"},
		{ID: "ok", Provider: "ppio", Model: ""},
	} {
		if _, err := store.Save(context.Background(), request); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
			t.Errorf("invalid request %#v returned %v", request, err)
		}
	}
	profile, err := store.Save(context.Background(), SaveRequest{
		ID:       "custom-local",
		Provider: "custom",
		BaseURL:  "http://127.0.0.1:9000/",
		Model:    "m", Protocol: "openai",
	})
	if err != nil || profile.BaseURL == nil || *profile.BaseURL != "http://127.0.0.1:9000" {
		t.Fatalf("custom profile = %#v, err=%v", profile, err)
	}
}

func TestWriteActiveReplacesProfileAndSupportsExistingAccount(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	if _, err := store.WriteActive(context.Background(), ActiveRequest{
		Configure: true, Provider: "ppio", Model: "model-a", Protocol: "responses", APIKey: "sk-a",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteActive(context.Background(), ActiveRequest{
		Configure: true, Provider: "ppio", Model: "model-a", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	active := store.LoadActive()
	if active.Error != "" || active.Profile == nil || active.Profile.Protocol != "openai" {
		t.Fatalf("updated active profile = %#v", active)
	}
	if active.Profile.BaseURL == nil || *active.Profile.BaseURL != "https://api.ppio.com/openai" {
		t.Fatalf("active base URL = %#v", active.Profile.BaseURL)
	}
	if _, err := store.WriteActive(context.Background(), ActiveRequest{
		Configure: true, Provider: "novita", Model: "model-b", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	active = store.LoadActive()
	if active.Profile.Provider != "novita" || active.Profile.Protocol != "openai" {
		t.Fatalf("replaced active profile = %#v", active.Profile)
	}
	if _, err := store.WriteActive(context.Background(), ActiveRequest{Configure: false}); err != nil {
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
	if got := string(data); !strings.HasPrefix(got, "{\n  \"schema_version\": 2,\n  \"active\": \"default\"\n}") {
		t.Fatalf("active pointer key order changed: %s", data)
	}
}

func TestProfileWritesHonorCancellation(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Save(ctx, SaveRequest{ID: "team", Provider: "ppio", Model: "m"})
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("cancelled Save() = %v", err)
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
	if _, err := store.Save(context.Background(), SaveRequest{ID: "win", Provider: "ppio", Model: "m", APIKey: "key'value"}); err != nil {
		t.Fatal(err)
	}
	path, _ := store.SecretPath("win")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "$env:ONEAGENT_API_KEY = 'key''value'") {
		t.Fatalf("PowerShell secret = %q", data)
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

func TestWriteActiveLabelsNewProfilesAndKeepsExistingNames(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	base := ActiveRequest{
		ProfileID: "codex-ppio", Configure: true,
		Provider: "ppio", Model: "model-a", APIKey: "sk-a",
	}
	labelled := base
	labelled.Label = "Codex · PPIO"
	if _, err := store.WriteActive(context.Background(), labelled); err != nil {
		t.Fatal(err)
	}
	active := store.LoadActive()
	if active.Profile == nil || active.Profile.Label != "Codex · PPIO" {
		t.Fatalf("onboarding label was not stored: %#v", active.Profile)
	}
	// A re-run must not rename what the user already named, even when the
	// request carries a different generated default.
	renamed := base
	renamed.Label = "Something Else"
	if _, err := store.WriteActive(context.Background(), renamed); err != nil {
		t.Fatal(err)
	}
	if active = store.LoadActive(); active.Profile.Label != "Codex · PPIO" {
		t.Fatalf("existing label was overwritten: %q", active.Profile.Label)
	}
	// Without a label the id remains the fallback, as before.
	if _, err := store.WriteActive(context.Background(), ActiveRequest{
		ProfileID: "plain", Configure: true, Provider: "ppio", Model: "model-a",
	}); err != nil {
		t.Fatal(err)
	}
	if active = store.LoadActive(); active.Profile.Label != "plain" {
		t.Fatalf("unlabelled profile = %q", active.Profile.Label)
	}
}
