package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

// capabilitySchemaVersion 2 added per-model modality verdicts. Version 1 files
// are read as version 2 with no modalities: discarding them would throw away
// protocol verdicts and make the next activation spend a billed probe.
const capabilitySchemaVersion = 2

const legacyCapabilitySchemaVersion = 1

// ProtocolCapability is one probe verdict. ErrorCode is kept alongside Supported
// because routing has to tell "this endpoint refuses this protocol" apart from "the
// key was rejected": only the first is a property of the endpoint, and only it
// justifies converting instead of failing.
type ProtocolCapability struct {
	Supported bool   `json:"supported"`
	Reachable bool   `json:"reachable"`
	Status    int    `json:"status,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// ModelCapabilities holds the verdicts for one model. Protocol support is a property
// of the model as much as the endpoint -- the measurements behind ADR-004 found one
// relay serving Chat Completions for 31 of 36 models but Responses for only 10 -- so
// a verdict may never be reused across models.
type ModelCapabilities struct {
	ProbedAt  time.Time                     `json:"probed_at"`
	Protocols map[string]ProtocolCapability `json:"protocols"`
	// Modalities holds probe verdicts for multimodal capabilities, keyed by
	// capability ID. Only probe verdicts are cached: catalog matches and
	// declared metadata are recomputed on every read.
	Modalities map[string]ModalityVerdict `json:"modalities,omitempty"`
}

// ModalityVerdict is the outcome of one capability probe the user asked for.
// Only conclusive answers are stored -- the Provider accepted the request, or
// refused this model -- because a busy endpoint or a rejected key says nothing
// about what the model can do.
type ModalityVerdict struct {
	Supported bool      `json:"supported"`
	Source    string    `json:"source"`
	Adapter   string    `json:"adapter"`
	ProbedAt  time.Time `json:"probed_at"`
	Status    int       `json:"status,omitempty"`
}

// ProviderCapabilities groups every probed model for one Provider. Fingerprint binds
// them to the endpoints and key that produced them, so editing either invalidates
// the lot without any caller having to remember to clear the cache.
type ProviderCapabilities struct {
	Fingerprint string                       `json:"fingerprint"`
	Models      map[string]ModelCapabilities `json:"models"`
}

type capabilityFile struct {
	SchemaVersion int                             `json:"schema_version"`
	Providers     map[string]ProviderCapabilities `json:"providers"`
}

// CapabilityStore caches probe verdicts so deciding whether an Agent needs
// conversion does not spend a billed completion request on every activation. It is
// deliberately a separate file from providers.json: this is derived data that may be
// deleted at any moment, while providers.json is user truth returned through the
// binding.
type CapabilityStore struct {
	home       string
	filesystem securefs.Store
}

func NewCapabilityStore(home string, filesystem securefs.Store) CapabilityStore {
	return CapabilityStore{home: home, filesystem: filesystem}
}

func (s CapabilityStore) Path() string {
	return filepath.Join(s.home, ".bootagent", "capabilities.json")
}

// Fingerprint covers everything about a Provider that a verdict depends on. The key
// is hashed rather than stored: a verdict must change when the key does, but this
// file must never carry a credential.
func Fingerprint(entry Entry) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{entry.BaseURL, entry.AnthropicBaseURL, entry.APIKey}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// Get reports the cached verdicts for one Provider and model. A stale fingerprint, an
// unknown model, or an unreadable file is a miss rather than an error: the cache is
// derived, and the caller's answer to a miss is simply to probe again.
func (s CapabilityStore) Get(entry Entry, model string) (ModelCapabilities, bool) {
	file, err := s.load()
	if err != nil {
		return ModelCapabilities{}, false
	}
	saved, ok := file.Providers[entry.ID]
	if !ok || saved.Fingerprint != Fingerprint(entry) {
		return ModelCapabilities{}, false
	}
	verdicts, ok := saved.Models[strings.TrimSpace(model)]
	if !ok || len(verdicts.Protocols) == 0 {
		return ModelCapabilities{}, false
	}
	return verdicts, true
}

// Save merges verdicts for one Provider and model. Protocols absent from the update
// are kept, so probing one protocol never discards what is already known about
// another, and other models keep their own records.
func (s CapabilityStore) Save(ctx context.Context, entry Entry, model string, verdicts map[string]ProtocolCapability) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "A capability verdict must name the model it was measured with")
	}
	file, err := s.load()
	if err != nil {
		file = newCapabilityFile()
	}
	fingerprint := Fingerprint(entry)
	current, ok := file.Providers[entry.ID]
	if !ok || current.Fingerprint != fingerprint || current.Models == nil {
		current = ProviderCapabilities{Fingerprint: fingerprint, Models: map[string]ModelCapabilities{}}
	}
	existing, ok := current.Models[model]
	if !ok || existing.Protocols == nil {
		existing.Protocols = map[string]ProtocolCapability{}
	}
	for protocolID, verdict := range verdicts {
		existing.Protocols[protocolID] = verdict
	}
	existing.ProbedAt = time.Now().UTC()
	current.Models[model] = existing
	file.Providers[entry.ID] = current
	return s.write(ctx, file)
}

// Modalities returns every cached modality verdict for one Provider, keyed by
// model and then capability. A stale fingerprint is a miss, like Get.
func (s CapabilityStore) Modalities(entry Entry) map[string]map[string]ModalityVerdict {
	result := map[string]map[string]ModalityVerdict{}
	file, err := s.load()
	if err != nil {
		return result
	}
	saved, ok := file.Providers[entry.ID]
	if !ok || saved.Fingerprint != Fingerprint(entry) {
		return result
	}
	for model, record := range saved.Models {
		if len(record.Modalities) == 0 {
			continue
		}
		verdicts := make(map[string]ModalityVerdict, len(record.Modalities))
		for capability, verdict := range record.Modalities {
			verdicts[capability] = verdict
		}
		result[model] = verdicts
	}
	return result
}

// SaveModality records one capability probe verdict for a model. Protocol
// verdicts and other capabilities of the same model are kept.
func (s CapabilityStore) SaveModality(ctx context.Context, entry Entry, model, capability string, verdict ModalityVerdict) error {
	model = strings.TrimSpace(model)
	capability = strings.TrimSpace(capability)
	if model == "" || capability == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "A capability verdict must name the model and capability it was measured with")
	}
	file, err := s.load()
	if err != nil {
		file = newCapabilityFile()
	}
	fingerprint := Fingerprint(entry)
	current, ok := file.Providers[entry.ID]
	if !ok || current.Fingerprint != fingerprint || current.Models == nil {
		current = ProviderCapabilities{Fingerprint: fingerprint, Models: map[string]ModelCapabilities{}}
	}
	existing := current.Models[model]
	if existing.Protocols == nil {
		existing.Protocols = map[string]ProtocolCapability{}
	}
	if existing.Modalities == nil {
		existing.Modalities = map[string]ModalityVerdict{}
	}
	if verdict.ProbedAt.IsZero() {
		verdict.ProbedAt = time.Now().UTC()
	}
	existing.Modalities[capability] = verdict
	current.Models[model] = existing
	file.Providers[entry.ID] = current
	return s.write(ctx, file)
}

// Invalidate drops a Provider's records after an edit that changes what a probe would
// answer. A stale fingerprint is already caught on read, so this reclaims the entry
// rather than upholding correctness on its own.
func (s CapabilityStore) Invalidate(ctx context.Context, providerID string) error {
	file, err := s.load()
	if err != nil {
		return nil
	}
	providerID = strings.TrimSpace(providerID)
	if _, ok := file.Providers[providerID]; !ok {
		return nil
	}
	delete(file.Providers, providerID)
	return s.write(ctx, file)
}

// CapabilityFromProbe reduces a probe result to what routing needs.
func CapabilityFromProbe(result ProbeResult) ProtocolCapability {
	capability := ProtocolCapability{Supported: result.OK, Reachable: result.Reachable, Status: result.Status}
	if result.ErrorCode != nil {
		capability.ErrorCode = *result.ErrorCode
	}
	return capability
}

func newCapabilityFile() capabilityFile {
	return capabilityFile{SchemaVersion: capabilitySchemaVersion, Providers: map[string]ProviderCapabilities{}}
}

func (s CapabilityStore) load() (capabilityFile, error) {
	data, err := os.ReadFile(s.Path())
	if os.IsNotExist(err) {
		return newCapabilityFile(), nil
	}
	if err != nil {
		return newCapabilityFile(), err
	}
	file := newCapabilityFile()
	if err := json.Unmarshal(data, &file); err != nil || file.Providers == nil {
		return newCapabilityFile(), errors.New("cached Provider capabilities are unusable")
	}
	switch file.SchemaVersion {
	case capabilitySchemaVersion:
	case legacyCapabilitySchemaVersion:
		// Same shape minus modalities, which unmarshal as nil. The next write
		// stamps the current version.
		file.SchemaVersion = capabilitySchemaVersion
	default:
		return newCapabilityFile(), errors.New("cached Provider capabilities are unusable")
	}
	return file, nil
}

func (s CapabilityStore) write(ctx context.Context, file capabilityFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot encode cached Provider capabilities")
	}
	// Not written as a secret: the record holds verdicts and a hash, never a key.
	_, err = s.filesystem.AtomicWrite(ctx, s.Path(), append(data, '\n'), false)
	return err
}
