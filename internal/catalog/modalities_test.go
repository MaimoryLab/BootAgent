package catalog

import (
	"strings"
	"testing"
)

func TestEmbeddedModalitiesParse(t *testing.T) {
	manifest, err := LoadEmbeddedModalities()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.IDRules) == 0 || manifest.Providers["novita"].NativeBaseURL != "https://api.novita.ai" {
		t.Fatalf("embedded modalities = %#v", manifest)
	}
	// The returned value is a copy; mutating it must not leak into the next load.
	manifest.IDRules[0].Capabilities[0] = "mutated"
	again, _ := LoadEmbeddedModalities()
	if again.IDRules[0].Capabilities[0] == "mutated" {
		t.Fatal("LoadEmbeddedModalities returned shared state")
	}
}

func TestParseModalitiesRejectsBrokenEntries(t *testing.T) {
	valid := `{"schema_version":1,"id_rules":[{"pattern":"^x","capabilities":["vision"]}],"providers":{"novita":{"native_base_url":"https://api.novita.ai","generation":[]}}}`
	if _, err := ParseModalities([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"schema":         strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1),
		"no rules":       `{"schema_version":1,"id_rules":[],"providers":{}}`,
		"bad pattern":    strings.Replace(valid, `"^x"`, `"(unclosed"`, 1),
		"bad exclude":    strings.Replace(valid, `"pattern":"^x"`, `"pattern":"^x","exclude":"[z"`, 1),
		"no capability":  strings.Replace(valid, `["vision"]`, `[]`, 1),
		"unknown vendor": strings.Replace(valid, `"novita"`, `"nobody"`, 1),
		"http base":      strings.Replace(valid, `https://api.novita.ai`, `http://api.novita.ai`, 1),
		"trailing slash": strings.Replace(valid, `https://api.novita.ai`, `https://api.novita.ai/`, 1),
		"incomplete gen": strings.Replace(valid, `"generation":[]`, `"generation":[{"id":"m","capabilities":["video-generation"]}]`, 1),
		"duplicate gen":  strings.Replace(valid, `"generation":[]`, `"generation":[{"id":"m","capabilities":["video-generation"],"adapter":"novita-async"},{"id":"m","capabilities":["video-generation"],"adapter":"novita-async"}]`, 1),
		"not json":       `{`,
	}
	for name, data := range cases {
		if _, err := ParseModalities([]byte(data)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
