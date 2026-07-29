package profile

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
)

// storeSchema is the current stored-profile version. Version 1 was a single
// profile.json holding the profile itself; version 2 made that file a pointer
// into profiles/ so more than one profile can exist.
const storeSchema = 2

// ReadStored returns one stored profile, or why it cannot be read. A missing
// profile is an error here rather than an empty result: the caller asked for a
// specific id, so silence would look like an empty profile.
func (s *Store) ReadStored(profileID string) (*jsonorder.Object, string, error) {
	path, err := StorePath(s.Runtime, profileID)
	if err != nil {
		return nil, "", err
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, "Profile " + profileID + " is missing", nil
		}
		return nil, readErr.Error(), nil
	}
	value, parseErr := jsonorder.Parse(raw)
	if parseErr != nil {
		return nil, parseErr.Error(), nil
	}
	return value, "", nil
}

// writeStored writes one stored profile.
func (s *Store) writeStored(profileID string, stored *jsonorder.Object) (string, error) {
	path, err := StorePath(s.Runtime, profileID)
	if err != nil {
		return "", err
	}
	if err := s.FS.EnsureDir(Dir(s.Runtime)); err != nil {
		return "", err
	}
	encoded, err := jsonorder.Marshal(stored)
	if err != nil {
		return "", oerr.Newf("INTERNAL_ERROR", "Cannot encode the profile: %v", err)
	}
	if _, err := s.FS.Write(path, string(encoded), false); err != nil {
		return "", err
	}
	return path, nil
}

// writePointer records which profile is active.
func (s *Store) writePointer(profileID string) (string, error) {
	if _, err := ValidateID(profileID); err != nil {
		return "", err
	}
	pointer := jsonorder.NewObject()
	pointer.Set("schema_version", storeSchema)
	pointer.Set("active", profileID)
	encoded, err := jsonorder.Marshal(pointer)
	if err != nil {
		return "", oerr.Newf("INTERNAL_ERROR", "Cannot encode the profile pointer: %v", err)
	}
	path := PointerPath(s.Runtime)
	if _, err := s.FS.Write(path, string(encoded), false); err != nil {
		return "", err
	}
	return path, nil
}

// ActiveID names the active profile, or "" when there is none.
//
// The pointer is a file on disk, so its contents are untrusted input even though
// we wrote it: the value reaches SecretPath, and anything able to edit
// profile.json could otherwise name a path outside the secrets directory. An
// illegal id is reported as no active profile rather than passed along.
func (s *Store) ActiveID() string {
	raw, err := os.ReadFile(PointerPath(s.Runtime))
	if err != nil {
		return ""
	}
	value, err := jsonorder.Parse(raw)
	if err != nil {
		return ""
	}
	schema, _ := value.Get("schema_version")
	if !isSchema(schema, storeSchema) {
		return ""
	}
	active := value.GetString("active")
	if active == "" {
		return ""
	}
	if _, err := ValidateID(active); err != nil {
		return ""
	}
	return active
}

// Load returns the active profile.
//
// A schema_version 1 file is migrated in place on first read -- the atomic write
// backs the original up first -- so an existing installation upgrades instead of
// failing.
func (s *Store) Load() (*jsonorder.Object, string, error) {
	raw, readErr := os.ReadFile(PointerPath(s.Runtime))
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, "", nil
		}
		return nil, readErr.Error(), nil
	}
	value, parseErr := jsonorder.Parse(raw)
	if parseErr != nil {
		return nil, parseErr.Error(), nil
	}
	schema, _ := value.Get("schema_version")
	if isSchema(schema, 1) {
		return s.migrateV1(value)
	}
	if !isSchema(schema, storeSchema) {
		return nil, "Unsupported environment profile schema", nil
	}
	active := value.GetString("active")
	if active == "" {
		return nil, "Unsupported environment profile schema", nil
	}
	if _, err := ValidateID(active); err != nil {
		return nil, messageOf(err), nil
	}
	return s.ReadStored(active)
}

// migrateV1 converts the single-profile layout into the store.
func (s *Store) migrateV1(legacy *jsonorder.Object) (*jsonorder.Object, string, error) {
	const profileID = "default"
	now := s.timestamp()
	activated := legacy.GetString("activated_at")
	if activated == "" {
		activated = now
	}
	configMode := legacy.GetString("config_mode")
	if configMode == "" {
		configMode = "provider"
	}

	stored := jsonorder.NewObject()
	stored.Set("schema_version", storeSchema)
	stored.Set("id", profileID)
	stored.Set("label", profileID)
	stored.Set("provider", passThrough(legacy, "provider"))
	stored.Set("base_url", passThrough(legacy, "base_url"))
	stored.Set("model", passThrough(legacy, "model"))
	stored.Set("config_mode", configMode)
	stored.Set("agent_ids", agentIDsOf(legacy))
	stored.Set("created_at", activated)
	stored.Set("activated_at", activated)

	if _, err := s.writeStored(profileID, stored); err != nil {
		return nil, "Cannot migrate legacy profile: " + messageOf(err), nil
	}
	if _, err := s.writePointer(profileID); err != nil {
		return nil, "Cannot migrate legacy profile: " + messageOf(err), nil
	}
	return stored, "", nil
}

// List returns the stored profiles in stable order, annotated with whether a key
// is held for each.
//
// A profile never carries key material, so whether the matching secret file
// exists is what decides if it can be activated without re-pasting the key. An
// unreadable file is skipped rather than failing the call.
func (s *Store) List() ([]*jsonorder.Object, error) {
	profiles := []*jsonorder.Object{}
	entries, err := os.ReadDir(Dir(s.Runtime))
	if err != nil {
		return profiles, nil
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(Dir(s.Runtime), name))
		if err != nil {
			continue
		}
		value, err := jsonorder.Parse(raw)
		if err != nil {
			continue
		}
		id := value.GetString("id")
		if id == "" {
			continue
		}
		value.Set("has_key", s.HasSecret(id))
		profiles = append(profiles, value)
	}
	return profiles, nil
}

// HasSecret reports whether a key is stored for a profile, without reading it.
func (s *Store) HasSecret(profileID string) bool {
	path, err := SecretPath(s.Runtime, profileID)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// passThrough copies a value unchanged, keeping JSON null as null rather than
// turning it into an empty string: the frontend distinguishes "no model recorded"
// from "the empty model".
func passThrough(object *jsonorder.Object, key string) any {
	value, present := object.Get(key)
	if !present {
		return nil
	}
	return value
}

// agentIDsOf reads the agent list, defaulting to empty rather than null so the
// field always encodes as an array.
func agentIDsOf(object *jsonorder.Object) []any {
	value, present := object.Get("agent_ids")
	if !present {
		return []any{}
	}
	items, ok := value.([]any)
	if !ok || items == nil {
		return []any{}
	}
	return items
}

// messageOf prefers a OneAgentError's message over the wrapped text, matching
// what Python reports for the same failure.
func messageOf(err error) string {
	var converted *oerr.Error
	if errors.As(err, &converted) {
		return converted.Message
	}
	return err.Error()
}
