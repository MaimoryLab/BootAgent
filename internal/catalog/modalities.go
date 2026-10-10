package catalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	bootagent "github.com/MaimoryLab/BootAgent"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const ModalitySchemaVersion = 1

var (
	modalityOnce     sync.Once
	modalityManifest ModalityManifest
	modalityErr      error
)

// ModalityManifest is modalities.lock.json: ID rules that map a model ID to
// the capabilities it most likely has, plus per Provider the native route and
// the generation models /models does not list. The capability and adapter
// vocabulary is internal/modality's, which validates it on load; this parser
// checks structure only.
type ModalityManifest struct {
	SchemaVersion int                         `json:"schema_version"`
	IDRules       []ModalityIDRule            `json:"id_rules"`
	Providers     map[string]ModalityProvider `json:"providers"`
}

// ModalityIDRule matches the lower-cased model ID with any vendor/ prefix
// removed. Exclude, when set, vetoes a match.
type ModalityIDRule struct {
	Pattern      string   `json:"pattern"`
	Exclude      string   `json:"exclude,omitempty"`
	Capabilities []string `json:"capabilities"`
}

type ModalityProvider struct {
	// NativeBaseURL is the host root native async and audio routes hang off. It
	// is listed separately because the catalog's base_url is the OpenAI route
	// (https://api.novita.ai/openai), which is not where those routes live.
	NativeBaseURL string                    `json:"native_base_url"`
	Generation    []ModalityGenerationModel `json:"generation"`
	// Unsupported names capabilities this Provider is known not to offer
	// through any documented API, so the UI can say so instead of showing
	// nothing.
	Unsupported []string `json:"unsupported,omitempty"`
}

// ModalityGenerationModel is a model only the catalog knows about: the async
// generation and audio models are not in any /models listing.
type ModalityGenerationModel struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
	Adapter      string   `json:"adapter"`
	// Options are the request fields the model documents beyond the prompt,
	// from the adapter's option vocabulary. The generated script refuses any
	// other.
	Options []string `json:"options,omitempty"`
	// Recommended marks the model preselected for its capability on this
	// Provider, usually the cheaper of two that do the same job.
	Recommended bool `json:"recommended,omitempty"`
}

func LoadEmbeddedModalities() (ModalityManifest, error) {
	modalityOnce.Do(func() {
		data, err := bootagent.EmbeddedModalityLock()
		if err != nil {
			modalityErr = oneerrors.New(oneerrors.InvalidRequest, "Cannot load embedded modality lock manifest", oneerrors.WithCause(err))
			return
		}
		modalityManifest, modalityErr = ParseModalities(data)
	})
	if modalityErr != nil {
		return ModalityManifest{}, modalityErr
	}
	return cloneModalityManifest(modalityManifest), nil
}

func ParseModalities(data []byte) (ModalityManifest, error) {
	var manifest ModalityManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ModalityManifest{}, invalidModalities(fmt.Sprintf("Cannot load modality lock manifest: %v", err))
	}
	if manifest.SchemaVersion != ModalitySchemaVersion {
		return ModalityManifest{}, invalidModalities("Unsupported modality lock manifest schema")
	}
	if len(manifest.IDRules) == 0 {
		return ModalityManifest{}, invalidModalities("Modality lock manifest has no ID rules")
	}
	for index, rule := range manifest.IDRules {
		if _, err := regexp.Compile(rule.Pattern); err != nil || strings.TrimSpace(rule.Pattern) == "" {
			return ModalityManifest{}, invalidModalities(fmt.Sprintf("Modality ID rule %d has an invalid pattern", index))
		}
		if rule.Exclude != "" {
			if _, err := regexp.Compile(rule.Exclude); err != nil {
				return ModalityManifest{}, invalidModalities(fmt.Sprintf("Modality ID rule %d has an invalid exclude pattern", index))
			}
		}
		if len(rule.Capabilities) == 0 {
			return ModalityManifest{}, invalidModalities(fmt.Sprintf("Modality ID rule %d names no capability", index))
		}
	}
	for providerID, entry := range manifest.Providers {
		if _, ok := ProviderByID(providerID); !ok {
			return ModalityManifest{}, invalidModalities(fmt.Sprintf("Modality provider %q is not a built-in Provider", providerID))
		}
		if entry.NativeBaseURL != "" && (!httpsURL(entry.NativeBaseURL) || strings.HasSuffix(entry.NativeBaseURL, "/")) {
			return ModalityManifest{}, invalidModalities(fmt.Sprintf("Modality provider %q has an invalid native_base_url", providerID))
		}
		seen := map[string]bool{}
		for _, model := range entry.Generation {
			if strings.TrimSpace(model.ID) == "" || len(model.Capabilities) == 0 || strings.TrimSpace(model.Adapter) == "" {
				return ModalityManifest{}, invalidModalities(fmt.Sprintf("Modality provider %q has an incomplete generation model", providerID))
			}
			if seen[model.ID] {
				return ModalityManifest{}, invalidModalities(fmt.Sprintf("Modality provider %q lists %q twice", providerID, model.ID))
			}
			seen[model.ID] = true
		}
	}
	return cloneModalityManifest(manifest), nil
}

func invalidModalities(message string) error {
	return oneerrors.New(oneerrors.InvalidRequest, message)
}

func cloneModalityManifest(source ModalityManifest) ModalityManifest {
	result := ModalityManifest{SchemaVersion: source.SchemaVersion}
	result.IDRules = make([]ModalityIDRule, len(source.IDRules))
	for index, rule := range source.IDRules {
		rule.Capabilities = append([]string(nil), rule.Capabilities...)
		result.IDRules[index] = rule
	}
	result.Providers = make(map[string]ModalityProvider, len(source.Providers))
	for id, entry := range source.Providers {
		models := make([]ModalityGenerationModel, len(entry.Generation))
		for index, model := range entry.Generation {
			model.Capabilities = append([]string(nil), model.Capabilities...)
			model.Options = append([]string(nil), model.Options...)
			models[index] = model
		}
		result.Providers[id] = ModalityProvider{NativeBaseURL: entry.NativeBaseURL, Generation: models, Unsupported: append([]string(nil), entry.Unsupported...)}
	}
	return result
}
