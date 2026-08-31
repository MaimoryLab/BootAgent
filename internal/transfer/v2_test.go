package transfer

import (
	"testing"
)

func TestV2RoundTrip(t *testing.T) {
	data, err := Build(map[string][]byte{"providers.json": []byte(`{"providers":[]}`), "skills/demo/SKILL.md": []byte("# Demo")})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.Version != Version || string(pkg.Files["skills/demo/SKILL.md"]) != "# Demo" {
		t.Fatalf("package = %#v", pkg)
	}
}

func TestV2RejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "skills/../escape", `skills\\escape`, "manifest.json"} {
		if _, err := Build(map[string][]byte{name: []byte("x")}); err == nil {
			t.Fatalf("Build accepted %q", name)
		}
	}
}
