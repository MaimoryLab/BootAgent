package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hermesConfig mirrors the shape of a real ~/.hermes/config.yaml: a documented
// model block, a custom_providers list, and unrelated sections the user owns.
// The comments are the point of most tests in this file.
const hermesConfig = `# Hermes configuration. Edit with care.

model:
  # Which provider entry below to resolve the model through.
  provider: nous
  default: nousresearch/hermes-4-70b
  base_url: https://inference-api.nousresearch.com/v1

custom_providers:
- name: nous
  base_url: https://inference-api.nousresearch.com/v1
  api_key: nous-key
  # A field OneAgent does not model.
  request_timeout_seconds: 90

# Everything below belongs to the user and Hermes, not to OneAgent.
skills:
  auto_improve: true
mcp_servers: {}
display:
  theme: dark
`

func writeHermesFixture(t *testing.T, home, content string) string {
	t.Helper()
	path := filepath.Join(home, ".hermes", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func countCommentLines(text string) int {
	count := 0
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			count++
		}
	}
	return count
}

// The reason OneAgent writes this file itself instead of shelling out to
// `hermes config set`: that command strips every comment in the file, and a real
// config.yaml is overwhelmingly comments -- one observed file was 1246 lines of
// which 1044 were documentation.
func TestWriteHermesKeepsCommentsAndUnmanagedSections(t *testing.T) {
	home := t.TempDir()
	path := writeHermesFixture(t, home, hermesConfig)
	writer := testWriter(t, home, "linux")
	if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result := string(data)

	if before, after := countCommentLines(hermesConfig), countCommentLines(result); after != before {
		t.Fatalf("comment lines = %d, want %d:\n%s", after, before, result)
	}
	for _, comment := range []string{
		"# Hermes configuration. Edit with care.",
		"# Which provider entry below to resolve the model through.",
		"# A field OneAgent does not model.",
		"# Everything below belongs to the user and Hermes, not to OneAgent.",
	} {
		if !strings.Contains(result, comment) {
			t.Errorf("lost comment %q from:\n%s", comment, result)
		}
	}
	// Sections OneAgent has no business rewriting must survive untouched.
	for _, fragment := range []string{"auto_improve: true", "mcp_servers: {}", "theme: dark"} {
		if !strings.Contains(result, fragment) {
			t.Errorf("lost unmanaged setting %q from:\n%s", fragment, result)
		}
	}
	// A key inside the provider entry OneAgent rewrites, which forward-compat
	// requires it to carry over rather than drop.
	if !strings.Contains(result, "request_timeout_seconds: 90") {
		t.Errorf("dropped an unmodelled field inside the rewritten entry:\n%s", result)
	}
	// Two-space indentation, not the encoder's default of four: reformatting every
	// nested line would bury the actual change in a whole-file diff.
	if !strings.Contains(result, "\n  provider: ppio") {
		t.Errorf("indentation changed:\n%s", result)
	}
}

func TestWriteHermesPointsTheModelAtTheProviderItWrites(t *testing.T) {
	home := t.TempDir()
	path := writeHermesFixture(t, home, hermesConfig)
	writer := testWriter(t, home, "linux")
	if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Hermes resolves the model through the named provider, so a write that set
	// one without the other would leave a config that cannot run.
	detected := ReadHermesConfig(string(data))
	if detected.Unreadable != nil {
		t.Fatalf("wrote a config its own reader rejects: %s", *detected.Unreadable)
	}
	// Normalised to /v1, matching every other OpenAI-compatible adapter: Hermes
	// speaks chat_completions against this base.
	if detected.Model != "model-a" || detected.BaseURL != "https://api.ppio.com/openai/v1" {
		t.Fatalf("round-trip = %#v", detected)
	}
	if !detected.ManagedByOneAgent {
		t.Fatal("a config OneAgent just wrote did not read back as managed")
	}
	// The pre-existing provider is a different vendor's entry and must remain.
	if !strings.Contains(string(data), "name: nous") {
		t.Fatalf("removed an unrelated provider:\n%s", data)
	}
}

// The provider entry is matched by name, so a second write must update it in
// place. Appending would leave duplicate entries and Hermes would resolve
// whichever it saw first.
func TestWriteHermesUpdatesAnExistingProviderEntryInPlace(t *testing.T) {
	home := t.TempDir()
	path := writeHermesFixture(t, home, hermesConfig)
	writer := testWriter(t, home, "linux")
	for _, model := range []string{"model-a", "model-b"} {
		if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", model); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result := string(data)
	if got := strings.Count(result, "name: ppio"); got != 1 {
		t.Fatalf("ppio provider appears %d times, want 1:\n%s", got, result)
	}
	if detected := ReadHermesConfig(result); detected.Model != "model-b" {
		t.Fatalf("second write did not take: %#v", detected)
	}
}

// Writing twice with the same input must produce the same bytes, or every status
// refresh would show a spurious diff and backups would pile up.
func TestWriteHermesIsIdempotent(t *testing.T) {
	home := t.TempDir()
	path := writeHermesFixture(t, home, hermesConfig)
	writer := testWriter(t, home, "linux")
	if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("second identical write changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// Found by running the writer against a real 1246-line config, which the
// synthetic fixture above did not catch. yaml.v3 re-emits a comment trailing a
// nested mapping one indent level deeper, so repeated writes would walk
// commented-out settings rightward a little at a time. Skipping an unchanged
// write is what bounds it, so this asserts both the no-op and the stability.
func TestWriteHermesDoesNotWalkTrailingCommentsRightward(t *testing.T) {
	home := t.TempDir()
	// The shape that triggers it: a commented-out sibling after a nested mapping.
	path := writeHermesFixture(t, home, `model:
  provider: ppio
  default: model-a
  base_url: https://api.ppio.com/openai/v1
custom_providers:
- name: ppio
  base_url: https://api.ppio.com/openai/v1
  api_key: sk-hermes-secret
stt:
  enabled: true
  openai:
    model: "whisper-1"
  # mistral:
  #   model: "voxtral-mini-latest"
`)
	writer := testWriter(t, home, "linux")
	// The first write may normalise the encoder's own formatting once. What must
	// not happen is a further shift on every write after that.
	if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-b"); err != nil {
		t.Fatal(err)
	}
	settled, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for pass := range 4 {
		if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-b"); err != nil {
			t.Fatal(err)
		}
		next, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(next) != string(settled) {
			t.Fatalf("write %d changed a file it had nothing to change:\nwas:\n%s\nnow:\n%s", pass+2, settled, next)
		}
	}
	// The commented-out block must still be a comment at its own level, not
	// pushed under the mapping above it.
	if !strings.Contains(string(settled), "\n  # mistral:") {
		t.Fatalf("trailing comment moved:\n%s", settled)
	}
	// And the values still round-trip after all that.
	if detected := ReadHermesConfig(string(settled)); detected.Model != "model-b" || !detected.ManagedByOneAgent {
		t.Fatalf("round-trip after repeated writes = %#v", detected)
	}
}

// The file must be left completely alone when the Profile it already holds is
// re-applied, which happens on every Provider save. A rewrite would take a
// backup and churn the mtime for nothing.
func TestWriteHermesSkipsAWriteThatWouldChangeNothing(t *testing.T) {
	home := t.TempDir()
	path := writeHermesFixture(t, home, hermesConfig)
	writer := testWriter(t, home, "linux")
	apply := func() {
		if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-a"); err != nil {
			t.Fatal(err)
		}
	}
	apply()
	settled, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	apply()
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(settled) {
		t.Fatalf("re-applying the same Profile rewrote the file:\n%s", again)
	}
	nextInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !nextInfo.ModTime().Equal(info.ModTime()) {
		t.Fatal("re-applying the same Profile touched the file on disk")
	}
}

// A fresh install ships `model: ""` as an unconfigured sentinel, and a file may
// hold nothing but comments. Neither is corrupt, so neither may fail.
func TestWriteHermesHandlesAFreshOrEmptyConfig(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
	}{
		{"absent", ""},
		{"empty file", "\n"},
		{"comments only", "# nothing configured yet\n"},
		{"null model sentinel", "model:\ncustom_providers:\n"},
		{"empty string sentinel", "model: \"\"\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, ".hermes", "config.yaml")
			if testCase.content != "" {
				path = writeHermesFixture(t, home, testCase.content)
			}
			writer := testWriter(t, home, "linux")
			if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-a"); err != nil {
				t.Fatalf("%s config was rejected: %v", testCase.name, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			detected := ReadHermesConfig(string(data))
			if detected.Unreadable != nil || detected.Model != "model-a" || !detected.ManagedByOneAgent {
				t.Fatalf("%s round-trip = %#v\n%s", testCase.name, detected, data)
			}
		})
	}
}

// The API key lands in this file, so it must not be world-readable even though
// the rest of the document is ordinary settings.
func TestWriteHermesTightensPermissionsBecauseTheKeyLandsThere(t *testing.T) {
	home := t.TempDir()
	path := writeHermesFixture(t, home, hermesConfig)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(t, home, "linux")
	if err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-a"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config mode = %v, want 0600: the API key is in this file", mode)
	}
}

// Refusing is the right answer when the file's shape contradicts what a write
// would assume. Overwriting would discard whatever the user actually had there.
func TestWriteHermesRefusesAConfigItWouldHaveToClobber(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
	}{
		{"model is a list", "model:\n- not-a-mapping\n"},
		{"model is a string", "model: gpt-5\n"},
		{"providers is a mapping", "custom_providers:\n  nous:\n    base_url: https://example.test\n"},
		{"top level is a list", "- one\n- two\n"},
		{"not yaml at all", "{{ this is not yaml\n  - [unclosed\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			path := writeHermesFixture(t, home, testCase.content)
			writer := testWriter(t, home, "linux")
			err := writer.WriteHermes(context.Background(), path, "ppio", "https://api.ppio.com/openai", "sk-hermes-secret", "model-a")
			if err == nil {
				t.Fatalf("%s was accepted; the original content would have been lost", testCase.name)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(data) != testCase.content {
				t.Fatalf("a rejected write still changed the file:\n%s", data)
			}
			if strings.Contains(string(data), "sk-hermes-secret") {
				t.Fatal("a rejected write leaked the API key into the file")
			}
		})
	}
}

// A model set through `hermes model` against a provider Hermes resolves itself
// has no custom_providers entry. Reporting that as managed would let a later
// write silently take over a binding OneAgent never made.
func TestReadHermesConfigOnlyClaimsBindingsItWrote(t *testing.T) {
	unmanaged := ReadHermesConfig("model:\n  provider: openai\n  default: gpt-5.5-pro\n  base_url: https://api.openai.com/v1\n")
	if unmanaged.Unreadable != nil {
		t.Fatalf("unreadable: %s", *unmanaged.Unreadable)
	}
	if unmanaged.ManagedByOneAgent {
		t.Fatal("a provider with no custom_providers entry read as managed")
	}
	if unmanaged.Model != "gpt-5.5-pro" || unmanaged.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("unmanaged projection = %#v", unmanaged)
	}

	// A custom provider that is not the one the model names is somebody else's
	// entry, so it must not make this binding look managed either.
	other := ReadHermesConfig("model:\n  provider: openai\n  default: gpt-5\ncustom_providers:\n- name: nous\n  base_url: https://example.test\n")
	if other.ManagedByOneAgent {
		t.Fatal("a non-matching provider entry read as managed")
	}
}

func TestReadHermesConfigNeverReturnsCredentials(t *testing.T) {
	detected := ReadHermesConfig(hermesConfig)
	if detected.Unreadable != nil {
		t.Fatalf("unreadable: %s", *detected.Unreadable)
	}
	// Detected has no key field by design; this guards the projection rather than
	// the struct, in case a future edit adds one.
	if strings.Contains(detected.BaseURL, "nous-key") || strings.Contains(detected.Model, "nous-key") {
		t.Fatalf("projection carried credential material: %#v", detected)
	}
}
