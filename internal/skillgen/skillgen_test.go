package skillgen

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/skill"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var goldenSpecs = map[string]Spec{
	modality.Vision: {
		Capability: modality.Vision, Adapter: modality.AdapterChatVision,
		ProviderID: "novita", ProviderName: "Novita", Model: "qwen/qwen3-vl-8b", BaseURL: "https://api.novita.ai/openai",
	},
	modality.ImageGeneration: {
		Capability: modality.ImageGeneration, Adapter: modality.AdapterImages,
		ProviderID: "my-gateway", ProviderName: `Gateway "Prod": EU`, Model: "gpt-image-1", BaseURL: "https://gw.example/v1/",
	},
	modality.SpeechToText: {
		Capability: modality.SpeechToText, Adapter: modality.AdapterAudioTranscriptions,
		ProviderID: "ppio", ProviderName: "PPIO", Model: "whisper-1", BaseURL: "https://api.ppio.com/openai",
	},
	modality.TextToSpeech: {
		Capability: modality.TextToSpeech, Adapter: modality.AdapterAudioSpeech,
		ProviderID: "local", ProviderName: "", Model: "tts-1", BaseURL: "http://127.0.0.1:8000",
	},
}

func renderTo(t *testing.T, spec Spec) string {
	t.Helper()
	dir := t.TempDir()
	if err := Render(spec, dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGoldenFiles(t *testing.T) {
	for capability, spec := range goldenSpecs {
		t.Run(capability, func(t *testing.T) {
			dir := renderTo(t, spec)
			for _, name := range []string{"SKILL.md", "config.json"} {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				golden := filepath.Join("testdata", "golden", capability, name)
				if *update {
					if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, got, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%v (run with -update to create it)", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s differs from %s; run with -update after checking the change\n%s", name, golden, got)
				}
			}
			script, _ := os.ReadFile(filepath.Join(dir, "scripts", "run.mjs"))
			source, _ := os.ReadFile(filepath.Join("templates", "run.mjs"))
			if !bytes.Equal(script, source) {
				t.Error("scripts/run.mjs is not the embedded script")
			}
		})
	}
}

// The design's promise: no key in any generated file. The Spec has no key
// field, so this guards against a future template interpolating something
// key-shaped from elsewhere.
var keyShaped = regexp.MustCompile(`sk-[A-Za-z0-9]|"api_key"|Bearer [A-Za-z0-9]`)

func TestGeneratedTreesCarryNoKey(t *testing.T) {
	for capability, spec := range goldenSpecs {
		dir := renderTo(t, spec)
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if match := keyShaped.Find(data); match != nil && filepath.Base(path) != "run.mjs" {
				t.Errorf("%s/%s contains %q", capability, filepath.Base(path), match)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderIsDeterministicAndReadableAsASkill(t *testing.T) {
	spec := goldenSpecs[modality.ImageGeneration]
	first, err := skill.HashTree(context.Background(), renderTo(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	dir := renderTo(t, spec)
	second, err := skill.HashTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same Spec, different trees: %v vs %v", first, second)
	}
	// A provider name with quotes and a colon must not break the frontmatter.
	name, description, diagnostic := skill.ReadMetadata(context.Background(), dir, "fallback")
	if diagnostic != "" || name != "bootagent-image-generation" || !strings.Contains(description, `Gateway "Prod": EU`) {
		t.Fatalf("metadata = %q, %q, %q", name, description, diagnostic)
	}
	changed := spec
	changed.Model = "gpt-image-2"
	third, _ := skill.HashTree(context.Background(), renderTo(t, changed))
	if third.Hash == first.Hash {
		t.Fatal("a different model produced the same tree")
	}
}

func TestConfigNormalizesAndReadsBack(t *testing.T) {
	dir := renderTo(t, goldenSpecs[modality.ImageGeneration])
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://gw.example/v1" || cfg.KeyEnv != "BOOTAGENT_MY_GATEWAY_API_KEY" || cfg.KeyFile != "~/.bootagent/skill-env/my-gateway.env" {
		t.Fatalf("config = %#v", cfg)
	}
	// A hand-edited key_env is not trusted: the name is re-derived.
	path := filepath.Join(dir, "config.json")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, bytes.Replace(data, []byte("BOOTAGENT_MY_GATEWAY_API_KEY"), []byte("PATH"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = ReadConfig(dir)
	if err != nil || cfg.KeyEnv != "BOOTAGENT_MY_GATEWAY_API_KEY" {
		t.Fatalf("tampered config = %#v, %v", cfg, err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"generator":"someone","provider_id":"x","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConfig(dir); err == nil {
		t.Fatal("a foreign config was accepted")
	}
}

func TestNewConfigRefusesWhatItCannotGenerate(t *testing.T) {
	base := goldenSpecs[modality.Vision]
	cases := map[string]func(*Spec){
		"chat audio input": func(s *Spec) { s.Capability, s.Adapter = modality.SpeechToText, modality.AdapterChatAudioInput },
		"chat image output": func(s *Spec) {
			s.Capability, s.Adapter = modality.ImageGeneration, modality.AdapterChatImageOutput
		},
		"mismatched adapter": func(s *Spec) { s.Adapter = modality.AdapterImages },
		"video":              func(s *Spec) { s.Capability, s.Adapter = modality.VideoUnderstanding, modality.AdapterChatVideo },
		"bad provider":       func(s *Spec) { s.ProviderID = "Bad_ID" },
		"model with newline": func(s *Spec) { s.Model = "m\nrm -rf" },
		"empty model":        func(s *Spec) { s.Model = " " },
		"bad base":           func(s *Spec) { s.BaseURL = "ftp://x" },
		"base with userinfo": func(s *Spec) { s.BaseURL = "https://user:pass@x.test" },
	}
	for name, mutate := range cases {
		spec := base
		mutate(&spec)
		if _, err := NewConfig(spec); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSkillIDRoundTrip(t *testing.T) {
	for _, capability := range modality.Capabilities {
		id := SkillID(capability)
		if err := skill.ValidateID(id); err != nil {
			t.Fatalf("%s is not a valid Skill ID: %v", id, err)
		}
		if CapabilityFromSkillID(id) != capability {
			t.Fatalf("round trip of %s", id)
		}
	}
	for _, id := range []string{"review", "bootagent-", "bootagent-smell"} {
		if CapabilityFromSkillID(id) != "" {
			t.Fatalf("%s read as generated", id)
		}
	}
}
