package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEmbeddedManifestMatchesCurrentCatalogContract(t *testing.T) {
	manifest, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != SchemaVersion {
		t.Fatalf("schema = %d", manifest.SchemaVersion)
	}
	automatic := 0
	for id, agent := range manifest.Agents {
		if agent.ConfigMode == "auto" {
			automatic++
			if agent.Package == nil || agent.ConfigAdapter == "" {
				t.Errorf("auto Agent %s has incomplete contract", id)
			}
		}
	}
	// A deliberate tripwire, not a fact about the world: 363550c removed three
	// entries as a side effect of an unrelated commit and nothing failed, while
	// the README went on describing them. Changing this number is fine; changing
	// it without noticing is what this prevents.
	if automatic != 7 {
		t.Fatalf("automatic Agent count = %d, want 7", automatic)
	}
	items := PublicCatalog(manifest, "windows")
	if len(items) != len(manifest.Agents) {
		t.Fatalf("catalog length = %d", len(items))
	}
	for index := 1; index < len(items); index++ {
		if items[index-1].Rank > items[index].Rank || (items[index-1].Rank == items[index].Rank && items[index-1].ID > items[index].ID) {
			t.Fatalf("catalog is not deterministic: %#v", items)
		}
	}
}

func TestEmbeddedProvidersMatchCurrentCatalogContract(t *testing.T) {
	manifest, err := LoadEmbeddedProviders()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != ProviderSchemaVersion || len(manifest.Providers) == 0 {
		t.Fatalf("provider manifest = %#v", manifest)
	}
	for id, provider := range manifest.Providers {
		if provider.Name == "" || provider.BaseURL == "" || provider.fallbackModel == "" {
			t.Fatalf("provider %q is incomplete: %#v", id, provider)
		}
		if provider.DefaultModel == "" {
			t.Fatalf("provider %q has no default model to pre-fill: %#v", id, provider)
		}
	}
	if got := FallbackProbeModel("unknown"); got != manifest.DefaultFallbackProbeModel {
		t.Fatalf("unknown fallback = %q, want %q", got, manifest.DefaultFallbackProbeModel)
	}
}

// Reads both values from the manifest rather than comparing against literals.
// PPIO and Novita spelled the same DeepSeek release differently at v3 (hyphen
// versus underscore) and identically at v4, so a literal would encode whichever
// happened to be true when it was written. What must hold is that each Provider
// names its own model and that nothing invents one for an endpoint we have never
// seen.
func TestDefaultModelIsPerProviderAndAbsentForUnknownProviders(t *testing.T) {
	manifest, err := LoadEmbeddedProviders()
	if err != nil {
		t.Fatal(err)
	}
	for id, provider := range manifest.Providers {
		if got := DefaultModel(id); got != provider.DefaultModel {
			t.Fatalf("DefaultModel(%q) = %q, want the manifest value %q", id, got, provider.DefaultModel)
		}
	}
	// Unlike FallbackProbeModel, which falls back to a manifest-wide default,
	// this must stay empty: a custom endpoint gets no guessed model, because a
	// guess would produce a config that fails on the user's first request.
	for _, id := range []string{"custom", "unknown"} {
		if got := DefaultModel(id); got != "" {
			t.Fatalf("DefaultModel(%q) = %q, want empty", id, got)
		}
	}
}

// The key page is what the UI opens, so an unreachable or downgraded URL is a
// user-visible failure. Scheme is enforced at parse time; this pins the
// contract that absence is allowed and falls back to Home.
func TestKeyManagementURLIsHTTPSOrAbsent(t *testing.T) {
	manifest, err := LoadEmbeddedProviders()
	if err != nil {
		t.Fatal(err)
	}
	for id, provider := range manifest.Providers {
		if got := KeyManagementURL(id); got != provider.KeyManagementURL {
			t.Fatalf("KeyManagementURL(%q) = %q, want %q", id, got, provider.KeyManagementURL)
		}
		if provider.KeyManagementURL != "" && !httpsURL(provider.KeyManagementURL) {
			t.Fatalf("provider %q key page is not HTTPS: %q", id, provider.KeyManagementURL)
		}
		if provider.KeyManagementURL == "" && provider.Home == "" {
			t.Fatalf("provider %q has neither a key page nor a home URL", id)
		}
	}
	if got := KeyManagementURL("unknown"); got != "" {
		t.Fatalf("KeyManagementURL(unknown) = %q, want empty", got)
	}
}

// The assertion is on the probe model's own value, not on the substring
// "deepseek": default_model is public and legitimately names a DeepSeek model,
// so a vendor-name check would fail on correct output while still passing if
// the probe model were renamed. Comparing against what the manifest actually
// holds is what makes this test mean "the internal field did not leak".
func TestPublicProjectionDoesNotExposeFallbackModel(t *testing.T) {
	providers := PublicProviders()
	data, err := json.Marshal(providers)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || contains(string(data), "fallback") {
		t.Fatalf("internal provider fields leaked: %s", data)
	}
	for id, provider := range providers {
		if provider.fallbackModel != "" {
			t.Fatalf("provider %q kept its probe model in the public projection: %q", id, provider.fallbackModel)
		}
		// Guards the case the substring check cannot see: a probe model that
		// happens to equal the default model would leak without appearing to.
		if probe := FallbackProbeModel(id); probe != "" && contains(string(data), probe) {
			t.Fatalf("probe model %q for %q reached the public projection: %s", probe, id, data)
		}
	}
}

func TestParseRejectsInvalidManifest(t *testing.T) {
	for _, data := range []string{
		`{"schema_version":2,"agents":{}}`,
		`{"schema_version":1,"agents":{"bad id":{"name":"Bad","config_mode":"guide","guide":"x","platforms":["linux"],"rank":1}}}`,
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("Parse(%s) unexpectedly succeeded", data)
		}
	}
}

func TestParseProvidersRejectsInvalidEntries(t *testing.T) {
	// Every case carries a complete entry apart from the one defect it is named
	// for. Otherwise adding a required field makes each case fail for the new
	// omission instead of the flaw it was written to catch, and the test keeps
	// passing while testing nothing.
	tests := map[string]string{
		"unsupported schema":  `{"schema_version":2,"providers":{}}`,
		"invalid provider id": `{"schema_version":1,"default_fallback_probe_model":"m","providers":{"bad id":{"name":"Bad","home":"https://example.com","base_url":"https://api.example.com","default_model":"m","fallback_probe_model":"m"}}}`,
		"plaintext home":      `{"schema_version":1,"default_fallback_probe_model":"m","providers":{"ok":{"name":"OK","home":"http://example.com","base_url":"https://api.example.com","default_model":"m","fallback_probe_model":"m"}}}`,
		"empty probe model":   `{"schema_version":1,"default_fallback_probe_model":"m","providers":{"ok":{"name":"OK","home":"https://example.com","base_url":"https://api.example.com","default_model":"m","fallback_probe_model":""}}}`,
		"empty default model": `{"schema_version":1,"default_fallback_probe_model":"m","providers":{"ok":{"name":"OK","home":"https://example.com","base_url":"https://api.example.com","default_model":"","fallback_probe_model":"m"}}}`,
		// The key page is opened in the user's browser, so a downgraded scheme
		// is rejected rather than silently carried through.
		"plaintext key page": `{"schema_version":1,"default_fallback_probe_model":"m","providers":{"ok":{"name":"OK","home":"https://example.com","key_management_url":"http://example.com/keys","base_url":"https://api.example.com","default_model":"m","fallback_probe_model":"m"}}}`,
	}
	for name, data := range tests {
		if _, err := ParseProviders([]byte(data)); err == nil {
			t.Errorf("ParseProviders unexpectedly succeeded for %s: %s", name, data)
		}
	}
}

// docs/public-site-operations.md states that the commercial-disclosure fields
// are published for the site and deliberately not read by the app. That claim is
// currently true only because providerFileEntry omits them, which is invisible
// at the call site: someone adding a field would not know it was load-bearing.
// This pins it, so wiring one up has to be a deliberate edit here too.
func TestSiteOnlyProviderFieldsAreNotParsed(t *testing.T) {
	// Every field carries a value that would be obvious if it leaked.
	data := `{"schema_version":1,"default_fallback_probe_model":"m","providers":{"ok":{
		"name":"OK","home":"https://example.com","base_url":"https://api.example.com",
		"default_model":"m","fallback_probe_model":"m",
		"relationship":"LEAKED","disclosure":"LEAKED","referral_url":"https://leaked.example.com",
		"order":9,"protocols":{"openai":"LEAKED"}}}}`
	manifest, err := ParseProviders([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"LEAKED", "relationship", "disclosure", "referral", "order", "protocols"} {
		if contains(string(encoded), field) {
			t.Fatalf("site-only field %q reached the parsed manifest: %s", field, encoded)
		}
	}
}

// The key page is optional, so this must parse. Without it, making the field
// required by accident would only show up as a released build that refuses to
// start on a Provider that has no key page.
func TestParseProvidersAcceptsAnAbsentKeyManagementURL(t *testing.T) {
	data := `{"schema_version":1,"default_fallback_probe_model":"m","providers":{"ok":{"name":"OK","home":"https://example.com","base_url":"https://api.example.com","default_model":"m","fallback_probe_model":"m"}}}`
	manifest, err := ParseProviders([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Providers["ok"].KeyManagementURL; got != "" {
		t.Fatalf("KeyManagementURL = %q, want empty", got)
	}
}

// A Provider that serves Anthropic Messages on a different path than its
// OpenAI base must declare anthropic_base_url, because BaseFor falls back to
// BaseURL when it is absent. Getting this pair wrong is silent: the Claude Code
// probe would be sent to <base_url>/v1/messages, and the user would read the
// resulting PROTOCOL_UNSUPPORTED as "this Provider cannot serve Claude Code"
// when in fact only the manifest was incomplete.
//
// The check is driven by the manifest itself rather than a hardcoded list, so a
// newly added Provider is covered the moment it lands.
func TestAnthropicBaseURLIsDistinctFromTheOpenAIBase(t *testing.T) {
	manifest, err := LoadEmbeddedProviders()
	if err != nil {
		t.Fatal(err)
	}
	for id, entry := range manifest.Providers {
		if entry.AnthropicBaseURL == "" {
			continue
		}
		if entry.AnthropicBaseURL == entry.BaseURL {
			t.Errorf("provider %q declares anthropic_base_url identical to base_url (%q); drop the field instead", id, entry.BaseURL)
		}
	}
}

func TestParsePreservesDeclaredAgentOrder(t *testing.T) {
	manifest, err := Parse([]byte(`{
		"schema_version": 1,
		"agents": {
			"z-agent": {"name":"Z","config_mode":"guide","guide":"z","platforms":["linux"],"rank":2},
			"a-agent": {"name":"A","config_mode":"guide","guide":"a","platforms":["linux"],"rank":1}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"z-agent", "a-agent"}
	if got := AgentIDs(manifest); !reflect.DeepEqual(got, want) {
		t.Fatalf("AgentIDs() = %v, want declaration order %v", got, want)
	}
}

func contains(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
