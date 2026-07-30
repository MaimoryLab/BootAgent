// Package parity holds the gate that keeps the cross-language tests honest.
//
// The tests it guards live next to the code they check, in oerr and runtime.
// The count lives here because a self-check inside the file it counts cannot
// survive that file being deleted -- and `go test -run` reports success when
// its pattern matches nothing, so deleting a parity file would turn CI green
// rather than red. Checking from outside is what closes that.
package parity

import (
	"io/fs"
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
	filepath.Join("oerr", "codes_parity_test.go"):    3,
	filepath.Join("runtime", "home_parity_test.go"):  2,
	filepath.Join("catalog", "embed_parity_test.go"): 6,
	filepath.Join("provider", "urls_parity_test.go"): 9,
	// The probe classification decides what advice the wizard gives, so these
	// drive both implementations over the same canned responses rather than
	// reaching a provider -- the regular CI run touches no network.
	filepath.Join("provider", "probe_parity_test.go"):   4,
	filepath.Join("shellquote", "quote_parity_test.go"): 5,
	// The byte comparison the migration's stop-loss checkpoint depends on.
	filepath.Join("securefs", "bytes_parity_test.go"):     4,
	filepath.Join("jsonorder", "encoding_parity_test.go"): 2,
	filepath.Join("jsonorder", "number_parity_test.go"):   2,
	filepath.Join("config", "adapters_parity_test.go"):    4,
	filepath.Join("config", "readers_parity_test.go"):     7,
	filepath.Join("install", "install_parity_test.go"):    4,
	// The profile store and the Agent bindings persist across runs and decide
	// where a plaintext key is written, so these compare the bytes rather than a
	// parsed shape -- both implementations have to keep reading each other's
	// output for as long as the migration lasts.
	filepath.Join("profile", "store_parity_test.go"): 3,
	// The orchestration composes everything below it, so this compares the whole
	// response and every file the run wrote -- including the Agent configs
	// outside .oneagent, which an earlier scope silently ignored.
	filepath.Join("app", "install_parity_test.go"): 1,
	// The status payload is what the entire frontend reads, and the one response
	// that must be provably free of credential material -- three of the five config
	// formats hold the key in plain text and this payload reports on all of them.
	filepath.Join("app", "status_parity_test.go"): 2,
	// Outside internal/, hence the "..": the CLI's exit codes are read by
	// install.sh and by CI, which cannot read a message. Every other entry here
	// compares what a layer computes; this one compares what the process returns,
	// because the argument parsing and the code mapping sit above every layer the
	// rest of this table reaches. It was verified by hand once during the review
	// that first ran the Go core as a program -- a contract checked once with no
	// gate behind it is exactly what this file exists to prevent.
	filepath.Join("..", "cmd", "oneagent", "exitcode_parity_test.go"): 3,
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
	//
	// Walks the whole module rather than the immediate children of internal/. The
	// first version read one level of internal/, so a parity file under cmd/ -- or
	// nested any deeper -- was invisible to this check, which is the same shape of
	// hole the gate exists to close.
	root := filepath.Dir(internalDir(t))
	for _, relative := range parityFilesUnder(t, root) {
		if _, declared := expected[relative]; !declared {
			t.Errorf("%s carries parity tests but is not in the declared list", relative)
		}
	}
}

// parityFilesUnder lists every file carrying parity tests, relative to internal/
// so the paths match the declared list.
func parityFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	found := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.Contains(name, "parity") || !strings.HasSuffix(name, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(filepath.Join(root, "internal"), path)
		if err != nil {
			return err
		}
		found = append(found, relative)
		return nil
	})
	if err != nil {
		t.Fatalf("cannot walk %s: %v", root, err)
	}
	return found
}
