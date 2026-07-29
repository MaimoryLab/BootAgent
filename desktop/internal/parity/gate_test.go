// Package parity holds the gate that keeps the cross-language tests honest.
//
// The tests it guards live next to the code they check, in oerr and runtime.
// The count lives here because a self-check inside the file it counts cannot
// survive that file being deleted -- and `go test -run` reports success when
// its pattern matches nothing, so deleting a parity file would turn CI green
// rather than red. Checking from outside is what closes that.
package parity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// expected names every file carrying cross-language tests, and the minimum
// number each must hold. Adding a parity file means adding a line here; that is
// the point, because the alternative is a gate that silently stops covering it.
var expected = map[string]int{
	filepath.Join("oerr", "codes_parity_test.go"):       3,
	filepath.Join("runtime", "home_parity_test.go"):     2,
	filepath.Join("catalog", "embed_parity_test.go"):    6,
	filepath.Join("provider", "urls_parity_test.go"):    9,
	filepath.Join("shellquote", "quote_parity_test.go"): 4,
	// The byte comparison the migration's stop-loss checkpoint depends on.
	filepath.Join("securefs", "bytes_parity_test.go"):     4,
	filepath.Join("jsonorder", "encoding_parity_test.go"): 2,
	filepath.Join("jsonorder", "number_parity_test.go"):   2,
}

var testFunc = regexp.MustCompile(`func (Test\w+)`)

func internalDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("cannot resolve the internal directory: %v", err)
	}
	return dir
}

func TestEveryDeclaredParityFileStillExists(t *testing.T) {
	// The failure this exists for: a deleted parity file leaves CI's
	// `-run TestParity` step matching nothing, which exits 0.
	root := internalDir(t)
	for relative := range expected {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("%s is gone; the cross-language gate it carried no longer runs: %v", relative, err)
		}
	}
}

func TestEveryParityTestIsNamedSoTheCIFilterFindsIt(t *testing.T) {
	root := internalDir(t)
	for relative, minimum := range expected {
		source, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			// Reported by the test above; nothing to add here.
			continue
		}
		found := 0
		for _, match := range testFunc.FindAllStringSubmatch(string(source), -1) {
			if strings.HasPrefix(match[1], "TestParity") {
				found++
				continue
			}
			t.Errorf("%s: %s would be skipped by -run TestParity", relative, match[1])
		}
		if found < minimum {
			t.Errorf("%s: found %d TestParity tests, expected at least %d; was one renamed or removed?", relative, found, minimum)
		}
	}
}

func TestNoParityFileEscapesTheDeclaredList(t *testing.T) {
	// A parity file nobody declared is as much a gap as a deleted one: it looks
	// covered, and the count above would not notice if it lost its tests.
	root := internalDir(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("cannot read %s: %v", root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		for _, file := range files {
			name := file.Name()
			if !strings.Contains(name, "parity") || !strings.HasSuffix(name, "_test.go") {
				continue
			}
			relative := filepath.Join(entry.Name(), name)
			if _, declared := expected[relative]; !declared {
				t.Errorf("%s carries parity tests but is not in the declared list", relative)
			}
		}
	}
}
