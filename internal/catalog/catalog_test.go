package catalog

import (
	"encoding/json"
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
	if automatic != 5 {
		t.Fatalf("automatic Agent count = %d, want 5", automatic)
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

func contains(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
