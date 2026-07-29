package shellquote

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The file this quoting produces is sourced by a shell and holds an API key. A
// value that escapes its quotes does not just break the file -- it lets the rest
// of the key be interpreted as shell. So the rules are compared against the real
// shlex.quote rather than trusted to a reading of the documentation.

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

func runPython(t *testing.T, expression string, inputs []string) []string {
	t.Helper()
	script := `
import json, shlex, sys
sys.path.insert(0, sys.argv[1])
from oneagent.installer import _powershell_quote
print(json.dumps([eval(sys.argv[2]) for value in json.loads(sys.argv[3])]))
`
	encoded, err := json.Marshal(inputs)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	cmd := exec.Command(pythonBin(t), "-c", script, repoRoot(t), expression, string(encoded))
	cmd.Dir = repoRoot(t)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed for %q: %v", expression, err)
	}
	var results []string
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	return results
}

// values are what actually reaches this: endpoints and API keys. The hostile
// ones are included because the key comes from a user paste and nothing
// upstream guarantees its shape.
var values = []string{
	"",
	"plain",
	"sk-abc123",
	"sk_under-score.dot",
	"https://api.ppio.com/openai/v1",
	"http://127.0.0.1:9000/v1",
	"with space",
	"tab\there",
	"new\nline",
	"single'quote",
	"double\"quote",
	"both'and\"quotes",
	"'leading",
	"trailing'",
	"''",
	"a$b",
	"a${b}c",
	"a`b`c",
	"a\\b",
	"a;rm -rf /",
	"a&&b",
	"a|b",
	"a>b<c",
	"a#b",
	"a!b",
	"a*b?c[d]",
	"a~b",
	"a(b)c",
	"a{b}c",
	"%2F",
	"a=b",
	"a,b",
	"a:b",
	"a@b",
	"a+b",
	"a/b",
	"a.b",
	"a-b",
	"a_b",
	"中文",
	"emoji✅",
	"$(whoami)",
	"`whoami`",
	"\\'",
	"'\"'\"'",
}

func TestParityPosixQuotingMatchesShlex(t *testing.T) {
	want := runPython(t, "shlex.quote(value)", values)
	for index, value := range values {
		if got := Posix(value); got != want[index] {
			t.Errorf("Posix(%q):\n  Go:     %q\n  Python: %q", value, got, want[index])
		}
	}
}

func TestParityPowerShellQuotingMatchesPython(t *testing.T) {
	want := runPython(t, "_powershell_quote(value)", values)
	for index, value := range values {
		if got := PowerShell(value); got != want[index] {
			t.Errorf("PowerShell(%q):\n  Go:     %q\n  Python: %q", value, got, want[index])
		}
	}
}

func TestParityAQuotedValueSurvivesARealShell(t *testing.T) {
	// The comparison above proves the two implementations agree. This proves
	// they are both right: an actual shell reads the value back unchanged, which
	// is the property the file depends on.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available")
	}
	for _, value := range values {
		if strings.Contains(value, "\x00") {
			continue
		}
		script := "printf %s " + Posix(value)
		output, err := exec.Command("sh", "-c", script).Output()
		if err != nil {
			t.Errorf("%q: the shell rejected the quoted form %s: %v", value, Posix(value), err)
			continue
		}
		if string(output) != value {
			t.Errorf("%q round-tripped through sh as %q", value, output)
		}
	}
}

func TestParityAHostileKeyCannotEscapeIntoShellCommands(t *testing.T) {
	// The failure this quoting prevents: a key ending the string and letting the
	// remainder run. If any of these executed, the marker file would exist.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available")
	}
	marker := filepath.Join(t.TempDir(), "escaped")
	hostile := []string{
		"'; touch " + marker + "; '",
		"$(touch " + marker + ")",
		"`touch " + marker + "`",
		"'; touch " + marker + " #",
	}
	for _, value := range hostile {
		script := "OPENAI_API_KEY=" + Posix(value) + "\nprintf %s \"$OPENAI_API_KEY\""
		output, err := exec.Command("sh", "-c", script).Output()
		if err != nil {
			t.Errorf("%q: the shell rejected the quoted form: %v", value, err)
			continue
		}
		if string(output) != value {
			t.Errorf("%q was altered by the shell: got %q", value, output)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a quoted value executed a command; the quoting does not hold")
	}
}
