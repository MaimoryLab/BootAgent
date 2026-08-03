package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	oneagent "github.com/MaimoryLab/OneAgent"
)

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
