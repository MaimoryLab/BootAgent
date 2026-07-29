package provider

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
)

// These URLs end up written into Agent configuration, so a difference between
// the two implementations is a request sent somewhere the user did not choose.
// Hand-written cases only cover what someone thought to write down; this runs
// both implementations over the same inputs, including the shapes nobody would
// think to test, and compares.

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

// runPython evaluates one expression per input against the real Python module
// and returns the results in order. Errors come back as the literal "ERROR" so
// a refusal on one side can be compared with a refusal on the other.
func runPython(t *testing.T, expression string, inputs []string) []string {
	t.Helper()
	root := repoRoot(t)
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent import providers
from oneagent.errors import OneAgentError

results = []
for value in json.loads(sys.argv[3]):
    try:
        results.append(eval(sys.argv[2]))
    except OneAgentError:
        results.append("ERROR")
print(json.dumps(results))
`
	encoded, err := json.Marshal(inputs)
	if err != nil {
		t.Fatalf("cannot encode inputs: %v", err)
	}
	cmd := exec.Command(pythonBin(t), "-c", script, root, expression, string(encoded))
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed for %q: %v", expression, err)
	}
	var results []string
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatalf("cannot read python output %q: %v", output, err)
	}
	return results
}

// urlShapes are what users actually paste, plus the shapes that have caused
// bugs: doubled slashes, pasted routes, query strings, ports, and a bare host.
var urlShapes = []string{
	"https://example.com",
	"https://example.com/",
	"https://example.com//",
	"https://example.com/v1",
	"https://example.com/v1/",
	"https://example.com/v1/chat/completions",
	"https://example.com/v1/responses",
	"https://example.com/v1/models",
	"https://example.com/chat/completions",
	"https://example.com/models",
	"https://example.com/openai",
	"https://example.com/openai/v1",
	"https://api.ppio.com/openai",
	"https://api.novita.ai/openai",
	"http://127.0.0.1:9000",
	"http://127.0.0.1:9000/v1",
	"https://example.com/a/b/c",
	"https://example.com/v1?key=value",
	"https://example.com/V1",
	"https://example.com/v1/chat/completions/",
	"https://example.com/v10",
	"https://example.com/v1/models/models",
}

func TestParityOpenAIBaseURLMatchesPython(t *testing.T) {
	want := runPython(t, "providers.openai_base_url(value)", urlShapes)
	for index, input := range urlShapes {
		if got := OpenAIBaseURL(input); got != want[index] {
			t.Errorf("OpenAIBaseURL(%q):\n  Go:     %q\n  Python: %q", input, got, want[index])
		}
	}
}

var anthropicShapes = []string{
	"https://api.ppio.com/anthropic",
	"https://api.ppio.com/anthropic/",
	"https://api.novita.ai/anthropic",
	"https://proxy.test/v1",
	"https://proxy.test/v1/",
	"https://proxy.test/v1/messages",
	"https://proxy.test/v1/messages/",
	"https://proxy.test/messages",
	"https://proxy.test/messages/",
	"https://proxy.test",
	"https://proxy.test//",
	"http://127.0.0.1:9000/anthropic",
	// The one that would break if the suffixes were stripped in a loop rather
	// than stopping at the first match.
	"https://proxy.test/v1/messages/messages",
	"https://proxy.test/v1/v1/messages",
}

func TestParityAnthropicMessagesURLMatchesPython(t *testing.T) {
	want := runPython(t, "providers.anthropic_messages_url(value)", anthropicShapes)
	for index, input := range anthropicShapes {
		if got := AnthropicMessagesURL(input); got != want[index] {
			t.Errorf("AnthropicMessagesURL(%q):\n  Go:     %q\n  Python: %q", input, got, want[index])
		}
	}
}

// validationShapes mixes accepted and refused values so the comparison covers
// both the normalisation and the rejection boundary.
var validationShapes = []string{
	"",
	"https://example.com",
	"https://example.com/",
	"https://example.com/v1///",
	"http://example.com",
	"ftp://example.com",
	"file:///etc/passwd",
	"example.com",
	"//example.com",
	"https://",
	"https:///missing-host",
	"https://user:pass@example.com",
	"https://user@example.com",
	// An empty userinfo section carries no credential, but Go builds a Userinfo
	// for it while Python tests username and password for truthiness. This is the
	// same divergence as in ResolveRegistry, which is why HasCredentials is shared.
	"https://@example.com",
	"https://:@example.com",
	"https://:pass@example.com",
	"https://example.com/\nHost: evil",
	"https://example.com/\r",
	"https://example.com/\x00",
	"https://example.com/\x7f",
	"https://example.com/\tx",
	"HTTPS://EXAMPLE.COM/V1",
	"https://例え.テスト/v1",
	"https://example.com:8443/v1",
	"https://example.com/v1#fragment",
}

func TestParityBaseURLValidationAgreesOnWhatToRefuse(t *testing.T) {
	// Where the two disagree, one of them writes an endpoint the other would
	// have refused -- and the refusals here exist because a control character
	// can split a config line and a credential in a URL lands on disk.
	want := runPython(t, "providers.validate_base_url(value)", validationShapes)
	for index, input := range validationShapes {
		got := "ERROR"
		if normalised, err := ValidateBaseURL(input); err == nil {
			got = normalised
		}
		if got != want[index] {
			t.Errorf("ValidateBaseURL(%q):\n  Go:     %q\n  Python: %q", input, got, want[index])
		}
	}
}

// modelShapes include the IDs that motivated separator anchoring, plus real
// names from provider catalogues.
var modelShapes = []string{
	"deepseek-v3",
	"deepseek/deepseek-v3",
	"gpt-5.6-terra",
	"whisper-large-v3",
	"bge-reranker-v2",
	"text-embeddings-large",
	"text-embedding-3-small",
	"qwen-vl-max",
	"resolver-1",
	"evolve-chat",
	"visionary-max",
	"flux-schnell",
	"sd-xl",
	"sdxl-turbo",
	"llama-guard-3",
	"moderation-latest",
	"sql-coder",
	"nl2sql",
	"speech-01",
	"tts-1",
	"asr-v2",
	"ocr-base",
	"image-gen-1",
	"embedding",
	"embed",
	"EMBED-UPPER",
	"model.with.dots",
	"model_with_underscores",
	"chat",
}

func TestParityPickChatModelMatchesPythonOnEveryShape(t *testing.T) {
	// Compared one ID at a time so a mismatch names the ID rather than only the
	// chosen model: the classifier is a regex, and which ID it rejects is what
	// actually differs when RE2 and Python's engine disagree.
	singles := make([]string, len(modelShapes))
	copy(singles, modelShapes)
	want := runPython(t, "providers.pick_chat_model([value])", singles)
	for index, model := range singles {
		// A single-element list always returns that element, so this compares
		// classification indirectly; the list case below compares selection.
		if want[index] != model {
			t.Fatalf("python pick_chat_model([%q]) = %q, expected the sole entry", model, want[index])
		}
	}

	// Selection over the whole list is the behaviour callers depend on.
	encoded, err := json.Marshal(modelShapes)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	wholeList := runPython(t, "providers.pick_chat_model(json.loads(value))", []string{string(encoded)})
	if got := PickChatModel(modelShapes); got != wholeList[0] {
		t.Errorf("PickChatModel over %d shapes:\n  Go:     %q\n  Python: %q", len(modelShapes), got, wholeList[0])
	}
}

func TestParityNonChatClassificationMatchesPythonIDByID(t *testing.T) {
	// The regex is where RE2 and Python's engine could differ, so each ID is
	// classified individually rather than only through the selection result --
	// otherwise a disagreement on a later entry would be masked by an earlier
	// eligible one.
	script := `providers.pick_chat_model([value, "zzz-definitely-chat"])`
	want := runPython(t, script, modelShapes)
	for index, model := range modelShapes {
		// Python returns the model itself when it looks like chat, and the
		// sentinel when it does not.
		pythonSaysChat := want[index] == model
		goSaysChat := !nonChatModel.MatchString(model)
		if pythonSaysChat != goSaysChat {
			t.Errorf("%q: Go says chat=%v, Python says chat=%v", model, goSaysChat, pythonSaysChat)
		}
	}
}

func TestParityProviderBaseAndConfigBaseMatchPython(t *testing.T) {
	// Which route an Agent is pointed at is the whole reason ConfigBase is
	// keyed on protocol. A divergence here is an Agent that cannot authenticate
	// while reporting success.
	type probe struct{ provider, custom, protocol string }
	probes := []probe{
		{"ppio", "", "openai"},
		{"ppio", "", "anthropic"},
		{"ppio", "", "responses"},
		{"novita", "", "openai"},
		{"novita", "", "anthropic"},
		{"ppio", "https://override.example.com", "anthropic"},
		{"ppio", "https://override.example.com/", "openai"},
		{"custom", "http://127.0.0.1:9000", "anthropic"},
		{"custom", "http://127.0.0.1:9000/", "openai"},
		{"custom", "", "openai"},
		{"unknown", "", "openai"},
		{"", "", "openai"},
		{"ppio", "", "carrier-pigeon"},
	}
	inputs := make([]string, len(probes))
	for index, item := range probes {
		encoded, err := json.Marshal([]string{item.provider, item.custom, item.protocol})
		if err != nil {
			t.Fatalf("cannot encode: %v", err)
		}
		inputs[index] = string(encoded)
	}
	want := runPython(
		t,
		"providers.provider_config_base(*json.loads(value))",
		inputs,
	)
	for index, item := range probes {
		got := "ERROR"
		if base, err := ConfigBase(item.provider, item.custom, item.protocol); err == nil {
			got = base
		}
		if got != want[index] {
			t.Errorf("ConfigBase(%q, %q, %q):\n  Go:     %q\n  Python: %q",
				item.provider, item.custom, item.protocol, got, want[index])
		}
	}
}

func TestParityFallbackProbeModelMatchesPython(t *testing.T) {
	// The probe gates the wizard, so a fallback naming a model the provider
	// does not serve fails for a reason the user cannot act on.
	providers := []string{"ppio", "novita", "custom", "", "not-a-provider"}
	root := repoRoot(t)
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent import catalog
print(json.dumps([catalog.fallback_probe_model(p) for p in json.loads(sys.argv[2])]))
`
	encoded, _ := json.Marshal(providers)
	cmd := exec.Command(pythonBin(t), "-c", script, root, string(encoded))
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python fallback_probe_model failed: %v", err)
	}
	var want []string
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	for index, provider := range providers {
		if got := catalog.FallbackProbeModel(provider); got != want[index] {
			t.Errorf("fallback for %q: Go=%q Python=%q", provider, got, want[index])
		}
	}
}

func TestParityUnsupportedProtocolClassificationMatchesPython(t *testing.T) {
	// Classifying a permanent failure as retryable has the user retry something
	// that cannot succeed instead of choosing a workable model.
	type probe struct {
		code int
		body string
	}
	probes := []probe{
		{404, ""}, {405, ""}, {501, ""},
		{400, "does not support endpoint"},
		{400, "Does Not Support Endpoint"},
		{422, "not implemented"},
		{500, "unsupported endpoint"},
		{400, "unknown endpoint"},
		{400, "missing field"},
		{401, "does not support endpoint"},
		{429, "not implemented"},
		{500, "internal error"},
		{200, ""},
		{503, "does not support endpoint"},
	}
	inputs := make([]string, len(probes))
	for index, item := range probes {
		encoded, _ := json.Marshal([]any{item.code, item.body})
		inputs[index] = string(encoded)
	}
	want := runPython(
		t,
		`str(providers._unsupported_protocol(*json.loads(value)))`,
		inputs,
	)
	for index, item := range probes {
		got := "False"
		if UnsupportedProtocol(item.code, item.body) {
			got = "True"
		}
		if got != want[index] {
			t.Errorf("UnsupportedProtocol(%d, %q): Go=%s Python=%s", item.code, item.body, got, want[index])
		}
	}
}

func TestParityProtocolLabelsMatchPython(t *testing.T) {
	// The label is user-visible text in the probe result.
	protocols := []string{"openai", "anthropic", "responses", "carrier-pigeon", ""}
	want := runPython(t, "providers.protocol_label(value)", protocols)
	for index, protocol := range protocols {
		if got := ProtocolLabel(protocol); got != want[index] {
			t.Errorf("ProtocolLabel(%q): Go=%q Python=%q", protocol, got, want[index])
		}
	}
}
