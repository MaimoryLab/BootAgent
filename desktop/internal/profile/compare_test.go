package profile

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// timestampValue matches a rendered timestamp inside a JSON string value. The two
// runs happen microseconds apart, so a literal comparison would fail on every
// case and prove nothing. Replaced with a marker so everything around it -- key
// order, escaping, indentation, the trailing newline -- is still compared byte for
// byte, and the format itself is asserted separately below.
var timestampValue = regexp.MustCompile(`"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{6})?Z"`)

// timestampShape is what both sides must render. Asserted rather than assumed
// because Python omits the fractional part when microseconds are zero, so a
// format string that always emits six digits agrees only most of the time.
var timestampShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{6})?Z$`)

// backupSuffix matches the timestamped backup files an overwrite leaves behind.
var backupSuffix = regexp.MustCompile(`\.backup-\d{8}-\d{6}$`)

// compareTree asserts the two homes hold the same files with the same bytes.
func compareTree(t *testing.T, pythonHome, goHome string) {
	t.Helper()
	python := collectTree(t, pythonHome)
	golang := collectTree(t, goHome)

	names := map[string]bool{}
	for name := range python {
		names[name] = true
	}
	for name := range golang {
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	for _, name := range ordered {
		pythonContent, inPython := python[name]
		goContent, inGo := golang[name]
		switch {
		case !inGo:
			t.Errorf("%s: Python wrote it, Go did not", name)
		case !inPython:
			t.Errorf("%s: Go wrote it, Python did not", name)
		case normaliseTimestamps(pythonContent) != normaliseTimestamps(goContent):
			t.Errorf("%s differs:\n  Python: %q\n  Go:     %q", name, pythonContent, goContent)
		}
	}

	// Both sides having the same wrong format would pass the comparison above,
	// since each timestamp is replaced on both sides.
	for _, name := range ordered {
		for _, found := range timestampValue.FindAllString(golang[name], -1) {
			unquoted := strings.Trim(found, `"`)
			if !timestampShape.MatchString(unquoted) {
				t.Errorf("%s: %q is not the timestamp shape Python writes", name, unquoted)
			}
		}
	}
}

// collectTree reads every file under .oneagent, keyed by its relative path.
func collectTree(t *testing.T, home string) map[string]string {
	t.Helper()
	files := map[string]string{}
	root := filepath.Join(home, ".oneagent")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// A backup carries a timestamp in its name and its presence depends on
		// what the run overwrote, which is asserted by the securefs tests instead.
		if backupSuffix.MatchString(relative) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("cannot read %s: %v", root, err)
	}
	return files
}

func normaliseTimestamps(content string) string {
	return timestampValue.ReplaceAllString(content, `"<timestamp>"`)
}
