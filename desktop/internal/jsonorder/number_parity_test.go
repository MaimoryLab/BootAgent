package jsonorder

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// A number in a config file belongs to the user: a timeout, a token limit, a
// temperature. Rewriting one is the silent edit the "preserve what we do not
// manage" rule exists to prevent, so the rendering has to match Python rather
// than merely produce valid JSON.
//
// This is the divergence the byte comparison found that no hand-written test
// would have: Python's json.loads promotes any exponent-carrying number to a
// float and json.dumps then writes repr() of it, so "1e10" comes back as
// "10000000000.0".

// numberForms cover both sides of every boundary Python's repr uses: the
// exponent thresholds at -4 and 16, integers too large for float64, negative
// zero, and the extremes of the range.
var numberForms = []string{
	"0", "1", "-1", "42", "-42",
	"0.0", "1.0", "-1.0", "1.5", "-2.5", "0.1", "100.0", "-0.0",
	"12345678901234567890",
	"-12345678901234567890",
	"9007199254740993",
	"1e0", "2e0", "1e1", "1e10", "1E10", "1e15", "1e16", "1e17",
	"1e-1", "1e-4", "1e-5", "1e-6", "1e-10",
	"1.5e3", "3.0e2", "-1.5e3", "1.23e-7",
	"1.7976931348623157e308",
	"5e-324",
	"0e0", "0.0e0",
	"1.000000000000001",
	"0.30000000000000004",
}

func pythonRenderNumbers(t *testing.T, forms []string) []string {
	t.Helper()
	script := `
import json, sys
out = []
for raw in json.loads(sys.argv[1]):
    out.append(json.dumps(json.loads(raw)))
print(json.dumps(out))
`
	encoded, err := json.Marshal(forms)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	cmd := exec.Command(pythonBin(t), "-c", script, string(encoded))
	cmd.Dir = repoRoot(t)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed: %v", err)
	}
	var results []string
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	return results
}

func TestParityNumberRenderingMatchesPythonAcrossTheRange(t *testing.T) {
	want := pythonRenderNumbers(t, numberForms)
	for index, form := range numberForms {
		got := renderNumber(json.Number(form))
		if got != want[index] {
			t.Errorf("%s:\n  Go:     %s\n  Python: %s", form, got, want[index])
		}
	}
}

func TestParityNumbersInsideADocumentSurviveTheRoundTrip(t *testing.T) {
	// The unit above checks the formatter; this checks it is actually reached
	// from the document path, nested and inside arrays as well as at the top.
	document := `{"top":1e10,"nested":{"deep":1e-5},"list":[1e16,2e0,3],"plain":1.5}`
	script := `
import json, sys
print(json.dumps(json.dumps(json.loads(sys.argv[1]), ensure_ascii=False, indent=2) + "\n"))
`
	cmd := exec.Command(pythonBin(t), "-c", script, document)
	cmd.Dir = repoRoot(t)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed: %v", err)
	}
	var want string
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}

	object, err := Parse([]byte(document))
	if err != nil {
		t.Fatalf("cannot parse: %v", err)
	}
	got, err := Marshal(object)
	if err != nil {
		t.Fatalf("cannot marshal: %v", err)
	}
	if string(got) != want {
		t.Fatalf("documents differ:\n  Go:\n%s\n  Python:\n%s", got, want)
	}
}
