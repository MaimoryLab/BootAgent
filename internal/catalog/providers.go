package catalog

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"

	oneagent "github.com/MaimoryLab/OneAgent"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

const ProviderSchemaVersion = 1

// Provider definitions are embedded build-time data. A malformed lock file
// fails package initialization rather than silently leaving the app without
// endpoints.
var providerDefinitions, defaultFallbackProbeModel = mustLoadEmbeddedProviders()

type providerFileEntry struct {
	Name               string `json:"name"`
	Home               string `json:"home"`
	BaseURL            string `json:"base_url"`
	AnthropicBaseURL   string `json:"anthropic_base_url"`
	FallbackProbeModel string `json:"fallback_probe_model"`
}

type providerFile struct {
	SchemaVersion             int                          `json:"schema_version"`
	DefaultFallbackProbeModel string                       `json:"default_fallback_probe_model"`
	Providers                 map[string]providerFileEntry `json:"providers"`
}

func LoadEmbeddedProviders() (ProviderManifest, error) {
	data, err := oneagent.EmbeddedProviderLock()
	if err != nil {
		return ProviderManifest{}, oneerrors.New(
			oneerrors.InvalidRequest,
			"Cannot load embedded Provider lock manifest",
			oneerrors.WithCause(err),
		)
	}
	return ParseProviders(data)
}

func LoadProviders(path string) (ProviderManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProviderManifest{}, oneerrors.New(
			oneerrors.InvalidRequest,
			fmt.Sprintf("Cannot load Provider lock manifest: %v", err),
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
			BaseURL:          entry.BaseURL,
			AnthropicBaseURL: entry.AnthropicBaseURL,
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
