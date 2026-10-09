package modality

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// Capability is one capability found on one model.
type Capability struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Adapter string `json:"adapter"`
	// Supported is false only for a probe that the Provider refused. It is kept
	// in the result so the UI can say the check failed, rather than silently
	// dropping a model the user just checked.
	Supported bool `json:"supported"`
	// Installable says the evidence is strong enough to write a Skill for it.
	// An ID rule alone never is, and neither is a declaration for an adapter
	// whose wire format across gateways is unverified.
	Installable bool `json:"installable"`
	// Probe is the live check available for this capability; see ProbeKind.
	Probe string `json:"probe"`
}

// Model is one model with every capability found for it, in Capabilities order.
type Model struct {
	ID           string       `json:"id"`
	Capabilities []Capability `json:"capabilities"`
}

// Rules is the loaded catalog, compiled and checked against the vocabulary.
type Rules struct {
	rules     []compiledRule
	providers map[string]catalog.ModalityProvider
}

type compiledRule struct {
	pattern      *regexp.Regexp
	exclude      *regexp.Regexp
	capabilities []string
}

// Load reads the embedded modalities.lock.json.
func Load() (Rules, error) {
	manifest, err := catalog.LoadEmbeddedModalities()
	if err != nil {
		return Rules{}, err
	}
	return NewRules(manifest)
}

// NewRules compiles a parsed catalog. It rejects a capability or adapter
// outside the vocabulary, so a typo in the lock file fails tests instead of
// silently matching nothing.
func NewRules(manifest catalog.ModalityManifest) (Rules, error) {
	result := Rules{providers: manifest.Providers}
	for index, rule := range manifest.IDRules {
		for _, capability := range rule.Capabilities {
			if !slices.Contains(Capabilities, capability) {
				return Rules{}, invalid(fmt.Sprintf("Modality ID rule %d names unknown capability %q", index, capability))
			}
			if _, ok := chatAdapter[capability]; !ok {
				return Rules{}, invalid(fmt.Sprintf("Modality ID rule %d claims %q, which no chat model route serves", index, capability))
			}
		}
		pattern, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return Rules{}, invalid(fmt.Sprintf("Modality ID rule %d has an invalid pattern", index))
		}
		compiled := compiledRule{pattern: pattern, capabilities: rule.Capabilities}
		if rule.Exclude != "" {
			if compiled.exclude, err = regexp.Compile(rule.Exclude); err != nil {
				return Rules{}, invalid(fmt.Sprintf("Modality ID rule %d has an invalid exclude pattern", index))
			}
		}
		result.rules = append(result.rules, compiled)
	}
	for providerID, entry := range manifest.Providers {
		for _, model := range entry.Generation {
			if !slices.Contains(Adapters, model.Adapter) {
				return Rules{}, invalid(fmt.Sprintf("Modality provider %q model %q names unknown adapter %q", providerID, model.ID, model.Adapter))
			}
			for _, capability := range model.Capabilities {
				if !slices.Contains(Capabilities, capability) {
					return Rules{}, invalid(fmt.Sprintf("Modality provider %q model %q names unknown capability %q", providerID, model.ID, capability))
				}
			}
		}
	}
	return result, nil
}

// NativeBaseURL is the host root for a Provider's native routes, empty when
// the catalog names none.
func (r Rules) NativeBaseURL(providerID string) string {
	return r.providers[providerID].NativeBaseURL
}

// RuleKey is the form ID rules match against: lower-cased, with any vendor/
// prefix removed. Gateway suffixes are left alone because rules are anchored
// at the start.
func RuleKey(modelID string) string {
	key := strings.ToLower(strings.TrimSpace(modelID))
	if index := strings.LastIndex(key, "/"); index >= 0 {
		key = key[index+1:]
	}
	return key
}

// MatchIDRules returns the capabilities the ID rules claim for a model, in
// Capabilities order.
func (r Rules) MatchIDRules(modelID string) []string {
	key := RuleKey(modelID)
	claimed := map[string]bool{}
	for _, rule := range r.rules {
		if rule.pattern.MatchString(key) && (rule.exclude == nil || !rule.exclude.MatchString(key)) {
			for _, capability := range rule.capabilities {
				claimed[capability] = true
			}
		}
	}
	return ordered(claimed)
}

// DeclaredCapabilities maps what a /models entry declares onto capabilities.
// Audio and video output on a chat model map to nothing: there is no chat
// route for either, and how a gateway would return them is unknown.
func DeclaredCapabilities(entry provider.ModelEntry) []string {
	claimed := map[string]bool{}
	for _, value := range entry.InputModalities {
		switch value {
		case "image":
			claimed[Vision] = true
		case "video":
			claimed[VideoUnderstanding] = true
		case "audio":
			claimed[SpeechToText] = true
		}
	}
	if slices.Contains(entry.OutputModalities, "image") {
		claimed[ImageGeneration] = true
	}
	return ordered(claimed)
}

// Resolve decides each model's capabilities for one Provider. For every model
// and capability the first source that answers wins:
//
//  1. a probe verdict in the capability cache (verdicts, keyed by model ID);
//  2. the model's declared modalities, when the Provider sent any;
//  3. an ID rule;
//  4. otherwise the capability is not offered.
//
// The declared metadata answers for the whole model: a model that declares
// modalities is not given more by an ID rule, because the Provider's own word
// about a model it serves outranks a guess from its name. Generation models
// the catalog lists for the Provider are appended with source catalog.
func (r Rules) Resolve(providerID string, entries []provider.ModelEntry, verdicts map[string]map[string]provider.ModalityVerdict) []Model {
	models := make([]Model, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		source, claimed := SourceIDRule, r.MatchIDRules(id)
		if entry.Declared() {
			source, claimed = SourceDeclared, DeclaredCapabilities(entry)
		}
		probed := verdicts[id]
		var capabilities []Capability
		for _, capabilityID := range Capabilities {
			if verdict, ok := probed[capabilityID]; ok {
				capabilities = append(capabilities, probedCapability(capabilityID, verdict))
				continue
			}
			if !slices.Contains(claimed, capabilityID) {
				continue
			}
			adapter := chatAdapter[capabilityID]
			capabilities = append(capabilities, Capability{
				ID:          capabilityID,
				Source:      source,
				Adapter:     adapter,
				Supported:   true,
				Installable: source == SourceDeclared && !probeOnly[adapter],
				Probe:       ProbeKind(adapter),
			})
		}
		if len(capabilities) > 0 {
			models = append(models, Model{ID: id, Capabilities: capabilities})
		}
	}
	for _, generation := range r.providers[providerID].Generation {
		if seen[generation.ID] {
			continue
		}
		seen[generation.ID] = true
		probed := verdicts[generation.ID]
		var capabilities []Capability
		for _, capabilityID := range Capabilities {
			if !slices.Contains(generation.Capabilities, capabilityID) {
				continue
			}
			if verdict, ok := probed[capabilityID]; ok {
				capabilities = append(capabilities, probedCapability(capabilityID, verdict))
				continue
			}
			capabilities = append(capabilities, Capability{
				ID:          capabilityID,
				Source:      SourceCatalog,
				Adapter:     generation.Adapter,
				Supported:   true,
				Installable: true,
				Probe:       ProbeKind(generation.Adapter),
			})
		}
		models = append(models, Model{ID: generation.ID, Capabilities: capabilities})
	}
	sort.SliceStable(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func probedCapability(capabilityID string, verdict provider.ModalityVerdict) Capability {
	adapter := verdict.Adapter
	if adapter == "" {
		adapter = chatAdapter[capabilityID]
	}
	return Capability{
		ID:          capabilityID,
		Source:      SourceProbe,
		Adapter:     adapter,
		Supported:   verdict.Supported,
		Installable: verdict.Supported,
		Probe:       ProbeKind(adapter),
	}
}

// ChatAdapter is the adapter a capability on a chat model uses, empty when no
// chat route serves it.
func ChatAdapter(capability string) string {
	return chatAdapter[capability]
}

func ordered(claimed map[string]bool) []string {
	result := make([]string, 0, len(claimed))
	for _, capability := range Capabilities {
		if claimed[capability] {
			result = append(result, capability)
		}
	}
	return result
}

func invalid(message string) error {
	return oneerrors.New(oneerrors.InvalidRequest, message)
}
