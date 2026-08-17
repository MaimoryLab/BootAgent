package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

func TestJSONWritersMatchGoldenFiles(t *testing.T) {
	cases := []struct {
		kind     string
		relative string
		existing string
	}{
		{"claude", filepath.Join(".claude", "settings.json"), `{"keep":true,"env":{"CUSTOM":"value","ANTHROPIC_MODEL":"old"},"other":true}`},
		{"opencode", filepath.Join(".config", "opencode", "opencode.json"), `{"keep":true,"provider":{"other":{"x":1}},"theme":"dark"}`},
		{"kilo", filepath.Join(".config", "kilo", "kilo.jsonc"), `{"provider":{"other":{"x":1}},"keep":true}`},
		// The existing file carries the parts of an OpenClaw config BootAgent must
		// not own: a paired channel, a tools profile, and another model provider.
		// The golden file is what proves they survive.
		{"openclaw", filepath.Join(".openclaw", "openclaw.json"), `{"channels":{"discord":{"allowFrom":["user#1"]}},"models":{"providers":{"other":{"apiKey":"keep"}}},"tools":{"profile":"safe"},"agents":{"defaults":{"workspace":"~/w","model":{"fallbacks":["other/m"]}}}}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.kind, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "parity", testCase.kind+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			got := jsonWriterOutput(t, testCase.kind, testCase.relative, testCase.existing)
			if string(got) != string(want) {
				t.Fatalf("writer output differs from golden file:\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func jsonWriterOutput(t *testing.T, kind, relative, existing string) []byte {
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
		err = writer.WriteClaude(context.Background(), path, "https://api.ppio.com/anthropic", "sk-parity", "model-new")
	case "opencode":
		err = writer.WriteOpenAICompatible(context.Background(), path, "https://opencode.ai/config.json", "PPIO", "https://api.ppio.com/openai", "sk-parity", "model-new", "")
	case "kilo":
		err = writer.WriteOpenAICompatible(context.Background(), path, "https://app.kilo.ai/config.json", "PPIO", "https://api.ppio.com/openai", "sk-parity", "model-new", "")
	case "openclaw":
		err = writer.WriteOpenClaw(context.Background(), path, "PPIO", "https://api.ppio.com/openai", "sk-parity", "model-new")
	default:
		t.Fatalf("unknown writer kind %q", kind)
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
