// Package skillgen renders the Skill tree for one multimodal capability bound
// to one Provider and model: SKILL.md, config.json and scripts/run.mjs.
//
// The tree never holds the API key. config.json names the environment
// variable that carries it (ADR-011); the Spec has no field a key could be put
// in, so no caller can render one by mistake.
package skillgen

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/MaimoryLab/BootAgent/internal/config"
	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

//go:embed templates
var templates embed.FS

// ConfigSchemaVersion is config.json's version; run.mjs reads it as data.
const ConfigSchemaVersion = 1

// IDPrefix marks every generated Skill ID.
const IDPrefix = "bootagent-"

// supported lists the capability and adapter pairs this version can render.
// run.mjs implements exactly these; anything else is refused instead of
// producing a Skill whose script cannot run it.
var supported = map[string][]string{
	modality.Vision:             {modality.AdapterChatVision},
	modality.ImageGeneration:    {modality.AdapterImages},
	modality.SpeechToText:       {modality.AdapterAudioTranscriptions},
	modality.TextToSpeech:       {modality.AdapterAudioSpeech, modality.AdapterNovitaAudio},
	modality.VideoGeneration:    {modality.AdapterNovitaAsync},
	modality.VideoUnderstanding: {modality.AdapterFramesVision},
}

// Supports reports whether a capability over an adapter can be generated.
func Supports(capability, adapter string) bool {
	return adapter != "" && slices.Contains(supported[capability], adapter)
}

// SupportedAdapters lists the adapters a Skill can be generated for.
func SupportedAdapters() []string {
	var result []string
	for _, capability := range modality.Capabilities {
		for _, adapter := range supported[capability] {
			if !slices.Contains(result, adapter) {
				result = append(result, adapter)
			}
		}
	}
	return result
}

// SkillID is the generated Skill's ID for a capability.
func SkillID(capability string) string {
	return IDPrefix + capability
}

// CapabilityFromSkillID reverses SkillID, empty for any other ID.
func CapabilityFromSkillID(id string) string {
	capability, ok := strings.CutPrefix(id, IDPrefix)
	if !ok {
		return ""
	}
	for _, known := range modality.Capabilities {
		if known == capability {
			return capability
		}
	}
	return ""
}

// Spec is everything a Skill is generated from. It deliberately has no field
// for the key.
type Spec struct {
	Capability   string
	Adapter      string
	ProviderID   string
	ProviderName string
	Model        string
	// BaseURL is the Provider's OpenAI-compatible base, normalized to /v1.
	BaseURL string
	// NativeBaseURL is the host root for native routes; required by a native
	// adapter, ignored otherwise.
	NativeBaseURL string
	// Options are the request fields beyond the prompt the model documents,
	// from the catalog. Only async generation uses them.
	Options []string
}

// Config is config.json. Nothing in it is secret.
type Config struct {
	SchemaVersion int    `json:"schema_version"`
	Generator     string `json:"generator"`
	Capability    string `json:"capability"`
	Adapter       string `json:"adapter"`
	ProviderID    string `json:"provider_id"`
	ProviderName  string `json:"provider_name"`
	Model         string `json:"model"`
	BaseURL       string `json:"base_url"`
	NativeBaseURL string `json:"native_base_url,omitempty"`
	// Options lists the request fields the model accepts beyond the prompt;
	// the script refuses a flag for any other.
	Options []string `json:"options,omitempty"`
	// KeyEnv names the variable holding the key; KeyFile is the private
	// fallback file read only when the variable is unset. "~" is the home
	// directory, expanded by the script so the tree is the same on every
	// machine.
	KeyEnv    string `json:"key_env"`
	KeyFile   string `json:"key_file"`
	OutputDir string `json:"output_dir"`
}

var (
	providerIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	nativeModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// NewConfig validates a Spec and derives config.json from it.
func NewConfig(spec Spec) (Config, error) {
	if !Supports(spec.Capability, spec.Adapter) {
		return Config{}, fmt.Errorf("a Skill for %s over %s cannot be generated yet", spec.Capability, spec.Adapter)
	}
	if !providerIDPattern.MatchString(spec.ProviderID) {
		return Config{}, errors.New("invalid Provider ID for a generated Skill")
	}
	model := strings.TrimSpace(spec.Model)
	if model == "" || len(model) > 256 || strings.ContainsAny(model, "\r\n\x00") {
		return Config{}, errors.New("invalid model for a generated Skill")
	}
	base, err := provider.ValidateBaseURL(strings.TrimSpace(spec.BaseURL))
	if err != nil {
		return Config{}, err
	}
	name := strings.TrimSpace(spec.ProviderName)
	if name == "" {
		name = spec.ProviderID
	}
	if strings.ContainsAny(name, "\r\n\x00") {
		return Config{}, errors.New("invalid Provider name for a generated Skill")
	}
	native := ""
	if modality.IsNative(spec.Adapter) {
		// The model ID becomes a path segment on a native route.
		if !nativeModelPattern.MatchString(model) {
			return Config{}, errors.New("invalid model for a native route")
		}
		parsed, err := url.Parse(strings.TrimSpace(spec.NativeBaseURL))
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" {
			return Config{}, errors.New("a native route needs the Provider's https host root")
		}
		native = strings.TrimRight(spec.NativeBaseURL, "/")
	}
	var options []string
	if spec.Adapter == modality.AdapterNovitaAsync {
		for _, option := range spec.Options {
			if !slices.Contains(modality.Options, option) {
				return Config{}, fmt.Errorf("unknown generation option %q", option)
			}
			if !slices.Contains(options, option) {
				options = append(options, option)
			}
		}
	}
	return Config{
		SchemaVersion: ConfigSchemaVersion,
		Generator:     "bootagent",
		Capability:    spec.Capability,
		Adapter:       spec.Adapter,
		ProviderID:    spec.ProviderID,
		ProviderName:  name,
		Model:         model,
		BaseURL:       provider.OpenAIBaseURL(base),
		NativeBaseURL: native,
		Options:       options,
		KeyEnv:        config.SkillEnvName(spec.ProviderID),
		KeyFile:       "~/.bootagent/skill-env/" + spec.ProviderID + ".env",
		OutputDir:     "bootagent-output",
	}, nil
}

type skillText struct {
	SkillID      string
	Title        string
	Description  string
	Model        string
	ProviderName string
	KeyEnv       string
	KeyFile      string
	CostRule     string
	Adapter      string
	Options      []string
}

var titles = map[string]string{
	modality.Vision:             "Image understanding",
	modality.ImageGeneration:    "Image generation",
	modality.SpeechToText:       "Speech to text",
	modality.TextToSpeech:       "Text to speech",
	modality.VideoGeneration:    "Video generation",
	modality.VideoUnderstanding: "Video understanding",
}

var descriptions = map[string]string{
	modality.Vision:             "Look at local image files (screenshots, photos, diagrams) and answer questions about them through %s on %s. Use when the user asks about an image and you cannot see images yourself.",
	modality.ImageGeneration:    "Generate images from a text description through %s on %s and save them as files. Use when the user asks you to create an image. Each run is billed: confirm with the user first.",
	modality.SpeechToText:       "Transcribe a local audio file to text through %s on %s. Use when the user asks for the text of a recording.",
	modality.TextToSpeech:       "Turn text into a spoken audio file through %s on %s. Use when the user asks for audio of some text. Each run is billed: confirm with the user first.",
	modality.VideoGeneration:    "Generate a short video from a text description through %s on %s and save it as a file. Use when the user asks you to create a video. Each run is billed by the second and cannot be cancelled: confirm with the user first.",
	modality.VideoUnderstanding: "Answer questions about a local video file by sampling frames with ffmpeg and sending them to %s on %s. Use when the user asks what happens in a video, or to find a moment in a screen recording.",
}

var costRules = map[string]string{
	modality.Vision:             "Each run is a billed request to the user's Provider. Run it when the user asks about specific images; do not run it speculatively over many files without asking.",
	modality.ImageGeneration:    "Every run costs money. Before each run, tell the user what you are about to generate and how many images, and wait for them to agree.",
	modality.SpeechToText:       "Each run is billed by the length of the audio. Run it when the user asks for a transcript; ask first before transcribing long or many recordings.",
	modality.TextToSpeech:       "Every run costs money, billed by the length of the text. Before each run, tell the user what text you are about to speak and wait for them to agree.",
	modality.VideoGeneration:    "Every run costs money, billed by the second of video, and a submitted run cannot be cancelled. Before each run, tell the user the prompt, the duration and the resolution or aspect ratio you will use, and wait for them to agree. Never retry a failed or timed-out run on your own: a timed-out run may still finish and be billed.",
	modality.VideoUnderstanding: "Each run is a billed request carrying every sampled frame as an image, so it costs about as much as that many image questions. Run it when the user asks about a specific video; ask before raising --frames or running it over many files.",
}

// Render writes the Skill tree into dir, which must exist and be empty. The
// output depends only on the Spec, so the same Spec always hashes the same.
func Render(spec Spec, dir string) error {
	cfg, err := NewConfig(spec)
	if err != nil {
		return err
	}
	files, err := Files(cfg)
	if err != nil {
		return err
	}
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// Files returns the tree's contents by slash-separated path.
func Files(cfg Config) (map[string][]byte, error) {
	if !Supports(cfg.Capability, cfg.Adapter) {
		return nil, fmt.Errorf("a Skill for %s over %s cannot be generated yet", cfg.Capability, cfg.Adapter)
	}
	text := skillText{
		SkillID:      SkillID(cfg.Capability),
		Title:        titles[cfg.Capability],
		Description:  fmt.Sprintf(descriptions[cfg.Capability], cfg.Model, cfg.ProviderName),
		Model:        cfg.Model,
		ProviderName: cfg.ProviderName,
		KeyEnv:       cfg.KeyEnv,
		KeyFile:      cfg.KeyFile,
		CostRule:     costRules[cfg.Capability],
		Adapter:      cfg.Adapter,
		Options:      cfg.Options,
	}
	parsed, err := template.New("skill").Funcs(template.FuncMap{"yamlString": yamlString, "has": slices.Contains[[]string, string]}).ParseFS(templates, "templates/common.md.tmpl", "templates/"+cfg.Capability+".md.tmpl")
	if err != nil {
		return nil, err
	}
	var skill bytes.Buffer
	if err := parsed.ExecuteTemplate(&skill, cfg.Capability+".md.tmpl", text); err != nil {
		return nil, err
	}
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	script, err := templates.ReadFile("templates/run.mjs")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		"SKILL.md":        normalizeSkill(skill.Bytes()),
		"config.json":     append(configJSON, '\n'),
		"scripts/run.mjs": script,
	}, nil
}

// ReadConfig reads config.json from a stored Skill tree. The key variable is
// re-derived from the Provider ID rather than trusted from the file, so a
// hand-edited tree cannot point BootAgent at a variable it does not own.
func ReadConfig(dir string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	if len(data) > 64<<10 {
		return Config{}, errors.New("generated Skill config is too large")
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.SchemaVersion != ConfigSchemaVersion || cfg.Generator != "bootagent" || !providerIDPattern.MatchString(cfg.ProviderID) || strings.TrimSpace(cfg.Model) == "" {
		return Config{}, errors.New("not a BootAgent generated Skill config")
	}
	cfg.KeyEnv = config.SkillEnvName(cfg.ProviderID)
	return cfg, nil
}

// yamlString renders a YAML double-quoted scalar. JSON string syntax is a
// subset of it, so encoding/json does the escaping.
func yamlString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// normalizeSkill collapses the blank lines template actions leave behind.
func normalizeSkill(data []byte) []byte {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n")
}
