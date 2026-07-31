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
	"sync"
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
	mu   *sync.Mutex
}

type Profile struct {
	SchemaVersion int
	ID            string
	Label         string
	Provider      string
	BaseURL       *string
	Model         *string
	ConfigMode    string
	AgentIDs      []string
	CreatedAt     string
	ActivatedAt   *string
	HasKey        bool
}

type Summary struct {
	ID          string
	Label       string
	Provider    string
	BaseURL     *string
	Model       *string
	AgentIDs    []string
	ActivatedAt *string
	HasKey      bool
}

type ActiveResult struct {
	Profile     *Profile
	Environment map[string]any
	ID          string
	Error       string
}

type storedProfile struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Provider      string   `json:"provider"`
	BaseURL       *string  `json:"base_url"`
	Model         *string  `json:"model"`
	ConfigMode    string   `json:"config_mode"`
	AgentIDs      []string `json:"agent_ids"`
	CreatedAt     string   `json:"created_at"`
	ActivatedAt   *string  `json:"activated_at"`
}

// activePointer is deliberately a struct rather than a map so the persisted
// field order stays stable across activations.
type activePointer struct {
	SchemaVersion int    `json:"schema_version"`
	Active        string `json:"active"`
}

func NewStore(home, osID string) Store {
	filesystem := securefs.New(securefs.Options{OS: osID})
	return Store{Home: home, OS: osID, FS: &filesystem, Now: time.Now, mu: &sync.Mutex{}}
}

func NewStoreWithDependencies(home, osID string, filesystem securefs.Store, now func() time.Time) Store {
	if now == nil {
		now = time.Now
	}
	return Store{Home: home, OS: osID, FS: &filesystem, Now: now, mu: &sync.Mutex{}}
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

func (s Store) List() []Profile {
	entries, err := os.ReadDir(s.ProfilesPath())
	if err != nil {
		return []Profile{}
	}
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.ProfilesPath(), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		profile, err := decodeStored(data)
		if err != nil || ValidateID(profile.ID) != nil {
			continue
		}
		secret, err := s.SecretPath(profile.ID)
		if err == nil {
			_, statErr := os.Stat(secret)
			profile.HasKey = statErr == nil
		}
		profiles = append(profiles, profile)
	}
	return profiles
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
	var pointer map[string]any
	if err := json.Unmarshal(data, &pointer); err != nil {
		return ActiveResult{Error: err.Error()}
	}
	schema, ok := integerField(pointer["schema_version"])
	if !ok {
		return ActiveResult{Error: "Unsupported environment profile schema"}
	}
	if schema == 1 {
		profile := migrateLegacy(pointer)
		profile, err = s.persistLegacy(ctx, profile)
		if err != nil {
			return ActiveResult{Error: fmt.Sprintf("Cannot migrate legacy profile: %v", err)}
		}
		return ActiveResult{Profile: &profile, Environment: profile.environment(), ID: profile.ID}
	}
	if schema != 2 {
		return ActiveResult{Error: "Unsupported environment profile schema"}
	}
	active, ok := pointer["active"].(string)
	if !ok {
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

func (s Store) persistLegacy(ctx context.Context, profile Profile) (Profile, error) {
	if s.mu != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	stored := storedProfile{
		SchemaVersion: 2,
		ID:            profile.ID,
		Label:         profile.Label,
		Provider:      profile.Provider,
		BaseURL:       profile.BaseURL,
		Model:         profile.Model,
		ConfigMode:    profile.ConfigMode,
		AgentIDs:      cloneStrings(profile.AgentIDs),
		CreatedAt:     profile.CreatedAt,
		ActivatedAt:   profile.ActivatedAt,
	}
	if stored.CreatedAt == "" {
		stored.CreatedAt = s.clock().UTC().Format(time.RFC3339)
		profile.CreatedAt = stored.CreatedAt
	}
	if stored.ActivatedAt == nil {
		stored.ActivatedAt = stringPointer(stored.CreatedAt)
		profile.ActivatedAt = stored.ActivatedAt
	}
	if err := s.writeStored(ctx, stored); err != nil {
		return Profile{}, err
	}
	pointer := activePointer{SchemaVersion: 2, Active: stored.ID}
	data, err := json.MarshalIndent(pointer, "", "  ")
	if err != nil {
		return Profile{}, writeError("Cannot encode migrated profile pointer: %v", err)
	}
	data = append(data, '\n')
	_, err = s.filesystem().AtomicWrite(ctx, s.PointerPath(), data, false)
	if err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (p Profile) Summary() Summary {
	return Summary{
		ID:          p.ID,
		Label:       valueOr(p.Label, p.ID),
		Provider:    p.Provider,
		BaseURL:     p.BaseURL,
		Model:       p.Model,
		AgentIDs:    cloneStrings(p.AgentIDs),
		ActivatedAt: p.ActivatedAt,
		HasKey:      p.HasKey,
	}
}

func (p Profile) environment() map[string]any {
	return map[string]any{
		"schema_version": p.SchemaVersion,
		"id":             p.ID,
		"label":          p.Label,
		"provider":       p.Provider,
		"base_url":       optionalStringValue(p.BaseURL),
		"model":          optionalStringValue(p.Model),
		"config_mode":    p.ConfigMode,
		"agent_ids":      cloneStrings(p.AgentIDs),
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
		BaseURL:       stored.BaseURL,
		Model:         stored.Model,
		ConfigMode:    stored.ConfigMode,
		AgentIDs:      cloneStrings(stored.AgentIDs),
		CreatedAt:     stored.CreatedAt,
		ActivatedAt:   stored.ActivatedAt,
	}, nil
}

func migrateLegacy(pointer map[string]any) Profile {
	activated, _ := pointer["activated_at"].(string)
	configMode, _ := pointer["config_mode"].(string)
	if configMode == "" {
		configMode = "provider"
	}
	return Profile{
		SchemaVersion: 2,
		ID:            "default",
		Label:         "default",
		Provider:      stringField(pointer["provider"]),
		BaseURL:       pointerString(pointer["base_url"]),
		Model:         pointerString(pointer["model"]),
		ConfigMode:    configMode,
		AgentIDs:      stringSlice(pointer["agent_ids"]),
		CreatedAt:     activated,
		ActivatedAt:   stringPointer(activated),
	}
}

func integerField(value any) (int, bool) {
	number, ok := value.(float64)
	return int(number), ok && number == float64(int(number))
}

func stringField(value any) string {
	valueString, _ := value.(string)
	return valueString
}

func pointerString(value any) *string {
	if value == nil {
		return nil
	}
	valueString, ok := value.(string)
	if !ok {
		return nil
	}
	return &valueString
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if valueString, ok := item.(string); ok {
			result = append(result, valueString)
		}
	}
	return result
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string{}, values...)
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
