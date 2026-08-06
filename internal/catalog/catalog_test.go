package catalog

import (
	"encoding/json"
	"reflect"
	"sort"
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
	}
	ids := ProviderIDs()
	wantIDs := make([]string, 0, len(manifest.Providers))
	for id := range manifest.Providers {
		wantIDs = append(wantIDs, id)
	}
	sort.Strings(wantIDs)
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("ProviderIDs() = %v, want %v", ids, wantIDs)
	}
	if got := FallbackProbeModel("unknown"); got != manifest.DefaultFallbackProbeModel {
		t.Fatalf("unknown fallback = %q, want %q", got, manifest.DefaultFallbackProbeModel)
	}
}

func TestPublicProjectionDoesNotExposeFallbackModel(t *testing.T) {
	providers := PublicProviders()
	data, err := json.Marshal(providers)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || contains(string(data), "fallback") || contains(string(data), "deepseek") {
		t.Fatalf("internal provider fields leaked: %s", data)
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
	tests := []string{
		`{"schema_version":2,"providers":{}}`,
		`{"schema_version":1,"default_fallback_probe_model":"m","providers":{"bad id":{"name":"Bad","home":"https://example.com","base_url":"https://api.example.com","fallback_probe_model":"m"}}}`,
		`{"schema_version":1,"default_fallback_probe_model":"m","providers":{"ok":{"name":"OK","home":"http://example.com","base_url":"https://api.example.com","fallback_probe_model":"m"}}}`,
		`{"schema_version":1,"default_fallback_probe_model":"m","providers":{"ok":{"name":"OK","home":"https://example.com","base_url":"https://api.example.com","fallback_probe_model":""}}}`,
	}
	for _, data := range tests {
		if _, err := ParseProviders([]byte(data)); err == nil {
			t.Errorf("ParseProviders(%s) unexpectedly succeeded", data)
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
