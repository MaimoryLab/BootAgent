package oerr

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The exit codes are an external contract, and during the migration two
// implementations claim to honour it. Rather than copy the table into Go and
// trust that it stays copied, this test reads the Python literal and compares.
// A code added, removed or renumbered on either side fails here.
//
// When oneagent/errors.py is finally deleted, this test becomes an assertion
// against a frozen fixture rather than being removed -- the contract outlives
// the Python file.

var pythonEntry = regexp.MustCompile(`^\s*"([A-Z_]+)"\s*:\s*(\d+)\s*,\s*$`)

// CI runs these separately with -run TestParity so a cross-language failure is
// named in its own step. But `go test -run` reports success when its pattern
// matches nothing, so renaming one of these would silently retire the gate
// rather than break it. This test counts them, which turns that into a failure.
func TestParityGateCoversEveryCrossLanguageTest(t *testing.T) {
	source, err := os.ReadFile("codes_parity_test.go")
	if err != nil {
		t.Fatalf("cannot read this test file: %v", err)
	}
	found := regexp.MustCompile(`func (Test\w+)`).FindAllStringSubmatch(string(source), -1)
	prefixed := 0
	for _, match := range found {
		if strings.HasPrefix(match[1], "TestParity") {
			prefixed++
			continue
		}
		t.Errorf("%s lives in the parity file but CI's -run TestParity would skip it", match[1])
	}
	if prefixed < 4 {
		t.Fatalf("found %d TestParity tests, expected at least 4; did one get renamed?", prefixed)
	}
}

func pythonExitCodes(t *testing.T) map[string]int {
	t.Helper()
	// Relative to desktop/internal/oerr.
	path := filepath.Join("..", "..", "..", "oneagent", "errors.py")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the Python source of truth at %s: %v", path, err)
	}
	codes := map[string]int{}
	inTable := false
	for _, line := range splitLines(string(source)) {
		if !inTable {
			if line == "EXIT_CODES = {" {
				inTable = true
			}
			continue
		}
		if line == "}" {
			inTable = false
			break
		}
		match := pythonEntry.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("unparsed line inside EXIT_CODES: %q", line)
		}
		value, err := strconv.Atoi(match[2])
		if err != nil {
			t.Fatalf("exit code for %s is not a number: %v", match[1], err)
		}
		codes[match[1]] = value
	}
	if len(codes) == 0 {
		t.Fatal("found no entries in EXIT_CODES; the parser or the file changed shape")
	}
	return codes
}

func splitLines(text string) []string {
	lines := []string{}
	start := 0
	for index := 0; index < len(text); index++ {
		if text[index] == '\n' {
			line := text[start:index]
			if length := len(line); length > 0 && line[length-1] == '\r' {
				line = line[:length-1]
			}
			lines = append(lines, line)
			start = index + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

func TestParityExitCodesMatchThePythonSourceOfTruth(t *testing.T) {
	python := pythonExitCodes(t)

	for code, want := range python {
		got, present := ExitCodes[code]
		if !present {
			t.Errorf("%s is defined in errors.py but missing from Go", code)
			continue
		}
		if got != want {
			t.Errorf("%s exits %d in Go but %d in errors.py", code, got, want)
		}
	}
	for code := range ExitCodes {
		if _, present := python[code]; !present {
			t.Errorf("%s is defined in Go but missing from errors.py", code)
		}
	}
}

func TestParityUnknownCodeFallbackMatchesPython(t *testing.T) {
	// errors.py:34 spells the default as the literal 10. Reading it here keeps
	// the two sides from drifting if that literal is ever changed.
	path := filepath.Join("..", "..", "..", "oneagent", "errors.py")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read errors.py: %v", err)
	}
	fallback := regexp.MustCompile(`EXIT_CODES\.get\(code,\s*(\d+)\)`).FindStringSubmatch(string(source))
	if fallback == nil {
		t.Fatal("could not find the EXIT_CODES.get default in errors.py")
	}
	want, err := strconv.Atoi(fallback[1])
	if err != nil {
		t.Fatalf("the default is not a number: %v", err)
	}
	if UnknownExitCode != want {
		t.Fatalf("Go falls back to %d, errors.py falls back to %d", UnknownExitCode, want)
	}
}

func TestParityPayloadKeysMatchPythonToDict(t *testing.T) {
	// to_dict() is what every HTTP error response and CLI JSON failure is
	// built from, so its key set is as much a contract as the codes.
	path := filepath.Join("..", "..", "..", "oneagent", "errors.py")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read errors.py: %v", err)
	}
	body := regexp.MustCompile(`(?s)def to_dict\(self\).*?return \{(.*?)\}`).FindStringSubmatch(string(source))
	if body == nil {
		t.Fatal("could not find to_dict in errors.py")
	}
	python := map[string]bool{}
	for _, match := range regexp.MustCompile(`"([a-z_]+)":`).FindAllStringSubmatch(body[1], -1) {
		python[match[1]] = true
	}
	got := New("TIMEOUT", "x").Payload()
	for key := range python {
		if _, present := got[key]; !present {
			t.Errorf("to_dict emits %q but the Go payload does not", key)
		}
	}
	for key := range got {
		if !python[key] {
			t.Errorf("the Go payload emits %q but to_dict does not", key)
		}
	}
}
