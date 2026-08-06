package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

const userProviderSchemaVersion = 1

var userProviderIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Entry is the editable local Provider record. APIKey is returned only by the
// explicit Provider CRUD service; status projections expose HasKey instead.
type Entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Home string `json:"home"`
	// Both derived from the built-in catalog, not user-editable: a user who adds
	// a Provider supplies Home, and a custom endpoint has no model we can vouch
	// for. Serialized so the frontend can pre-fill the model field and aim the
	// key button at a key page instead of a marketing site.
	KeyManagementURL string `json:"key_management_url,omitempty"`
	DefaultModel     string `json:"default_model,omitempty"`
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	APIKey           string `json:"api_key"`
	BuiltIn          bool   `json:"built_in"`
	// Excluded from JSON: the probe model is an internal detail and must not
	// reach the frontend, where it would read as a recommendation.
	FallbackModel string `json:"-"`
}

type storedProvider struct {
	Name             string `json:"name"`
	Home             string `json:"home,omitempty"`
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url,omitempty"`
	APIKey           string `json:"api_key,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
}

type userProviderFile struct {
	SchemaVersion int                       `json:"schema_version"`
	Providers     map[string]storedProvider `json:"providers"`
}

type Store struct {
	home       string
	filesystem securefs.Store
}

func NewStore(home string, filesystem securefs.Store) Store {
	return Store{home: home, filesystem: filesystem}
}

func (s Store) Path() string {
	return filepath.Join(s.home, ".oneagent", "providers.json")
}

func (s Store) Get(id string) (Entry, error) {
	id = strings.TrimSpace(id)
	file, err := s.load()
	if err != nil {
		return Entry{}, err
	}
	if saved, ok := file.Providers[id]; ok {
		_, builtIn := catalog.ProviderByID(id)
		return entryFromStored(id, saved, builtIn), nil
	}
	if builtIn, ok := catalog.ProviderByID(id); ok {
		return Entry{
			ID: id, Name: builtIn.Name, Home: builtIn.Home, BaseURL: builtIn.BaseURL,
			AnthropicBaseURL: builtIn.AnthropicBaseURL, BuiltIn: true,
			KeyManagementURL: catalog.KeyManagementURL(id),
			DefaultModel:     catalog.DefaultModel(id),
			FallbackModel:    catalog.FallbackProbeModel(id),
		}, nil
	}
	return Entry{}, oneerrors.New(oneerrors.InvalidRequest, "Unknown Provider: "+id)
}

func (s Store) Resolve(id, explicitBase string) (Entry, error) {
	id = strings.TrimSpace(id)
	if id == "custom" {
		base, err := ValidateBaseURL(strings.TrimSpace(explicitBase))
		if err != nil {
			return Entry{}, err
		}
		// No DefaultModel: "custom" is an endpoint we know nothing about, so the
		// user still has to name a model. FallbackModel resolves to the manifest
		// default, which is only ever used to probe.
		return Entry{ID: id, Name: "Custom", BaseURL: base, FallbackModel: catalog.FallbackProbeModel(id)}, nil
	}
	entry, err := s.Get(id)
	if err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(explicitBase) != "" {
		entry.BaseURL, err = ValidateBaseURL(strings.TrimSpace(explicitBase))
		entry.AnthropicBaseURL = ""
	}
	return entry, err
}

func (entry Entry) BaseFor(protocol string) string {
	if protocol == ProtocolAnthropic && entry.AnthropicBaseURL != "" {
		return entry.AnthropicBaseURL
	}
	return entry.BaseURL
}

func (s Store) Public() (map[string]catalog.Provider, error) {
	result := catalog.PublicProviders()
	file, err := s.load()
	if err != nil {
		return nil, err
	}
	for id, saved := range file.Providers {
		_, builtIn := catalog.ProviderByID(id)
		result[id] = catalog.Provider{
			Name: saved.Name, Home: saved.Home, BaseURL: saved.BaseURL,
			AnthropicBaseURL: saved.AnthropicBaseURL, Custom: !builtIn, HasKey: saved.APIKey != "", CreatedAt: saved.CreatedAt,
			// This overlay replaces the catalog entry wholesale, so the manifest
			// fields have to be re-read here or a built-in Provider silently
			// loses its default model the moment the user saves a key against it.
			KeyManagementURL: catalog.KeyManagementURL(id),
			DefaultModel:     catalog.DefaultModel(id),
		}
	}
	return result, nil
}

func (s Store) Save(ctx context.Context, entry Entry) (Entry, error) {
	entry.ID = strings.TrimSpace(entry.ID)
	entry.Name = strings.TrimSpace(entry.Name)
	entry.Home = strings.TrimSpace(entry.Home)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.AnthropicBaseURL = strings.TrimSpace(entry.AnthropicBaseURL)
	if err := validateEntry(entry); err != nil {
		return Entry{}, err
	}
	file, err := s.load()
	if err != nil {
		return Entry{}, err
	}
	saved := file.Providers[entry.ID]
	if saved.CreatedAt == "" {
		saved.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	file.Providers[entry.ID] = storedProvider{
		Name: entry.Name, Home: entry.Home, BaseURL: entry.BaseURL,
		AnthropicBaseURL: entry.AnthropicBaseURL, APIKey: entry.APIKey, CreatedAt: saved.CreatedAt,
	}
	if err := s.write(ctx, file); err != nil {
		return Entry{}, err
	}
	_, entry.BuiltIn = catalog.ProviderByID(entry.ID)
	entry.KeyManagementURL = catalog.KeyManagementURL(entry.ID)
	entry.DefaultModel = catalog.DefaultModel(entry.ID)
	entry.FallbackModel = catalog.FallbackProbeModel(entry.ID)
	return entry, nil
}

func (s Store) SaveKey(ctx context.Context, id, apiKey string) error {
	if id == "custom" || apiKey == "" {
		return nil
	}
	entry, err := s.Get(id)
	if err != nil {
		return err
	}
	if entry.APIKey == apiKey {
		return nil
	}
	entry.APIKey = apiKey
	_, err = s.Save(ctx, entry)
	return err
}

func (s Store) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	file, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := file.Providers[id]; !ok {
		return oneerrors.New(oneerrors.InvalidRequest, "Unknown Provider: "+id)
	}
	delete(file.Providers, id)
	return s.write(ctx, file)
}

func (s Store) load() (userProviderFile, error) {
	file := userProviderFile{SchemaVersion: userProviderSchemaVersion, Providers: map[string]storedProvider{}}
	data, err := os.ReadFile(s.Path())
	if os.IsNotExist(err) {
		return file, nil
	}
	if err != nil {
		return file, oneerrors.New(oneerrors.ConfigWriteFailed, fmt.Sprintf("Cannot read saved Providers: %v", err))
	}
	if err := json.Unmarshal(data, &file); err != nil || file.SchemaVersion != userProviderSchemaVersion || file.Providers == nil {
		return userProviderFile{}, oneerrors.New(oneerrors.InvalidRequest, "Saved Provider file is invalid")
	}
	for id, saved := range file.Providers {
		_, builtIn := catalog.ProviderByID(id)
		if err := validateEntry(entryFromStored(id, saved, builtIn)); err != nil {
			return userProviderFile{}, oneerrors.New(oneerrors.InvalidRequest, "Saved Provider file is invalid")
		}
	}
	return file, nil
}

func (s Store) write(ctx context.Context, file userProviderFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot encode saved Providers")
	}
	_, err = s.filesystem.AtomicWrite(ctx, s.Path(), append(data, '\n'), true)
	return err
}

func validateEntry(entry Entry) error {
	if !userProviderIDPattern.MatchString(entry.ID) || entry.ID == "custom" {
		return oneerrors.New(oneerrors.InvalidRequest, "Provider ID must use lowercase letters, digits, or '-' and cannot be custom")
	}
	if entry.Name == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "Provider name is required")
	}
	if entry.BaseURL == "" && entry.AnthropicBaseURL == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "At least one API base URL is required")
	}
	if entry.BaseURL != "" {
		if _, err := ValidateBaseURL(entry.BaseURL); err != nil {
			return err
		}
	}
	if entry.Home != "" {
		if _, err := ValidateBaseURL(entry.Home); err != nil {
			return oneerrors.New(oneerrors.InvalidRequest, "Provider home URL is invalid")
		}
	}
	if entry.AnthropicBaseURL != "" {
		if _, err := ValidateBaseURL(entry.AnthropicBaseURL); err != nil {
			return err
		}
	}
	return nil
}

func entryFromStored(id string, saved storedProvider, builtIn bool) Entry {
	return Entry{
		ID: id, Name: saved.Name, Home: saved.Home, BaseURL: saved.BaseURL,
		AnthropicBaseURL: saved.AnthropicBaseURL, APIKey: saved.APIKey, BuiltIn: builtIn,
		// Read from the catalog rather than the stored record, so a user overlay
		// on a built-in Provider keeps the manifest's model and key page. Both
		// resolve to empty for a purely user-added Provider.
		KeyManagementURL: catalog.KeyManagementURL(id),
		DefaultModel:     catalog.DefaultModel(id),
		FallbackModel:    catalog.FallbackProbeModel(id),
	}
}
