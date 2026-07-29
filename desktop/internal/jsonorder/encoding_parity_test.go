package jsonorder

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This encoder exists to reproduce json.dumps(ensure_ascii=False, indent=2)
// exactly, so the only test that means anything is a comparison with the real
// thing. Four of Go's defaults differ from it, and every one of them would
// produce valid JSON -- so nothing would fail until a byte comparison ran.

func pythonBin(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3.12", "python3"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if os.Getenv("ONEAGENT_REQUIRE_PARITY") != "" {
		t.Fatal("no Python on PATH, but ONEAGENT_REQUIRE_PARITY demands the comparison run")
	}
	t.Skip("no Python available to compare against")
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "agents.lock.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("walked to the filesystem root without finding agents.lock.json")
		}
		dir = parent
	}
}

// pythonDumps round-trips the input through Python: json.loads then
// json.dumps with the flags the adapters use. Because Python dicts preserve
// insertion order, the output carries the input's key order -- which is the
// property being compared.
func pythonDumps(t *testing.T, inputs []string) []string {
	t.Helper()
	script := `
import json, sys
out = []
for raw in json.loads(sys.argv[1]):
    out.append(json.dumps(json.loads(raw), ensure_ascii=False, indent=2) + "\n")
print(json.dumps(out))
`
	encoded, err := json.Marshal(inputs)
	if err != nil {
		t.Fatalf("cannot encode inputs: %v", err)
	}
	cmd := exec.Command(pythonBin(t), "-c", script, string(encoded))
	cmd.Dir = repoRoot(t)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python json.dumps failed: %v", err)
	}
	var results []string
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	return results
}

// documents are the shapes a config file actually takes, plus the ones where a
// Go default silently differs: keys out of alphabetical order, HTML-ish
// characters in a URL, non-ASCII values, numbers of every kind, and empty
// containers.
var documents = []string{
	`{}`,
	`{"a":1}`,
	`{"zeta":1,"alpha":2,"middle":3}`,
	`{"env":{"MY_VAR":"1","ANTHROPIC_MODEL":"old"},"theme":"dark"}`,
	`{"baseUrl":"https://x.test/?a=1&b=2<c>&d=e"}`,
	`{"model":"通义-max","note":"中文说明"}`,
	`{"emoji":"✅ done"}`,
	`{"empty":{},"list":[],"nested":{"deep":{}}}`,
	`{"int":1,"float":1.5,"neg":-2,"exp":1e10,"big":12345678901234567890,"zero":0}`,
	`{"flag":true,"off":false,"nothing":null}`,
	`{"items":[{"b":1,"a":2},"text",null,true,3]}`,
	`{"quotes":"say \"hi\"","backslash":"a\\b","newline":"a\nb","tab":"a\tb"}`,
	"{\"control\":\"\\u0001\\u001f\"}",
	`{"$schema":"https://opencode.ai/config.json","provider":{"oneagent":{"npm":"@ai-sdk/openai-compatible","name":"PPIO","options":{"baseURL":"https://api.ppio.com/openai/v1","apiKey":"{env:ONEAGENT_OPENCODE_API_KEY}"},"models":{"m":{"name":"m"}}}},"model":"oneagent/m"}`,
	// A key whose name needs escaping, and a deeply nested structure.
	`{"a\"b":{"c\\d":{"e\nf":1}}}`,
	`{"unicode_key_中文":"value"}`,
}

func TestParityEncodingMatchesPythonJSONDumps(t *testing.T) {
	want := pythonDumps(t, documents)
	for index, raw := range documents {
		object, err := Parse([]byte(raw))
		if err != nil {
			t.Errorf("cannot parse %s: %v", raw, err)
			continue
		}
		encoded, err := Marshal(object)
		if err != nil {
			t.Errorf("cannot marshal %s: %v", raw, err)
			continue
		}
		if string(encoded) != want[index] {
			t.Errorf("input %s\n  Go:\n%s\n  Python:\n%s", raw, string(encoded), want[index])
		}
	}
}

func TestParityAddingKeysProducesTheSameFileAsPython(t *testing.T) {
	// The realistic case: a user's existing settings.json gains the four
	// variables Claude Code needs. One of them is already present, so it must
	// stay where it was while the others append -- which is what a Go map would
	// get wrong in two different ways at once.
	existing := `{"theme":"dark","env":{"MY_VAR":"1","ANTHROPIC_MODEL":"old"},"other":true}`

	script := `
import json, sys
data = json.loads(sys.argv[1])
env = data.setdefault("env", {})
env["ANTHROPIC_BASE_URL"] = "https://api.ppio.com/anthropic"
env["ANTHROPIC_AUTH_TOKEN"] = "sk-parity"
env["ANTHROPIC_MODEL"] = "new-model"
env["ANTHROPIC_SMALL_FAST_MODEL"] = "new-model"
print(json.dumps(json.dumps(data, ensure_ascii=False, indent=2) + "\n"))
`
	cmd := exec.Command(pythonBin(t), "-c", script, existing)
	cmd.Dir = repoRoot(t)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed: %v", err)
	}
	var want string
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}

	object, err := Parse([]byte(existing))
	if err != nil {
		t.Fatalf("cannot parse: %v", err)
	}
	env, err := object.Child("env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env.Set("ANTHROPIC_BASE_URL", "https://api.ppio.com/anthropic")
	env.Set("ANTHROPIC_AUTH_TOKEN", "sk-parity")
	env.Set("ANTHROPIC_MODEL", "new-model")
	env.Set("ANTHROPIC_SMALL_FAST_MODEL", "new-model")

	got, err := Marshal(object)
	if err != nil {
		t.Fatalf("cannot marshal: %v", err)
	}
	if string(got) != want {
		t.Fatalf("files differ:\n  Go:\n%s\n  Python:\n%s", got, want)
	}
	// Stated separately so a failure says which property broke.
	if !strings.Contains(want, "\"MY_VAR\": \"1\",\n    \"ANTHROPIC_MODEL\": \"new-model\"") {
		t.Error("the fixture no longer exercises in-place update; revisit it")
	}
}
