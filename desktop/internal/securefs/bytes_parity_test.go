package securefs

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Porting the tests proves the cases someone thought to write down. This proves
// the rest: the two implementations are handed the same inputs and their output
// files are compared byte for byte.
//
// It is the only evidence that "equivalent" means equivalent rather than
// approximately the same, which is why the migration plan puts the stop-loss
// checkpoint at the end of this phase.

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

// pythonAtomicWrite runs the real atomic_write into its own temporary directory
// and returns the resulting bytes. Each side writes into a separate directory --
// never a shared HOME -- so a difference cannot be caused by one run observing
// the other's leftovers.
func pythonAtomicWrite(t *testing.T, relative, content string, secret bool) []byte {
	t.Helper()
	root := repoRoot(t)
	home := t.TempDir()
	script := `
import json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from oneagent.installer import Runtime, atomic_write

home = Path(sys.argv[2])
target = home / sys.argv[3]
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
atomic_write(runtime, target, sys.argv[4], secret=json.loads(sys.argv[5]))
print(json.dumps({"mode": oct(target.stat().st_mode & 0o777)}))
`
	secretArg := "false"
	if secret {
		secretArg = "true"
	}
	cmd := exec.Command(pythonBin(t), "-c", script, root, home, relative, content, secretArg)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("python atomic_write failed: %v\n%s", err, output)
	}
	raw, err := os.ReadFile(filepath.Join(home, relative))
	if err != nil {
		t.Fatalf("python wrote no file at %s: %v", relative, err)
	}
	return raw
}

func goAtomicWrite(t *testing.T, relative, content string, secret bool) []byte {
	t.Helper()
	home := t.TempDir()
	fs := newFS(t, home)
	target := filepath.Join(home, relative)
	if _, err := fs.Write(target, content, secret); err != nil {
		t.Fatalf("go write failed: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("go wrote no file: %v", err)
	}
	return raw
}

// contentShapes are the payloads a config write actually carries, plus the ones
// where an encoding difference would be invisible in a normal test: non-ASCII,
// characters Go's JSON encoder escapes by default, and a file with no trailing
// newline.
var contentShapes = map[string]string{
	"empty":                  "",
	"single line no newline": `model = "gpt-5"`,
	"trailing newline":       "model = \"gpt-5\"\n",
	"crlf":                   "a = 1\r\nb = 2\r\n",
	"non-ascii":              "model = \"通义-max\"\nnote = \"中文说明\"\n",
	"emoji":                  "note = \"✅ done\"\n",
	// & < > are escaped by Go's json encoder unless told otherwise; a base URL
	// with a query string is the realistic way one reaches a config file.
	"html-ish characters": "{\"baseUrl\": \"https://x.test/?a=1&b=2<c>\"}\n",
	"tabs and spaces":     "a\t=\t1\n  b = 2\n",
	"lone cr":             "a = 1\rb = 2\n",
	"nul-free control":    "a = \"\\u0001\"\n",
	"long line":           strings.Repeat("x", 8192) + "\n",
	"many lines":          strings.Repeat("line\n", 500),
	"json indented":       "{\n  \"env\": {\n    \"ANTHROPIC_MODEL\": \"通义\"\n  }\n}\n",
}

func TestParityAtomicWriteProducesIdenticalBytes(t *testing.T) {
	for name, content := range contentShapes {
		t.Run(name, func(t *testing.T) {
			want := pythonAtomicWrite(t, filepath.Join(".codex", "config.toml"), content, false)
			got := goAtomicWrite(t, filepath.Join(".codex", "config.toml"), content, false)
			if string(got) != string(want) {
				t.Fatalf("bytes differ for %s:\n  Go     (%d): %q\n  Python (%d): %q",
					name, len(got), truncate(got), len(want), truncate(want))
			}
		})
	}
}

func TestParityTheSecretPathWritesTheSameBytesToo(t *testing.T) {
	// The secret flag changes the backup handling, not the content. Asserted so
	// a future change to one path cannot quietly alter what lands on disk.
	content := "export OPENAI_API_KEY='sk-parity'\nexport OPENAI_API_BASE='https://x.test/v1'\n"
	want := pythonAtomicWrite(t, filepath.Join(".oneagent", "aider.env"), content, true)
	got := goAtomicWrite(t, filepath.Join(".oneagent", "aider.env"), content, true)
	if string(got) != string(want) {
		t.Fatalf("bytes differ:\n  Go:     %q\n  Python: %q", got, want)
	}
}

func TestParityModesMatchPython(t *testing.T) {
	// A file the other implementation would have written at 0600 must not land
	// at 0644: the whole point of this package is that a credential file is
	// unreadable to anyone else.
	root := repoRoot(t)
	home := t.TempDir()
	script := `
import json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from oneagent.installer import Runtime, atomic_write

home = Path(sys.argv[2])
target = home / "nested" / "config.json"
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
atomic_write(runtime, target, "{}\n", secret=True)
print(json.dumps({
    "file": oct(target.stat().st_mode & 0o777),
    "dir": oct(target.parent.stat().st_mode & 0o777),
}))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root, home)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed: %v", err)
	}
	var pythonModes map[string]string
	if err := json.Unmarshal(output, &pythonModes); err != nil {
		t.Fatalf("cannot read python output %q: %v", output, err)
	}

	goHome := t.TempDir()
	fs := newFS(t, goHome)
	target := filepath.Join(goHome, "nested", "config.json")
	if _, err := fs.Write(target, "{}\n", true); err != nil {
		t.Fatalf("go write failed: %v", err)
	}
	fileInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("cannot stat: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(target))
	if err != nil {
		t.Fatalf("cannot stat parent: %v", err)
	}

	if got, want := "0o"+describeMode(fileInfo.Mode())[1:], pythonModes["file"]; got != want {
		t.Errorf("file mode: Go=%s Python=%s", got, want)
	}
	if got, want := "0o"+describeMode(dirInfo.Mode())[1:], pythonModes["dir"]; got != want {
		t.Errorf("directory mode: Go=%s Python=%s", got, want)
	}
}

func TestParityBackupNamingMatchesPython(t *testing.T) {
	// The backup name is user-visible: someone recovering a config looks for
	// this pattern, and a tool that cleans up old backups matches on it.
	root := repoRoot(t)
	home := t.TempDir()
	script := `
import json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from oneagent.installer import Runtime, atomic_write

home = Path(sys.argv[2])
target = home / "config.json"
target.parent.mkdir(parents=True, exist_ok=True)
target.write_text("original\n", encoding="utf-8")
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
backup = atomic_write(runtime, target, "replacement\n", secret=False)
print(json.dumps({"backup": backup.name if backup else None}))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root, home)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed: %v", err)
	}
	var result map[string]*string
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	pythonName := result["backup"]
	if pythonName == nil {
		t.Fatal("python reported no backup for an existing file")
	}

	goHome := t.TempDir()
	target := filepath.Join(goHome, "config.json")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	fs := newFS(t, goHome)
	backup, err := fs.Write(target, "replacement\n", false)
	if err != nil {
		t.Fatalf("go write failed: %v", err)
	}
	goName := filepath.Base(backup)

	// The timestamps will differ by the second the two ran in, so the shape is
	// what is compared: prefix, separator and the length of the stamp.
	pythonPrefix, pythonStamp := splitBackupName(t, *pythonName)
	goPrefix, goStamp := splitBackupName(t, goName)
	if goPrefix != pythonPrefix {
		t.Errorf("backup prefix: Go=%q Python=%q", goPrefix, pythonPrefix)
	}
	if len(goStamp) != len(pythonStamp) {
		t.Errorf("timestamp shape: Go=%q (%d) Python=%q (%d)", goStamp, len(goStamp), pythonStamp, len(pythonStamp))
	}
	for _, char := range goStamp {
		if char < '0' || char > '9' {
			t.Errorf("Go timestamp %q is not all digits", goStamp)
			break
		}
	}
}

func splitBackupName(t *testing.T, name string) (prefix, stamp string) {
	t.Helper()
	const marker = ".backup-"
	index := strings.LastIndex(name, marker)
	if index < 0 {
		t.Fatalf("%q does not contain %q", name, marker)
	}
	return name[:index+len(marker)], name[index+len(marker):]
}

func truncate(raw []byte) string {
	if len(raw) <= 200 {
		return string(raw)
	}
	return string(raw[:200]) + "...(truncated)"
}
