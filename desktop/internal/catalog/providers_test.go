package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicProvidersCarriesNoInternalField(t *testing.T) {
	// The defect this projection exists for: serialising the provider struct
	// wholesale put the fallback probe model into the status payload the moment
	// it was added. Asserting on the serialised form is what catches a field
	// added later without a matching decision to expose it.
	encoded, err := json.Marshal(PublicProviders())
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	for _, forbidden := range []string{"fallback", "deepseek", "FallbackProbeModel"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("the public payload leaks %q: %s", forbidden, encoded)
		}
	}
}

func TestPublicProvidersKeepsTheFieldsTheFrontendReads(t *testing.T) {
	public := PublicProviders()
	for id, meta := range Providers {
		entry, present := public[id]
		if !present {
			t.Fatalf("%s missing from the public projection", id)
		}
		if entry.Name != meta.Name || entry.Home != meta.Home || entry.BaseURL != meta.BaseURL {
			t.Errorf("%s: projection dropped a field: %+v", id, entry)
		}
		if entry.AnthropicBaseURL != meta.AnthropicBaseURL {
			t.Errorf("%s: anthropic base = %q, want %q", id, entry.AnthropicBaseURL, meta.AnthropicBaseURL)
		}
	}
}

func TestEveryProviderDeclaresBothProtocolRoutes(t *testing.T) {
	// An Anthropic-speaking Agent pointed at the OpenAI route reports success
	// and then cannot authenticate, so a missing route is not a cosmetic gap.
	for id, meta := range Providers {
		if meta.BaseURL == "" || meta.AnthropicBaseURL == "" {
			t.Errorf("%s: base=%q anthropic=%q, both are required", id, meta.BaseURL, meta.AnthropicBaseURL)
		}
		if meta.FallbackProbeModel == "" {
			t.Errorf("%s: no fallback probe model", id)
		}
	}
}

func TestFallbackProbeModelIsProviderSpecific(t *testing.T) {
	// PPIO publishes deepseek-v3 and Novita publishes deepseek_v3. One shared
	// constant would send a foreign ID to one of them, and the probe gates the
	// whole wizard.
	ppio, novita := FallbackProbeModel("ppio"), FallbackProbeModel("novita")
	if ppio == novita {
		t.Errorf("both providers fall back to %q; the IDs differ upstream", ppio)
	}
	for id, meta := range Providers {
		if got := FallbackProbeModel(id); got != meta.FallbackProbeModel {
			t.Errorf("%s: got %q, want %q", id, got, meta.FallbackProbeModel)
		}
	}
}

func TestAnUnknownProviderGetsTheDocumentedGuess(t *testing.T) {
	// A custom endpoint has no catalog entry, so the widely published ID is the
	// best available guess. Being wrong costs one round trip; refusing to probe
	// would block the wizard.
	for _, id := range []string{"custom", "", "not-a-provider"} {
		if got := FallbackProbeModel(id); got != DefaultFallbackProbeModel {
			t.Errorf("FallbackProbeModel(%q) = %q, want %q", id, got, DefaultFallbackProbeModel)
		}
	}
}

func TestTheOfficialRegistryIsFirstAndTheOrderIsFixed(t *testing.T) {
	// Deriving the order from a map would let the default move between builds,
	// and the default registry is the one a user gets without choosing.
	listed := PublicMirrors()
	if len(listed) < 2 {
		t.Fatalf("expected at least the official source and one mirror, got %d", len(listed))
	}
	if listed[0].ID != "official" {
		t.Errorf("first mirror is %q, want the official source", listed[0].ID)
	}
}

func TestEveryMirrorNamesItsUpstreamOverHTTPS(t *testing.T) {
	// The upstream is what makes a mirror auditable: it is how the integrity
	// value in the manifest is known to describe the same package.
	for _, mirror := range PublicMirrors() {
		if mirror.Upstream != OfficialNpmRegistry {
			t.Errorf("%s: upstream = %q, want the official registry", mirror.ID, mirror.Upstream)
		}
		if !strings.HasPrefix(mirror.Registry, "https://") {
			t.Errorf("%s: registry %q is not HTTPS", mirror.ID, mirror.Registry)
		}
		if mirror.Note == "" {
			t.Errorf("%s: no note, so the UI cannot explain what this source is", mirror.ID)
		}
	}
}

func TestNoMirrorPointsAtStorageOneAgentOperates(t *testing.T) {
	// Redistributing a proprietary Agent needs a licence that pointing at a
	// public read-only mirror does not. This is a product-boundary constraint,
	// not a preference.
	for _, mirror := range PublicMirrors() {
		for _, forbidden := range []string{"oneagent", "maimory", "ppio.com", "novita.ai"} {
			if strings.Contains(strings.ToLower(mirror.Registry), forbidden) {
				t.Errorf("%s points at %q, which OneAgent must not redistribute from", mirror.ID, mirror.Registry)
			}
		}
	}
}

func TestAnUnknownMirrorIdReportsAbsence(t *testing.T) {
	if _, present := MirrorByID("not-a-mirror"); present {
		t.Error("an unknown mirror id must report present=false")
	}
	if mirror, present := MirrorByID("npmmirror"); !present || mirror.Registry == "" {
		t.Error("npmmirror should resolve")
	}
}
