// Package profile reads and writes the on-disk profile store without exposing
// secret contents. Agent configuration and installation remain separate.
package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

var profileIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type Store struct {
	Home string
	OS   string
	FS   *securefs.Store
	Now  func() time.Time
}

type Profile struct {
	SchemaVersion int
	ID            string
	Label         string
	Provider      string
	BaseURL       *string
	Model         *string
	ConfigMode    string
	Protocol      string
	CreatedAt     string
	ActivatedAt   *string
}

type Summary struct {
	ID          string
	Label       string
	Provider    string
	BaseURL     *string
	Model       *string
	Protocol    string
	ActivatedAt *string
	CreatedAt   string
}

type ActiveResult struct {
	Profile     *Profile
	Environment map[string]any
	ID          string
	Error       string
}

type storedProfile struct {
	SchemaVersion int     `json:"schema_version"`
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	Provider      string  `json:"provider"`
	BaseURL       *string `json:"base_url,omitempty"`
	Model         *string `json:"model"`
	ConfigMode    string  `json:"config_mode"`
	Protocol      string  `json:"protocol,omitempty"`
	CreatedAt     string  `json:"created_at"`
	ActivatedAt   *string `json:"activated_at"`
}

// activePointer is deliberately a struct rather than a map so the persisted
// field order stays stable across activations.
type activePointer struct {
	SchemaVersion int    `json:"schema_version"`
	Active        string `json:"active"`
}

func NewStore(home, osID string) Store {
	filesystem := securefs.New(securefs.Options{OS: osID})
	return Store{Home: home, OS: osID, FS: &filesystem, Now: time.Now}
}

func ValidateID(id string) error {
	if !profileIDPattern.MatchString(id) {
		return oneerrors.New(oneerrors.InvalidRequest, "Profile ID must start with a lowercase letter or digit and use only lowercase letters, digits, '-' or '_'")
	}
	return nil
}

func (s Store) Root() string {
	return filepath.Join(s.Home, ".oneagent")
}

func (s Store) PointerPath() string {
	return filepath.Join(s.Root(), "profile.json")
}

func (s Store) ProfilesPath() string {
	return filepath.Join(s.Root(), "profiles")
}

func (s Store) ProfilePath(id string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.ProfilesPath(), id+".json"), nil
}

func (s Store) SecretPath(id string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	suffix := "env"
	if s.OS == "windows" {
		suffix = "env.ps1"
	}
	return filepath.Join(s.Root(), "secrets", id+"."+suffix), nil
}

func (s Store) List() ([]Profile, error) {
	entries, err := os.ReadDir(s.ProfilesPath())
	if os.IsNotExist(err) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list Profiles: %w", err)
	}
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.ProfilesPath(), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cannot read Profile %s: %w", strings.TrimSuffix(entry.Name(), ".json"), err)
		}
		profile, err := decodeStored(data)
		if err != nil {
			return nil, fmt.Errorf("Profile %s is corrupt: %w", strings.TrimSuffix(entry.Name(), ".json"), err)
		}
		if err := ValidateID(profile.ID); err != nil {
			return nil, fmt.Errorf("Profile %s is corrupt: %w", strings.TrimSuffix(entry.Name(), ".json"), err)
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (s Store) LoadActive() ActiveResult {
	return s.LoadActiveContext(context.Background())
}

func (s Store) LoadActiveContext(ctx context.Context) ActiveResult {
	if err := requestContext(ctx); err != nil {
		return ActiveResult{Error: err.Error()}
	}
	data, err := os.ReadFile(s.PointerPath())
	if os.IsNotExist(err) {
		return ActiveResult{}
	}
	if err != nil {
		return ActiveResult{Error: err.Error()}
	}
	var pointer activePointer
	if err := json.Unmarshal(data, &pointer); err != nil {
		return ActiveResult{Error: err.Error()}
	}
	if pointer.SchemaVersion != 2 {
		return ActiveResult{Error: "Unsupported environment profile schema"}
	}
	active := pointer.Active
	if active == "" {
		return ActiveResult{Error: "Unsupported environment profile schema"}
	}
	if err := ValidateID(active); err != nil {
		return ActiveResult{Error: err.Error()}
	}
	result := ActiveResult{ID: active}
	path, err := s.ProfilePath(active)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	data, err = os.ReadFile(path)
	if os.IsNotExist(err) {
		result.Error = fmt.Sprintf("Profile %s is missing", active)
		return result
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	profile, err := decodeStored(data)
	if err != nil {
		result.Error = fmt.Sprintf("Profile %s is corrupt", active)
		return result
	}
	if profile.ID == "" || profile.ID != active {
		result.Error = fmt.Sprintf("Profile %s is corrupt", active)
		return result
	}
	if err := ValidateID(profile.ID); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Profile = &profile
	result.Environment = profile.environment()
	return result
}

func (p Profile) Summary() Summary {
	return Summary{
		ID:          p.ID,
		Label:       valueOr(p.Label, p.ID),
		Provider:    p.Provider,
		BaseURL:     nil,
		Model:       p.Model,
		Protocol:    p.Protocol,
		ActivatedAt: p.ActivatedAt,
		CreatedAt:   p.CreatedAt,
	}
}

func (p Profile) environment() map[string]any {
	return map[string]any{
		"schema_version": p.SchemaVersion,
		"id":             p.ID,
		"label":          p.Label,
		"provider":       p.Provider,
		"model":          optionalStringValue(p.Model),
		"config_mode":    p.ConfigMode,
		"protocol":       p.Protocol,
		"created_at":     p.CreatedAt,
		"activated_at":   optionalStringValue(p.ActivatedAt),
	}
}

func decodeStored(data []byte) (Profile, error) {
	var stored storedProfile
	if err := json.Unmarshal(data, &stored); err != nil {
		return Profile{}, err
	}
	if stored.ID == "" {
		return Profile{}, fmt.Errorf("profile has no id")
	}
	if stored.SchemaVersion != 2 {
		return Profile{}, fmt.Errorf("Unsupported profile schema")
	}
	return Profile{
		SchemaVersion: stored.SchemaVersion,
		ID:            stored.ID,
		Label:         stored.Label,
		Provider:      stored.Provider,
		// Legacy base_url is intentionally ignored. Provider endpoints are resolved
		// from the referenced Provider at use time.
		BaseURL:     nil,
		Model:       stored.Model,
		ConfigMode:  stored.ConfigMode,
		Protocol:    stored.Protocol,
		CreatedAt:   stored.CreatedAt,
		ActivatedAt: stored.ActivatedAt,
	}, nil
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
