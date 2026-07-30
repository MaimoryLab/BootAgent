package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

// This focused parity test compares the actual Python writers with the Go
// writers on the files where map ordering used to produce a byte difference.
// It is skipped when Python is unavailable so clean Go-only builds remain
// possible; CI can set ONEAGENT_REQUIRE_PARITY to make it mandatory.
func TestJSONWriterParityWithPython(t *testing.T) {
	// The first python3 on PATH is often older than the 3.12 the Python core
	// requires, so candidates are probed rather than assumed: an unusable
	// interpreter must skip this gate, not fail it with an import error.
	python := ""
	for _, name := range []string{"python3.12", "python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if runsPythonCore(path) {
			python = path
			break
		}
	}
	if python == "" {
		if os.Getenv("ONEAGENT_REQUIRE_PARITY") != "" {
			t.Fatal("Python 3.12+ is required for JSON writer parity")
		}
		t.Skip("no Python 3.12+ interpreter is available")
	}

	cases := []struct {
		name     string
		kind     string
		relative string
		existing string
	}{
		{
			name:     "claude preserves nested order",
			kind:     "claude",
			relative: filepath.Join(".claude", "settings.json"),
			existing: `{"keep":true,"env":{"CUSTOM":"value","ANTHROPIC_MODEL":"old"},"other":true}`,
		},
		{
			name:     "opencode preserves provider order",
			kind:     "opencode",
			relative: filepath.Join(".config", "opencode", "opencode.jsonc"),
			existing: `{"keep":true,"provider":{"other":{"x":1}},"theme":"dark"}`,
		},
		{
			name:     "kilo preserves provider order",
			kind:     "kilo",
			relative: filepath.Join(".config", "kilo", "kilo.jsonc"),
			existing: `{"provider":{"other":{"x":1}},"keep":true}`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			want := pythonJSONWriterOutput(t, python, testCase.kind, testCase.relative, testCase.existing)
			got := goJSONWriterOutput(t, testCase.kind, testCase.relative, testCase.existing)
			if string(got) != string(want) {
				t.Fatalf("writer output differs:\nGo:\n%s\nPython:\n%s", got, want)
			}
		})
	}
}

// runsPythonCore reports whether an interpreter is new enough to import the
// Python core this gate compares against. tomllib arrived in 3.11 and the core
// requires 3.12, so the version check is what decides; importing it here also
// confirms the interpreter is not a stub that satisfies the version test alone.
func runsPythonCore(python string) bool {
	command := exec.Command(python, "-c", "import sys, tomllib; raise SystemExit(0 if sys.version_info >= (3, 12) else 1)")
	return command.Run() == nil
}

func pythonJSONWriterOutput(t *testing.T, python, kind, relative, existing string) []byte {
	t.Helper()
	root := repoRootForParity(t)
	home := t.TempDir()
	script := `
import sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from oneagent.installer import Runtime, write_claude_config, write_openai_compatible_config

home = Path(sys.argv[2])
kind = sys.argv[3]
relative = Path(sys.argv[4])
existing = sys.argv[5]
path = home / relative
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(existing, encoding="utf-8")
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
meta = {"config_path": str(relative)}
if kind == "claude":
    write_claude_config(runtime, meta, "https://api.ppio.com/anthropic", "sk-parity", "model-new")
else:
    schema = "https://opencode.ai/config.json" if kind == "opencode" else "https://app.kilo.ai/config.json"
    agent = "opencode" if kind == "opencode" else "kilo-cli"
    write_openai_compatible_config(runtime, meta, "PPIO", "https://api.ppio.com/openai", "model-new", schema, agent)
`
	cmd := exec.Command(python, "-c", script, root, home, kind, relative, existing)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Python writer failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(home, relative))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func goJSONWriterOutput(t *testing.T, kind, relative, existing string) []byte {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	filesystem := securefs.New(securefs.Options{OS: "linux"})
	writer := NewWriter(home, "linux", filesystem)
	var err error
	switch kind {
	case "claude":
		err = writer.WriteClaude(context.Background(), path, "https://api.ppio.com/anthropic", "sk-parity", "model-new", "")
	case "opencode":
		err = writer.WriteOpenAICompatible(context.Background(), path, "https://opencode.ai/config.json", "PPIO", "https://api.ppio.com/openai", "model-new", "opencode")
	case "kilo":
		err = writer.WriteOpenAICompatible(context.Background(), path, "https://app.kilo.ai/config.json", "PPIO", "https://api.ppio.com/openai", "model-new", "kilo-cli")
	default:
		t.Fatalf("unknown parity kind %q", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func repoRootForParity(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "agents.lock.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}
