package provider

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

func capabilityStore(t *testing.T, home string) CapabilityStore {
	t.Helper()
	return NewCapabilityStore(home, securefs.New(securefs.Options{OS: "linux"}))
}

func probedEntry() Entry {
	return Entry{ID: "relay", Name: "Relay", BaseURL: "https://relay.test/v1", APIKey: "sk-secret-value"}
}

func TestCapabilitiesRoundTrip(t *testing.T) {
	store := capabilityStore(t, t.TempDir())
	entry := probedEntry()
	verdicts := map[string]ProtocolCapability{
		ProtocolOpenAI:    {Supported: true, Reachable: true, Status: 200},
		ProtocolResponses: {Reachable: true, Status: 404, ErrorCode: "PROTOCOL_UNSUPPORTED"},
	}
	if err := store.Save(context.Background(), entry, "gpt-test", verdicts); err != nil {
		t.Fatal(err)
	}
	saved, ok := store.Get(entry, "gpt-test")
	if !ok {
		t.Fatal("saved capabilities were not readable")
	}
	if !saved.Protocols[ProtocolOpenAI].Supported {
		t.Error("chat completions verdict lost")
	}
	responses := saved.Protocols[ProtocolResponses]
	if responses.Supported || responses.ErrorCode != "PROTOCOL_UNSUPPORTED" {
		t.Errorf("responses verdict = %#v", responses)
	}
	if saved.ProbedAt.IsZero() {
		t.Errorf("probe time lost: %#v", saved)
	}
}

// Protocol support varies by model on the same endpoint, so a verdict measured with
// one model must never answer for another. Reusing it would let BootAgent write a
// config for a model it never verified against that protocol.
func TestCapabilitiesAreNotSharedBetweenModels(t *testing.T) {
	store := capabilityStore(t, t.TempDir())
	entry := probedEntry()
	if err := store.Save(context.Background(), entry, "supports-responses", map[string]ProtocolCapability{
		ProtocolResponses: {Supported: true, Reachable: true, Status: 200},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(entry, "never-probed"); ok {
		t.Fatal("an unprobed model inherited another model's verdict")
	}
	if err := store.Save(context.Background(), entry, "chat-only", map[string]ProtocolCapability{
		ProtocolResponses: {Reachable: true, Status: 404, ErrorCode: "PROTOCOL_UNSUPPORTED"},
	}); err != nil {
		t.Fatal(err)
	}
	first, ok := store.Get(entry, "supports-responses")
	if !ok || !first.Protocols[ProtocolResponses].Supported {
		t.Errorf("the first model's verdict was overwritten: %#v", first)
	}
	second, ok := store.Get(entry, "chat-only")
	if !ok || second.Protocols[ProtocolResponses].Supported {
		t.Errorf("the second model's verdict is wrong: %#v", second)
	}
}

// A verdict with no model attached could not be looked up again and would silently
// widen to every model on the Provider.
func TestCapabilitiesRejectAVerdictWithNoModel(t *testing.T) {
	store := capabilityStore(t, t.TempDir())
	if err := store.Save(context.Background(), probedEntry(), "  ", map[string]ProtocolCapability{
		ProtocolOpenAI: {Supported: true},
	}); err == nil {
		t.Fatal("a verdict with no model was accepted")
	}
}

// The record has to stop applying when the endpoint or the credential changes,
// without any caller remembering to clear it.
func TestCapabilitiesMissAfterTheProviderChanges(t *testing.T) {
	entry := probedEntry()
	for name, changed := range map[string]Entry{
		"base URL":       {ID: entry.ID, Name: entry.Name, BaseURL: "https://other.test/v1", APIKey: entry.APIKey},
		"anthropic base": {ID: entry.ID, Name: entry.Name, BaseURL: entry.BaseURL, AnthropicBaseURL: "https://other.test", APIKey: entry.APIKey},
		"api key":        {ID: entry.ID, Name: entry.Name, BaseURL: entry.BaseURL, APIKey: "sk-rotated"},
	} {
		t.Run(name, func(t *testing.T) {
			store := capabilityStore(t, t.TempDir())
			if err := store.Save(context.Background(), entry, "gpt-test", map[string]ProtocolCapability{
				ProtocolOpenAI: {Supported: true, Reachable: true, Status: 200},
			}); err != nil {
				t.Fatal(err)
			}
			if _, ok := store.Get(changed, "gpt-test"); ok {
				t.Fatalf("a changed %s still matched the cached record", name)
			}
			// The unchanged Provider must still hit, or every activation re-probes.
			if _, ok := store.Get(entry, "gpt-test"); !ok {
				t.Fatal("the unchanged Provider stopped matching its own record")
			}
		})
	}
}

// Probing one protocol must not discard what is already known about another, or a
// two-protocol decision would cost two probes every time.
func TestCapabilitiesSaveMergesProtocols(t *testing.T) {
	store := capabilityStore(t, t.TempDir())
	entry := probedEntry()
	if err := store.Save(context.Background(), entry, "gpt-test", map[string]ProtocolCapability{
		ProtocolOpenAI: {Supported: true, Reachable: true, Status: 200},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), entry, "gpt-test", map[string]ProtocolCapability{
		ProtocolAnthropic: {Reachable: true, Status: 404, ErrorCode: "PROTOCOL_UNSUPPORTED"},
	}); err != nil {
		t.Fatal(err)
	}
	saved, ok := store.Get(entry, "gpt-test")
	if !ok {
		t.Fatal("capabilities missing after the second save")
	}
	if len(saved.Protocols) != 2 || !saved.Protocols[ProtocolOpenAI].Supported {
		t.Errorf("second save overwrote the first verdict: %#v", saved.Protocols)
	}
}

// The cache is keyed by a hash of the credential, so the credential itself must not
// be recoverable from the file.
func TestCapabilityFileNeverStoresTheAPIKey(t *testing.T) {
	home := t.TempDir()
	store := capabilityStore(t, home)
	entry := probedEntry()
	if err := store.Save(context.Background(), entry, "gpt-test", map[string]ProtocolCapability{
		ProtocolOpenAI: {Supported: true, Reachable: true, Status: 200},
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), entry.APIKey) {
		t.Fatalf("the API key reached %s", store.Path())
	}
	if !strings.Contains(string(raw), Fingerprint(entry)) {
		t.Error("the fingerprint that binds the record to its Provider is missing")
	}
}

func TestCapabilitiesInvalidateDropsTheRecord(t *testing.T) {
	store := capabilityStore(t, t.TempDir())
	entry := probedEntry()
	if err := store.Save(context.Background(), entry, "gpt-test", map[string]ProtocolCapability{
		ProtocolOpenAI: {Supported: true, Reachable: true, Status: 200},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Invalidate(context.Background(), entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(entry, "gpt-test"); ok {
		t.Fatal("the record survived invalidation")
	}
	// Invalidating something that was never cached is a normal no-op.
	if err := store.Invalidate(context.Background(), "never-probed"); err != nil {
		t.Fatal(err)
	}
}

// A corrupt cache must read as a miss rather than an error: it is derived data, and
// failing here would block activation over a file nobody needs to keep.
func TestCapabilitiesTreatCorruptFileAsAMiss(t *testing.T) {
	home := t.TempDir()
	store := capabilityStore(t, home)
	if err := os.MkdirAll(strings.TrimSuffix(store.Path(), "/capabilities.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(probedEntry(), "gpt-test"); ok {
		t.Fatal("a corrupt cache reported a hit")
	}
	// Saving over the corrupt file has to succeed, or the state is unrecoverable.
	if err := store.Save(context.Background(), probedEntry(), "gpt-test", map[string]ProtocolCapability{
		ProtocolOpenAI: {Supported: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(probedEntry(), "gpt-test"); !ok {
		t.Fatal("the cache did not recover after a save")
	}
}
