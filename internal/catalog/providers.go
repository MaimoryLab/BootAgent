package catalog

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	bootagent "github.com/MaimoryLab/BootAgent"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const ProviderSchemaVersion = 1

// Provider definitions are embedded build-time data. A malformed lock file
// fails package initialization rather than silently leaving the app without
// endpoints.
var providerDefinitions, defaultFallbackProbeModel = mustLoadEmbeddedProviders()

type providerFileEntry struct {
	Name string `json:"name"`
	Home string `json:"home"`
	// Where a signed-in user creates or copies a key. Distinct from Home,
	// which is the marketing site and stays the target of the "Website" link.
	KeyManagementURL string `json:"key_management_url"`
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	// DefaultModel is user-facing: it pre-fills the model field so a first-time
	// user does not have to invent a model ID. FallbackProbeModel is internal
	// and answers a different question -- the cheapest model that proves the
	// endpoint and key work. They are deliberately separate fields because the
	// cheapest model to test with is not the best one to hand someone for
	// coding, and neither should silently become the other.
	DefaultModel       string `json:"default_model"`
	FallbackProbeModel string `json:"fallback_probe_model"`
}

type providerFile struct {
	SchemaVersion             int                          `json:"schema_version"`
	DefaultFallbackProbeModel string                       `json:"default_fallback_probe_model"`
	Providers                 map[string]providerFileEntry `json:"providers"`
}

func LoadEmbeddedProviders() (ProviderManifest, error) {
	data, err := bootagent.EmbeddedProviderLock()
	if err != nil {
		return ProviderManifest{}, oneerrors.New(
			oneerrors.InvalidRequest,
			"Cannot load embedded Provider lock manifest",
			oneerrors.WithCause(err),
		)
	}
	return ParseProviders(data)
}

func ParseProviders(data []byte) (ProviderManifest, error) {
	var source providerFile
	if err := json.Unmarshal(data, &source); err != nil {
		return ProviderManifest{}, oneerrors.New(
			oneerrors.InvalidRequest,
			fmt.Sprintf("Cannot load Provider lock manifest: %v", err),
			oneerrors.WithCause(err),
		)
	}
	if err := validateProviders(source); err != nil {
		return ProviderManifest{}, err
	}
	manifest := ProviderManifest{
		SchemaVersion:             source.SchemaVersion,
		DefaultFallbackProbeModel: source.DefaultFallbackProbeModel,
		Providers:                 make(map[string]Provider, len(source.Providers)),
	}
	for id, entry := range source.Providers {
		manifest.Providers[id] = Provider{
			Name:             entry.Name,
			Home:             entry.Home,
			KeyManagementURL: entry.KeyManagementURL,
			BaseURL:          entry.BaseURL,
			AnthropicBaseURL: entry.AnthropicBaseURL,
			DefaultModel:     entry.DefaultModel,
			fallbackModel:    entry.FallbackProbeModel,
		}
	}
	return cloneProviderManifest(manifest), nil
}

func validateProviders(source providerFile) error {
	if source.SchemaVersion != ProviderSchemaVersion || source.Providers == nil {
		return oneerrors.New(oneerrors.InvalidRequest, "Unsupported Provider lock manifest schema")
	}
	if len(source.Providers) == 0 {
		return oneerrors.New(oneerrors.InvalidRequest, "Provider lock manifest has no Providers")
	}
	if strings.TrimSpace(source.DefaultFallbackProbeModel) == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "Provider lock manifest has no default fallback probe model")
	}
	for id, entry := range source.Providers {
		if !agentIDPattern.MatchString(id) || id == "custom" {
			return invalidProvider(id, "invalid Provider ID")
		}
		if strings.TrimSpace(entry.Name) == "" {
			return invalidProvider(id, "name is required")
		}
		if !httpsURL(entry.Home) || !httpsURL(entry.BaseURL) {
			return invalidProvider(id, "home and base_url must use HTTPS")
		}
		if entry.AnthropicBaseURL != "" && !httpsURL(entry.AnthropicBaseURL) {
			return invalidProvider(id, "anthropic_base_url must use HTTPS")
		}
		// Optional: a built-in Provider without a dedicated key page falls back
		// to Home. Required to be HTTPS when present, like every other URL here,
		// because this one is opened in the user's browser.
		if entry.KeyManagementURL != "" && !httpsURL(entry.KeyManagementURL) {
			return invalidProvider(id, "key_management_url must use HTTPS")
		}
		// Required, unlike KeyManagementURL: this is the field that spares a
		// first-time user from inventing a model ID, and a built-in Provider
		// that cannot name one working model is not ready to ship.
		if strings.TrimSpace(entry.DefaultModel) == "" {
			return invalidProvider(id, "default_model is required")
		}
		if strings.TrimSpace(entry.FallbackProbeModel) == "" {
			return invalidProvider(id, "fallback_probe_model is required")
		}
	}
	return nil
}

func invalidProvider(providerID, message string) error {
	return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Invalid Provider lock manifest entry %s: %s", providerID, message))
}

func cloneProviderManifest(source ProviderManifest) ProviderManifest {
	result := source
	result.Providers = make(map[string]Provider, len(source.Providers))
	maps.Copy(result.Providers, source.Providers)
	return result
}

func mustLoadEmbeddedProviders() (map[string]Provider, string) {
	manifest, err := LoadEmbeddedProviders()
	if err != nil {
		panic(err)
	}
	return manifest.Providers, manifest.DefaultFallbackProbeModel
}
