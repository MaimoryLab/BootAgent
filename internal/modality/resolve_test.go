package modality

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// snapshots are the public /models listings fetched on 2026-10-09, trimmed to
// the fields detection reads.
var snapshots = []string{"novita", "ppio", "jiekou"}

func loadSnapshot(t *testing.T, name string) []provider.ModelEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+"-models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Data []provider.ModelEntry `json:"data"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Data) == 0 {
		t.Fatalf("snapshot %s is empty", name)
	}
	return file.Data
}

func embeddedRules(t *testing.T) Rules {
	t.Helper()
	rules, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func nonText(values []string) bool {
	for _, value := range values {
		if value != "text" {
			return true
		}
	}
	return false
}

// The ID rules exist for Providers that declare nothing. Wherever a gateway
// does declare, a rule claiming more than the declaration is a rule that would
// mislead on an undeclaring Provider too.
func TestIDRulesNeverContradictDeclaredMetadata(t *testing.T) {
	rules := embeddedRules(t)
	for _, name := range snapshots {
		for _, entry := range loadSnapshot(t, name) {
			if !entry.Declared() {
				continue
			}
			declared := DeclaredCapabilities(entry)
			for _, claimed := range rules.MatchIDRules(entry.ID) {
				if !slices.Contains(declared, claimed) {
					t.Errorf("%s: rule claims %s for %s, which declares %v", name, claimed, entry.ID, declared)
				}
			}
		}
	}
}

func TestEveryIDRuleMatchesTheSnapshots(t *testing.T) {
	rules := embeddedRules(t)
	hits := make([]int, len(rules.rules))
	multimodal, covered := 0, 0
	for _, name := range snapshots {
		for _, entry := range loadSnapshot(t, name) {
			key := RuleKey(entry.ID)
			for index, rule := range rules.rules {
				if rule.pattern.MatchString(key) && (rule.exclude == nil || !rule.exclude.MatchString(key)) {
					hits[index]++
				}
			}
			if nonText(entry.InputModalities) || nonText(entry.OutputModalities) {
				multimodal++
				if len(rules.MatchIDRules(entry.ID)) > 0 {
					covered++
				}
			}
		}
	}
	for index, count := range hits {
		if count == 0 {
			t.Errorf("ID rule %d (%s) matches no snapshot model", index, rules.rules[index].pattern)
		}
	}
	// The design records 194 of 207; a drop means a rule was narrowed too far.
	if multimodal != 207 || covered != 194 {
		t.Errorf("rules cover %d of %d declared multimodal entries, want 194 of 207", covered, multimodal)
	}
}

func TestResolveReplaysSnapshotsSafely(t *testing.T) {
	rules := embeddedRules(t)
	for _, name := range snapshots {
		entries := loadSnapshot(t, name)
		models := rules.Resolve(name, entries, nil)
		if len(models) == 0 {
			t.Fatalf("%s: nothing detected", name)
		}
		for _, model := range models {
			for _, capability := range model.Capabilities {
				if capability.Source == SourceIDRule && capability.Installable {
					t.Errorf("%s: %s %s installable on an ID rule alone", name, model.ID, capability.ID)
				}
				if probeOnly[capability.Adapter] && capability.Installable {
					t.Errorf("%s: %s %s installable on unverified adapter %s without a probe", name, model.ID, capability.ID, capability.Adapter)
				}
				// Speech and video generation have no chat route; only the
				// catalog, which names a native route, may offer them.
				if (capability.ID == TextToSpeech || capability.ID == VideoGeneration) && capability.Source != SourceCatalog {
					t.Errorf("%s: %s offered %s from a chat listing", name, model.ID, capability.ID)
				}
			}
		}
	}
}

func TestResolveOrder(t *testing.T) {
	rules := embeddedRules(t)
	entries := []provider.ModelEntry{
		{ID: "moonshotai/kimi-k3"},
		{ID: "qwen/qwen3-vl-8b", InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}},
		// Declared text-only: the Provider's word beats the ID rule.
		{ID: "claude-sonnet-x", InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
		{ID: "plain-text-model"},
		{ID: "gemini-3-pro-image", InputModalities: []string{"text", "image"}, OutputModalities: []string{"text", "image"}},
	}
	verdicts := map[string]map[string]provider.ModalityVerdict{
		"moonshotai/kimi-k3": {Vision: {Supported: true, Source: SourceProbe, Adapter: AdapterChatVision}},
		"qwen/qwen3-vl-8b":   {Vision: {Supported: false, Source: SourceProbe, Adapter: AdapterChatVision}},
	}
	models := rules.Resolve("custom", entries, verdicts)
	byID := map[string]Model{}
	for _, model := range models {
		byID[model.ID] = model
	}
	if _, ok := byID["claude-sonnet-x"]; ok {
		t.Error("an ID rule overrode a text-only declaration")
	}
	if _, ok := byID["plain-text-model"]; ok {
		t.Error("a model nothing claims was offered")
	}
	kimi := byID["moonshotai/kimi-k3"].Capabilities
	if len(kimi) != 2 || kimi[0] != (Capability{ID: Vision, Source: SourceProbe, Adapter: AdapterChatVision, Supported: true, Installable: true, Probe: ProbeMinimal}) {
		t.Fatalf("kimi vision = %#v", kimi)
	}
	if kimi[1].ID != VideoUnderstanding || kimi[1].Source != SourceIDRule || kimi[1].Installable {
		t.Fatalf("kimi video = %#v", kimi[1])
	}
	qwen := byID["qwen/qwen3-vl-8b"].Capabilities
	if len(qwen) != 1 || qwen[0].Supported || qwen[0].Installable || qwen[0].Source != SourceProbe {
		t.Fatalf("refused probe = %#v", qwen)
	}
	gemini := byID["gemini-3-pro-image"].Capabilities
	if len(gemini) != 2 || !gemini[0].Installable || gemini[1].ID != ImageGeneration || gemini[1].Installable || gemini[1].Adapter != AdapterChatImageOutput {
		t.Fatalf("declared image output = %#v", gemini)
	}
}

func TestResolveAddsCatalogGenerationModels(t *testing.T) {
	manifest := catalog.ModalityManifest{
		SchemaVersion: 1,
		IDRules:       []catalog.ModalityIDRule{{Pattern: "^claude-", Capabilities: []string{Vision}}},
		Providers: map[string]catalog.ModalityProvider{
			"novita": {NativeBaseURL: "https://api.novita.ai", Generation: []catalog.ModalityGenerationModel{
				{ID: "kling-v3.0-4k-t2v", Capabilities: []string{VideoGeneration}, Adapter: AdapterNovitaAsync},
			}},
		},
	}
	rules, err := NewRules(manifest)
	if err != nil {
		t.Fatal(err)
	}
	models := rules.Resolve("novita", nil, nil)
	if len(models) != 1 || models[0].Capabilities[0] != (Capability{ID: VideoGeneration, Source: SourceCatalog, Adapter: AdapterNovitaAsync, Supported: true, Installable: true}) {
		t.Fatalf("catalog models = %#v", models)
	}
	if got := rules.Resolve("ppio", nil, nil); len(got) != 0 {
		t.Fatalf("another Provider got novita's catalog: %#v", got)
	}
	if rules.NativeBaseURL("novita") != "https://api.novita.ai" || rules.NativeBaseURL("deepseek") != "" {
		t.Fatal("native base lookup")
	}
}

func TestNewRulesRejectsWordsOutsideTheVocabulary(t *testing.T) {
	base := func() catalog.ModalityManifest {
		return catalog.ModalityManifest{SchemaVersion: 1, IDRules: []catalog.ModalityIDRule{{Pattern: "^x", Capabilities: []string{Vision}}}}
	}
	cases := map[string]func(*catalog.ModalityManifest){
		"unknown capability": func(m *catalog.ModalityManifest) { m.IDRules[0].Capabilities = []string{"smell"} },
		// No chat route speaks text-to-speech, so a rule claiming it could
		// never be honoured.
		"rule without chat route": func(m *catalog.ModalityManifest) { m.IDRules[0].Capabilities = []string{TextToSpeech} },
		"unknown adapter": func(m *catalog.ModalityManifest) {
			m.Providers = map[string]catalog.ModalityProvider{"novita": {NativeBaseURL: "https://api.novita.ai", Generation: []catalog.ModalityGenerationModel{{ID: "m", Capabilities: []string{VideoGeneration}, Adapter: "fax"}}}}
		},
		"unknown generation capability": func(m *catalog.ModalityManifest) {
			m.Providers = map[string]catalog.ModalityProvider{"novita": {NativeBaseURL: "https://api.novita.ai", Generation: []catalog.ModalityGenerationModel{{ID: "m", Capabilities: []string{"smell"}, Adapter: AdapterNovitaAsync}}}}
		},
	}
	for name, mutate := range cases {
		manifest := base()
		mutate(&manifest)
		if _, err := NewRules(manifest); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestRuleKeyStripsVendorAndCase(t *testing.T) {
	for input, want := range map[string]string{
		"moonshotai/kimi-k3": "kimi-k3",
		"Claude-Opus-5-5":    "claude-opus-5-5",
		" a/b/qwen3-vl-8b ":  "qwen3-vl-8b",
	} {
		if got := RuleKey(input); got != want {
			t.Errorf("RuleKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDeclaredCapabilitiesIgnoreChatAudioAndVideoOutput(t *testing.T) {
	got := DeclaredCapabilities(provider.ModelEntry{
		ID:               "gpt-image-2.5-flare",
		InputModalities:  []string{"text", "image", "video", "audio"},
		OutputModalities: []string{"text", "image", "video", "audio"},
	})
	want := []string{Vision, ImageGeneration, SpeechToText, VideoUnderstanding}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("declared = %v, want %v", got, want)
	}
}

// A model the user typed by hand and probed is in no listing; the verdict is
// all there is, and a pass is enough to install it.
func TestResolveKeepsHandEnteredProbedModels(t *testing.T) {
	rules := embeddedRules(t)
	verdicts := map[string]map[string]provider.ModalityVerdict{
		"whisper-1": {SpeechToText: {Supported: true, Source: SourceProbe, Adapter: AdapterAudioTranscriptions}},
		"broken":    {TextToSpeech: {Supported: false, Source: SourceProbe, Adapter: AdapterAudioSpeech}},
	}
	models := rules.Resolve("custom", nil, verdicts)
	whisper, ok := Find(models, "whisper-1", SpeechToText)
	if !ok || !whisper.Installable || whisper.Adapter != AdapterAudioTranscriptions || whisper.Probe != ProbeMinimal {
		t.Fatalf("whisper = %#v, %v", whisper, ok)
	}
	broken, ok := Find(models, "broken", TextToSpeech)
	if !ok || broken.Installable || broken.Supported {
		t.Fatalf("broken = %#v", broken)
	}
	if _, ok := Find(models, "whisper-1", Vision); ok {
		t.Fatal("Find invented a capability")
	}
}

func TestAdapterVocabulary(t *testing.T) {
	for capability, adapter := range manualAdapter {
		if !Serves(adapter, capability) || ProbeKind(adapter) == ProbeNone {
			t.Errorf("manual route %s for %s cannot be probed", adapter, capability)
		}
	}
	if Serves(AdapterAudioSpeech, Vision) || Serves("fax", Vision) {
		t.Fatal("Serves accepted a mismatch")
	}
	// The native routes serve what their catalog entry says.
	if !Serves(AdapterNovitaAsync, VideoGeneration) || !Serves(AdapterNovitaAsync, ImageGeneration) {
		t.Fatal("native async route refused a catalog capability")
	}
	if ProbeKind(AdapterImages) != ProbeConfirm {
		t.Fatal("the image probe must ask first: it produces a billed image")
	}
	for _, adapter := range Adapters {
		if _, ok := adapterCapability[adapter]; !ok {
			t.Errorf("adapter %s has no capability", adapter)
		}
	}
	if ManualAdapter(VideoGeneration) != "" || ManualAdapter(VideoUnderstanding) != "" {
		t.Fatal("a capability with no OpenAI-compatible route offered manual entry")
	}
}

// A catalog entry names a verified route. For a listed model it outranks the
// declaration, which only says the model outputs images, not how a gateway
// returns them.
func TestCatalogOutranksDeclarationForListedModels(t *testing.T) {
	rules := embeddedRules(t)
	entries := loadSnapshot(t, "novita")
	models := rules.Resolve("novita", entries, nil)
	ming, ok := Find(models, "ming-image-0.1-design", ImageGeneration)
	if !ok || ming.Source != SourceCatalog || ming.Adapter != AdapterImages || !ming.Installable || !ming.Recommended {
		t.Fatalf("ming image = %#v, %v", ming, ok)
	}
	// Its other declared capabilities still come from the declaration.
	if vision, ok := Find(models, "ming-image-0.1-design", Vision); !ok || vision.Source != SourceDeclared {
		t.Fatalf("ming vision = %#v, %v", vision, ok)
	}
	// The layer model shares the name prefix but is edit-only: the catalog
	// does not list it, so it stays on the unverified chat route.
	if layer, ok := Find(models, "ming-image-0.1-design-layer", ImageGeneration); ok && layer.Installable {
		t.Fatalf("edit-only model installable: %#v", layer)
	}
	// A probe verdict still outranks the catalog.
	probed := rules.Resolve("novita", entries, map[string]map[string]provider.ModalityVerdict{
		"ming-image-0.1-design": {ImageGeneration: {Supported: false, Source: SourceProbe, Adapter: AdapterImages}},
	})
	if found, _ := Find(probed, "ming-image-0.1-design", ImageGeneration); found.Source != SourceProbe || found.Installable {
		t.Fatalf("probe did not outrank catalog: %#v", found)
	}
}

func TestEmbeddedCatalogOffersVerifiedNativeRoutes(t *testing.T) {
	rules := embeddedRules(t)
	for _, providerID := range []string{"novita", "ppio", "jiekou"} {
		models := rules.Resolve(providerID, nil, nil)
		speech, ok := Find(models, "minimax-speech-2.8-turbo", TextToSpeech)
		if !ok || speech.Adapter != AdapterNovitaAudio || !speech.Installable || speech.Probe != ProbeMinimal {
			t.Errorf("%s speech = %#v, %v", providerID, speech, ok)
		}
		if rules.NativeBaseURL(providerID) == "" {
			t.Errorf("%s has no native base", providerID)
		}
	}
	video, ok := Find(rules.Resolve("jiekou", nil, nil), "wan2.7-t2v", VideoGeneration)
	if !ok || video.Adapter != AdapterNovitaAsync || !video.Installable || video.Probe != ProbeNone {
		t.Fatalf("jiekou video = %#v, %v", video, ok)
	}
	generation, _ := rules.Generation("jiekou", "kling-v3.0-4k-t2v")
	if strings.Join(generation.Options, ",") != "duration,aspect_ratio,negative_prompt,sound" {
		t.Fatalf("kling options = %v", generation.Options)
	}
	for _, providerID := range []string{"novita", "ppio"} {
		if !slices.Contains(rules.Unsupported(providerID), VideoGeneration) {
			t.Errorf("%s video generation should be marked unsupported", providerID)
		}
	}
}

func TestNewRulesRejectsBrokenGenerationEntries(t *testing.T) {
	with := func(provider catalog.ModalityProvider) catalog.ModalityManifest {
		return catalog.ModalityManifest{SchemaVersion: 1, IDRules: []catalog.ModalityIDRule{{Pattern: "^x", Capabilities: []string{Vision}}}, Providers: map[string]catalog.ModalityProvider{"novita": provider}}
	}
	cases := map[string]catalog.ModalityProvider{
		"native without base": {Generation: []catalog.ModalityGenerationModel{{ID: "m", Capabilities: []string{TextToSpeech}, Adapter: AdapterNovitaAudio}}},
		"reserved adapter":    {NativeBaseURL: "https://api.novita.ai", Generation: []catalog.ModalityGenerationModel{{ID: "m", Capabilities: []string{VideoGeneration}, Adapter: AdapterPPIOVideo}}},
		"adapter mismatch":    {NativeBaseURL: "https://api.novita.ai", Generation: []catalog.ModalityGenerationModel{{ID: "m", Capabilities: []string{Vision}, Adapter: AdapterNovitaAudio}}},
		"unknown option":      {NativeBaseURL: "https://api.novita.ai", Generation: []catalog.ModalityGenerationModel{{ID: "m", Capabilities: []string{VideoGeneration}, Adapter: AdapterNovitaAsync, Options: []string{"fps"}}}},
		"unknown unsupported": {Unsupported: []string{"smell"}},
	}
	for name, entry := range cases {
		if _, err := NewRules(with(entry)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
