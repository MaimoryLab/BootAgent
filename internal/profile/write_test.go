package profile

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

func testStore(t *testing.T, home, osID string) Store {
	t.Helper()
	filesystem := securefs.New(securefs.Options{OS: osID, Now: fixedProfileClock})
	return Store{Home: home, OS: osID, FS: &filesystem, Now: fixedProfileClock}
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
	if profile.ID != "ppio-deepseek" || profile.Protocol != "openai" {
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
	if got, err := store.List(); err != nil || len(got) != 1 {
		t.Fatalf("List() = %#v, %v", got, err)
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
	if updated.Label != "Team PPIO" || updated.CreatedAt != created || updated.Model == nil || *updated.Model != "model-b" {
		t.Fatalf("updated profile = %#v", updated)
	}
	if _, err := store.Save(context.Background(), SaveRequest{
		ID: "ppio-deepseek", Provider: "novita", Model: "model-b", Protocol: "openai",
	}); err != nil {
		t.Fatalf("provider change without a local key failed: %v", err)
	}
	assertProfileMode(t, profilePath, 0o600)
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
	if err != nil || profile.BaseURL != nil {
		t.Fatalf("custom profile = %#v, err=%v", profile, err)
	}
	profilePath, _ := store.ProfilePath("custom-local")
	data, readErr := os.ReadFile(profilePath)
	if readErr != nil || strings.Contains(string(data), "base_url") {
		t.Fatalf("saved profile retained base_url: %s, %v", data, readErr)
	}
}

func TestDeleteProfileRemovesRecordSecretAndActivePointer(t *testing.T) {
	store := testStore(t, t.TempDir(), "linux")
	if _, err := store.WriteActive(context.Background(), ActiveRequest{
		ProfileID: "team", Configure: true, Provider: "ppio", Model: "m", APIKey: "sk", Protocol: "openai",
	}); err != nil {
		t.Fatal(err)
	}
	profilePath, _ := store.ProfilePath("team")
	secretPath, _ := store.SecretPath("team")
	if err := store.Delete(context.Background(), "team"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{profilePath, secretPath, store.PointerPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", path, err)
		}
	}
	// Deleting it again succeeds. This asserted an InvalidRequest until the
	// double-clicked delete button in the UI showed what that costs: the second
	// click reported "Unknown Profile" for a Profile the user had just deleted
	// successfully. Absence is the requested end state, so reaching it twice is
	// not a failure.
	if err := store.Delete(context.Background(), "team"); err != nil {
		t.Fatalf("deleting an already-deleted Profile returned %v, want nil", err)
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
	if active.Profile.BaseURL != nil {
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
