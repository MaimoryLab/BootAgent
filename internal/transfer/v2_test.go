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

func TestPreviewPackageValidatesSectionsAndNestedSkills(t *testing.T) {
	nested, err := Build(map[string][]byte{"SKILL.md": []byte("# Demo")})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Build(map[string][]byte{
		"providers.json":        []byte(`{"providers":[{}]}`),
		"profiles.json":         []byte(`[{}]`),
		"mcp.json":              []byte(`{"servers":[{}]}`),
		"skills/demo.skill.zip": nested,
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, _, err := PreviewPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Providers != 1 || preview.Profiles != 1 || preview.MCP != 1 || len(preview.Skills) != 1 || preview.Skills[0] != "demo" {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestPreviewPackageRejectsInvalidNestedSkill(t *testing.T) {
	data, err := Build(map[string][]byte{"skills/demo.skill.zip": []byte("not a zip")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := PreviewPackage(data); err == nil {
		t.Fatal("invalid nested archive accepted")
	}
}
