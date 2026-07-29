package install

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// I got the version-parsing expectations wrong twice writing these tests by
// hand, which is the argument for comparing against the real implementation
// instead of reasoning about a regex. The same applies to registry
// normalisation and the failure summary: all three are small enough to compare
// exhaustively and consequential enough to be worth it.

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

// runPython evaluates one expression per input against the real module. A
// refusal comes back as "ERROR" so the rejection boundary can be compared too.
func runPython(t *testing.T, expression string, inputs []string) []string {
	t.Helper()
	root := repoRoot(t)
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent import installer
from oneagent.errors import OneAgentError

results = []
for value in json.loads(sys.argv[3]):
    try:
        results.append(str(eval(sys.argv[2])))
    except OneAgentError:
        results.append("ERROR")
print(json.dumps(results))
`
	encoded, err := json.Marshal(inputs)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	cmd := exec.Command(pythonBin(t), "-c", script, root, expression, string(encoded))
	cmd.Dir = root
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

// versionOutputs are what Agents actually print, plus the numeric shapes where a
// regex difference would silently report the wrong installed version.
var versionOutputs = []string{
	"codex-cli 0.145.0",
	"0.145.0",
	"v0.145.0",
	"claude 1.2.3 (build 4)",
	"Python 3.12.9",
	"Python 3.14.1",
	"1.0.0-beta.2",
	"1.0.0+build.5",
	"1.0.0-rc.1+build.2",
	"banner\nversion 9.8.7\nmore",
	"no version here",
	"",
	"1.2",
	"1",
	"12345.6.7",
	"20260729.1.2.3",
	"1.2.3.4",
	"0.0.0",
	"10.20.30",
	"aider 0.86.1",
	"opencode 0.4.2\n",
	"kilo v1.0.0\r\n",
	"1.2.3-",
	"1.2.3+",
	"a1.2.3",
	"1.2.3a",
	".1.2.3",
}

func TestParityVersionParsingMatchesPython(t *testing.T) {
	// What this decides: whether an installed Agent is reported at the locked
	// version. A difference shows up as a spurious reinstall or a version the
	// overview reports wrongly.
	want := runPython(t, "installer._version_from_output(value) or ''", versionOutputs)
	for index, input := range versionOutputs {
		if got := VersionFromOutput(input); got != want[index] {
			t.Errorf("VersionFromOutput(%q):\n  Go:     %q\n  Python: %q", input, got, want[index])
		}
	}
}

// registryValues mix mirror ids, acceptable URLs and every rejection reason.
var registryValues = []string{
	"",
	"official",
	"npmmirror",
	"https://registry.npmjs.org/",
	"https://registry.npmjs.org",
	"https://mirror.example.com/npm",
	"https://mirror.example.com/npm/",
	"http://mirror.example.com/",
	"ftp://mirror.example.com/",
	"mirror.example.com",
	"//mirror.example.com",
	"https://",
	"https:///nohost",
	"https://user:pass@mirror.example.com/",
	"https://token@mirror.example.com/",
	// An empty userinfo section carries no credential, but Go builds a Userinfo
	// for it while Python tests username and password for truthiness. Checking
	// `parsed.User != nil` therefore refused a URL Python accepts.
	"https://@mirror.example.com/",
	"https://:@mirror.example.com/",
	"https://:pass@mirror.example.com/",
	"https://mirror.example.com/\nHost: evil",
	"https://mirror.example.com/\r",
	"https://mirror.example.com/\x00",
	"https://mirror.example.com/\x7f",
	"https://mirror.example.com:8443/",
	"not-a-known-mirror",
	"HTTPS://MIRROR.EXAMPLE.COM/",
}

func TestParityRegistryResolutionAgreesIncludingWhatToRefuse(t *testing.T) {
	// A registry URL reaches the installer environment and the install log, so a
	// value one side accepts and the other refuses is either a leak or a blocked
	// install.
	want := runPython(t, "installer.resolve_registry(value)", registryValues)
	for index, input := range registryValues {
		got := "ERROR"
		if resolved, err := ResolveRegistry(input); err == nil {
			got = resolved
		}
		if got != want[index] {
			t.Errorf("ResolveRegistry(%q):\n  Go:     %q\n  Python: %q", input, got, want[index])
		}
	}
}

func TestParityTheFailureSummaryMatchesPython(t *testing.T) {
	// The summary is shown to the user and written to the log, so both the
	// redaction and the truncation have to agree.
	root := repoRoot(t)
	cases := []struct {
		name   string
		env    map[string]string
		stdout string
		stderr string
	}{
		{"short error", map[string]string{}, "", "npm ERR! code E404"},
		{"many lines", map[string]string{}, "", "a\nb\nc\nd\ne\nf"},
		{"blank lines", map[string]string{}, "", "real\n\n\n"},
		{"colour codes", map[string]string{}, "", "\x1b[31mred\x1b[0m error"},
		{"both streams", map[string]string{}, "on stdout", "on stderr"},
		{"with a secret", map[string]string{"API_KEY": "sk-secret"}, "", "key=sk-secret failed"},
		{"secret in both", map[string]string{"ANTHROPIC_AUTH_TOKEN": "sk-tok"}, "sk-tok", "sk-tok"},
		{"empty", map[string]string{}, "", ""},
		{"whitespace only", map[string]string{}, "   ", "  \n\t"},
		{"non-ascii", map[string]string{}, "", "安装失败：找不到包"},
		{"indented", map[string]string{}, "", "  leading spaces\n\ttab"},
		// Every case above stays under the 600 limit, so until these three the
		// truncation this comment claims both sides agree on was never compared.
		// It did not: Python slices code points and the Go version sliced bytes,
		// so a Chinese error -- which is what npmmirror returns -- was cut to a
		// third of the text, mid-character, leaving a tail encoding/json rewrites
		// to U+FFFD.
		{"long ascii past the limit", map[string]string{}, "", strings.Repeat("e", 900)},
		{"long chinese past the limit", map[string]string{}, "", strings.Repeat("安装失败", 300)},
		{"multibyte astride the cut", map[string]string{}, "",
			strings.Repeat("x", 598) + strings.Repeat("装", 5)},
	}

	script := `
import json, subprocess, sys
sys.path.insert(0, sys.argv[1])
from oneagent.installer import Runtime, _installer_failure_detail

payload = json.loads(sys.argv[2])
runtime = Runtime.create(home=None, os_id="linux", env=payload["env"])
result = subprocess.CompletedProcess([], 1, payload["stdout"], payload["stderr"])
print(json.dumps(_installer_failure_detail(runtime, result)))
`
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"env": testCase.env, "stdout": testCase.stdout, "stderr": testCase.stderr,
			})
			if err != nil {
				t.Fatalf("cannot encode: %v", err)
			}
			cmd := exec.Command(pythonBin(t), "-c", script, root, string(payload))
			cmd.Dir = root
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("python failed: %v", err)
			}
			var want string
			if err := json.Unmarshal(output, &want); err != nil {
				t.Fatalf("cannot read python output: %v", err)
			}
			if got := FailureDetail(testCase.env, testCase.stdout, testCase.stderr); got != want {
				t.Errorf("FailureDetail:\n  Go:     %q\n  Python: %q", got, want)
			}
		})
	}
}

func TestParityALongSummaryIsTruncatedTheSameWay(t *testing.T) {
	// Separate because the input is generated rather than written out, and the
	// limit is where a byte-vs-rune difference would show up.
	root := repoRoot(t)
	long := ""
	for index := 0; index < 40; index++ {
		long += "npm ERR! a line long enough to matter number " + string(rune('a'+index%26)) + "\n"
	}
	script := `
import json, subprocess, sys
sys.path.insert(0, sys.argv[1])
from oneagent.installer import Runtime, _installer_failure_detail
runtime = Runtime.create(home=None, os_id="linux", env={})
result = subprocess.CompletedProcess([], 1, "", sys.argv[2])
print(json.dumps(_installer_failure_detail(runtime, result)))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root, long)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed: %v", err)
	}
	var want string
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	if got := FailureDetail(map[string]string{}, "", long); got != want {
		t.Errorf("truncated summary:\n  Go:     %q\n  Python: %q", got, want)
	}
}
