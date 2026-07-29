package runtime

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// Where the home directory comes from decides where every Agent's configuration
// is written, so the two implementations have to agree on it -- including in the
// cases nobody hits on a developer machine.
//
// The one that nearly slipped through: Python's Path.home() falls back to the
// passwd database when HOME is unset, while Go's os.UserHomeDir reads $HOME
// alone and errors otherwise. Returning "" there would have made Go resolve no
// home where Python resolves one, and that would not have surfaced as a clear
// failure -- it would have surfaced as a different exit code from somewhere
// further up, which is an external contract.

func pythonBin(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3.12", "python3"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	// Skipping is right on a developer machine without Python, but on CI it
	// would retire a cross-language gate without anything going red. CI sets
	// this so a missing interpreter fails instead.
	if os.Getenv("ONEAGENT_REQUIRE_PARITY") != "" {
		t.Fatal("no Python on PATH, but ONEAGENT_REQUIRE_PARITY demands the comparison run")
	}
	t.Skip("no Python available to compare against")
	return ""
}

// pythonResolveHome runs resolve_home from the Python core under the given
// environment. Calling the real function rather than reimplementing it is the
// point: a reimplementation would only prove the test agrees with itself.
func pythonResolveHome(t *testing.T, env map[string]string, osID string) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("cannot resolve the repository root: %v", err)
	}
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent.catalog import resolve_home
print(json.dumps(str(resolve_home(json.loads(sys.argv[2]), sys.argv[3]))))
`
	cmd := exec.Command(pythonBin(t), "-c", script, repoRoot, encodeEnv(t, env), osID)
	// env replaces rather than extends, so Python sees exactly what Go saw.
	environ := []string{}
	for key, value := range env {
		environ = append(environ, key+"="+value)
	}
	// Python itself needs PATH to start, and the interpreter must be able to
	// find its own standard library.
	environ = append(environ, "PATH="+os.Getenv("PATH"))
	cmd.Env = environ

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python resolve_home failed: %v\n%s", err, output)
	}
	var resolved string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(output))), &resolved); err != nil {
		t.Fatalf("cannot read python output %q: %v", output, err)
	}
	return resolved
}

func TestParityHomeResolutionMatchesPythonAcrossTheOrder(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the POSIX cases are what this compares; Windows has its own test")
	}
	cases := []struct {
		name string
		env  map[string]string
		osID string
	}{
		{"explicit override wins", map[string]string{"ONEAGENT_HOME": "/tmp/explicit", "HOME": "/home/real"}, "linux"},
		{"home when nothing overrides", map[string]string{"HOME": "/home/real"}, "linux"},
		{"macos behaves like linux", map[string]string{"HOME": "/Users/real"}, "macos"},
		{"windows profile beats home", map[string]string{"HOME": "/c/Users/real", "USERPROFILE": `C:\Users\real`}, "windows"},
		{"windows drive and path", map[string]string{"HOMEDRIVE": "D:", "HOMEPATH": `\Users\real`}, "windows"},
		{"windows falls through to home", map[string]string{"HOMEDRIVE": "D:", "HOME": "/fallback"}, "windows"},
		// The case that exposed the divergence: nothing declares a home, so
		// both sides must reach the passwd database rather than one of them
		// giving up.
		{"nothing declares a home", map[string]string{}, "linux"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			want := pythonResolveHome(t, testCase.env, testCase.osID)
			got := ResolveHome(testCase.env, testCase.osID)
			if got != want {
				t.Fatalf("Go resolved %q, Python resolved %q", got, want)
			}
		})
	}
}

func TestParityAnUnsetHomeStillResolvesRatherThanReturningEmpty(t *testing.T) {
	// Stated separately from the table because it is the specific regression:
	// os.UserHomeDir errors here, and stopping at that error would change an
	// exit code without any test recording that it had changed.
	if goruntime.GOOS == "windows" {
		t.Skip("UserHomeDir reads the profile variables on Windows")
	}
	got := ResolveHome(map[string]string{}, "linux")
	if got == "" {
		t.Fatal("no home resolved; Python reaches the passwd database here, so Go must too")
	}
	if want := pythonResolveHome(t, map[string]string{}, "linux"); got != want {
		t.Fatalf("Go resolved %q, Python resolved %q", got, want)
	}
}

func encodeEnv(t *testing.T, env map[string]string) string {
	t.Helper()
	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("cannot encode the environment: %v", err)
	}
	return string(encoded)
}
