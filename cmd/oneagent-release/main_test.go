package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	oneagent "github.com/MaimoryLab/OneAgent"
)

func TestParseFrontendPackages(t *testing.T) {
	packageDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(packageDir, "LICENSE"), []byte("MIT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string][]pnpmLicensePackage{
		"MIT": {{Name: "react", Versions: []string{"19.2.8"}, Paths: []string{packageDir}, License: "MIT"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	licenseDir := t.TempDir()
	items, err := parseFrontendPackages(data, licenseDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "react" || items[0].Version != "19.2.8" || items[0].License != "MIT" {
		t.Fatalf("frontend packages = %#v", items)
	}
	if _, err := os.Stat(filepath.Join(licenseDir, filepath.Base(items[0].LicenseFile))); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveHasValidAgentCatalog(t *testing.T) {
	valid, err := oneagent.EmbeddedAgentLock()
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		data []byte
		want bool
	}{
		"valid":   {valid, true},
		"invalid": {[]byte(`{"agents":`), false},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "release.zip")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			archive := zip.NewWriter(file)
			entry, err := archive.Create("agents.lock.json")
			if err == nil {
				_, err = entry.Write(test.data)
			}
			if closeErr := archive.Close(); err == nil {
				err = closeErr
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := archiveHasValidAgentCatalog(path); got != test.want {
				t.Fatalf("archiveHasValidAgentCatalog() = %v, want %v", got, test.want)
			}
		})
	}
}
