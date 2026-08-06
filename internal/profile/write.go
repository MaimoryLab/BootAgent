package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/provider"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

type SaveRequest struct {
	ID       string
	Label    string
	Provider string
	BaseURL  string
	APIKey   string
	// ProviderKeyAvailable lets the app change a Profile's Provider without
	// requiring a duplicate key when the Provider already has one.
	ProviderKeyAvailable bool
	Model                string
	ConfigMode           string
	Protocol             string
}

type ActiveRequest struct {
	ProfileID string
	// Label names a profile this request creates. An existing profile keeps its
	// stored label so a re-activation never renames it behind the user's back.
	Label     string
	Configure bool
	Provider  string
	BaseURL   string
	Model     string
	APIKey    string
	Protocol  string
}

// Save stores a reusable profile template without changing the active
// pointer. Credentials are written to the sibling secret store only.
func (s Store) Save(ctx context.Context, request SaveRequest) (Profile, error) {
	if err := requestContext(ctx); err != nil {
		return Profile{}, err
	}
	if err := ValidateID(request.ID); err != nil {
		return Profile{}, err
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		return Profile{}, oneerrors.New(oneerrors.InvalidRequest, "model is required")
	}
	mode, err := configMode(request.ConfigMode)
	if err != nil {
		return Profile{}, err
	}
	base, err := provider.ProviderBase(request.Provider, request.BaseURL)
	if err != nil {
		return Profile{}, err
	}
	existing := s.existing(request.ID)
	if existing.ID != "" && existing.Provider != request.Provider && request.APIKey == "" && !request.ProviderKeyAvailable && s.secretExists(request.ID) {
		return Profile{}, oneerrors.New(oneerrors.InvalidRequest, "API key is required when changing a Profile provider")
	}
	now := s.clock().UTC().Format(time.RFC3339)
	label := strings.TrimSpace(request.Label)
	if label == "" {
		label = valueOr(existing.Label, request.ID)
	}
	created := existing.CreatedAt
	if created == "" {
		created = now
	}
	stored := storedProfile{
		SchemaVersion: 2,
		ID:            request.ID,
		Label:         label,
		Provider:      request.Provider,
		BaseURL:       optionalPointer(request.BaseURL, base),
		Model:         stringPointer(model),
		ConfigMode:    mode,
		Protocol:      strings.TrimSpace(request.Protocol),
		CreatedAt:     created,
		ActivatedAt:   existing.ActivatedAt,
	}
	if err := s.writeStored(ctx, stored); err != nil {
		return Profile{}, err
	}
	if request.APIKey != "" && mode == "provider" {
		if err := s.writeSecret(ctx, request.ID, request.APIKey, base); err != nil {
			return Profile{}, err
		}
	}
	profile := profileFromStored(stored)
	profile.HasKey = s.secretExists(request.ID)
	return profile, nil
}

// Delete removes a Profile and its sibling secret. Deleting the active Profile
// also clears the active pointer so status returns to an unconfigured state.
func (s Store) Delete(ctx context.Context, id string) error {
	if err := requestContext(ctx); err != nil {
		return err
	}
	if err := ValidateID(id); err != nil {
		return err
	}
	profilePath, _ := s.ProfilePath(id)
	if err := os.Remove(profilePath); err != nil {
		if os.IsNotExist(err) {
			return oneerrors.New(oneerrors.InvalidRequest, "Unknown Profile: "+id)
		}
		return writeError("Cannot delete Profile %s: %v", id, err)
	}
	if secretPath, err := s.SecretPath(id); err == nil {
		if err := os.Remove(secretPath); err != nil && !os.IsNotExist(err) {
			return writeError("Cannot delete Profile secret %s: %v", id, err)
		}
	}
	active := s.LoadActive()
	if active.ID == id {
		if err := os.Remove(s.PointerPath()); err != nil && !os.IsNotExist(err) {
			return writeError("Cannot clear active Profile: %v", err)
		}
	}
	return nil
}

// WriteActive updates the v2 profile record and active pointer used by the
// installation workflow.
func (s Store) WriteActive(ctx context.Context, request ActiveRequest) (string, error) {
	if err := requestContext(ctx); err != nil {
		return "", err
	}
	currentResult := s.LoadActive()
	current := Profile{}
	if currentResult.Profile != nil {
		current = *currentResult.Profile
	}
	profileID := strings.TrimSpace(request.ProfileID)
	if profileID != "" {
		if err := ValidateID(profileID); err != nil {
			return "", err
		}
		current = s.existing(profileID)
	} else {
		profileID = "default"
		if current.ID != "" {
			profileID = current.ID
		}
	}
	var baseURL *string
	var model *string
	resolvedBase := ""
	providerID := "existing-account"
	mode := "existing-account"
	if request.Configure {
		base, providerErr := provider.ProviderBase(request.Provider, request.BaseURL)
		if providerErr != nil {
			return "", providerErr
		}
		resolvedBase = base
		providerID = request.Provider
		mode = "provider"
		baseURL = stringPointer(base)
		modelValue := strings.TrimSpace(request.Model)
		if modelValue == "" {
			return "", oneerrors.New(oneerrors.InvalidRequest, "model is required")
		}
		model = stringPointer(modelValue)
	}
	now := s.clock().UTC().Format(time.RFC3339)
	created := current.CreatedAt
	if created == "" {
		created = now
	}
	activated := stringPointer(now)
	stored := storedProfile{
		SchemaVersion: 2,
		ID:            profileID,
		Label:         valueOr(current.Label, valueOr(strings.TrimSpace(request.Label), profileID)),
		Provider:      providerID,
		BaseURL:       baseURL,
		Model:         model,
		ConfigMode:    mode,
		Protocol:      strings.TrimSpace(request.Protocol),
		CreatedAt:     created,
		ActivatedAt:   activated,
	}
	if err := s.writeStored(ctx, stored); err != nil {
		return "", err
	}
	if request.Configure && request.APIKey != "" {
		if err := s.writeSecret(ctx, profileID, request.APIKey, resolvedBase); err != nil {
			return "", err
		}
	}
	pointer := activePointer{SchemaVersion: 2, Active: profileID}
	data, err := json.MarshalIndent(pointer, "", "  ")
	if err != nil {
		return "", writeError("Cannot encode active profile: %v", err)
	}
	data = append(data, '\n')
	if _, err := s.filesystem().AtomicWrite(ctx, s.PointerPath(), data, false); err != nil {
		return "", err
	}
	return s.PointerPath(), nil
}

func (s Store) writeStored(ctx context.Context, stored storedProfile) error {
	path, err := s.ProfilePath(stored.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return writeError("Cannot encode profile %s: %v", stored.ID, err)
	}
	data = append(data, '\n')
	_, err = s.filesystem().AtomicWrite(ctx, path, data, false)
	return err
}

func (s Store) writeSecret(ctx context.Context, id, apiKey, base string) error {
	path, err := s.SecretPath(id)
	if err != nil {
		return err
	}
	content := secretContent(s.OS, apiKey, base)
	_, err = s.filesystem().AtomicWrite(ctx, path, []byte(content), true)
	return err
}

func (s Store) existing(id string) Profile {
	path, err := s.ProfilePath(id)
	if err != nil {
		return Profile{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}
	}
	profile, err := decodeStored(data)
	if err != nil {
		return Profile{}
	}
	return profile
}

func (s Store) secretExists(id string) bool {
	path, err := s.SecretPath(id)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (s Store) filesystem() securefs.Store {
	if s.FS != nil {
		return *s.FS
	}
	return securefs.New(securefs.Options{OS: s.OS})
}

func (s Store) clock() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func profileFromStored(stored storedProfile) Profile {
	return Profile{
		SchemaVersion: stored.SchemaVersion,
		ID:            stored.ID,
		Label:         stored.Label,
		Provider:      stored.Provider,
		BaseURL:       stored.BaseURL,
		Model:         stored.Model,
		ConfigMode:    stored.ConfigMode,
		Protocol:      stored.Protocol,
		CreatedAt:     stored.CreatedAt,
		ActivatedAt:   stored.ActivatedAt,
	}
}

func configMode(value string) (string, error) {
	if value == "" {
		return "provider", nil
	}
	if value != "provider" && value != "existing-account" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "config_mode must be provider or existing-account")
	}
	return value, nil
}

func optionalPointer(input, resolved string) *string {
	if input == "" {
		return nil
	}
	return stringPointer(resolved)
}

func secretContent(osID, apiKey, base string) string {
	if osID == "windows" {
		return "$env:ONEAGENT_API_KEY = '" + powershellQuote(apiKey) + "'\n" +
			"$env:ONEAGENT_API_BASE_URL = '" + powershellQuote(base) + "'\n"
	}
	return "export ONEAGENT_API_KEY=" + shellQuote(apiKey) + "\n" +
		"export ONEAGENT_API_BASE_URL=" + shellQuote(base) + "\n"
}

func shellQuote(value string) string {
	if value != "" {
		safe := true
		for _, character := range value {
			if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-", character) {
				safe = false
				break
			}
		}
		if safe {
			return value
		}
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func powershellQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func requestContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return oneerrors.New(oneerrors.Timeout, "Profile request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

func writeError(format string, values ...any) error {
	return oneerrors.New(oneerrors.ConfigWriteFailed, fmt.Sprintf(format, values...))
}
