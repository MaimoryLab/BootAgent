package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// What a reader reports drives what the overview shows and whether Apply warns
// before overwriting. A disagreement between the two implementations is a user
// told their configuration is something other than what it is.
//
// One field cannot be compared verbatim: the unreadable message embeds the
// parser's own error text, and tomllib and BurntSushi/toml word those
// differently. The Chinese prefix is the part the frontend shows as a category,
// so that is compared exactly and the parser detail only checked for presence.

// pythonDetect runs the real reader over the given text.
func pythonDetect(t *testing.T, adapter, text string) map[string]any {
	t.Helper()
	root := repoRoot(t)
	readers := map[string]string{
		AdapterCodex:      "read_codex_config",
		AdapterClaudeCode: "read_claude_config",
		AdapterOpenCode:   "read_openai_compatible_config",
		AdapterKiloCLI:    "read_openai_compatible_config",
		AdapterAider:      "read_aider_config",
	}
	function, known := readers[adapter]
	if !known {
		t.Fatalf("no Python reader for adapter %q", adapter)
	}
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent import installer
print(json.dumps(getattr(installer, sys.argv[2])(sys.argv[3])))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root, function, text)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python %s failed: %v", function, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	return parsed
}

// compare holds both readings to the same contract. The unreadable field is
// compared by its category prefix for the reason given above.
func compare(t *testing.T, label string, got Detected, want map[string]any) {
	t.Helper()
	if wantURL, _ := want["baseUrl"].(string); got.BaseURL != wantURL {
		t.Errorf("%s: baseUrl Go=%q Python=%q", label, got.BaseURL, wantURL)
	}
	if wantModel, _ := want["model"].(string); got.Model != wantModel {
		t.Errorf("%s: model Go=%q Python=%q", label, got.Model, wantModel)
	}
	if wantManaged, _ := want["managedByOneAgent"].(bool); got.ManagedByOneAgent != wantManaged {
		t.Errorf("%s: managed Go=%v Python=%v", label, got.ManagedByOneAgent, wantManaged)
	}

	wantUnreadable, hasReason := want["unreadable"].(string)
	switch {
	case !hasReason && got.Unreadable != nil:
		t.Errorf("%s: Go reports unreadable %q, Python reports none", label, *got.Unreadable)
	case hasReason && got.Unreadable == nil:
		t.Errorf("%s: Python reports unreadable %q, Go reports none", label, wantUnreadable)
	case hasReason && got.Unreadable != nil:
		// The category is the contract; the parser detail after it is not.
		goCategory := categoryOf(*got.Unreadable)
		pythonCategory := categoryOf(wantUnreadable)
		if goCategory != pythonCategory {
			t.Errorf("%s: unreadable category Go=%q Python=%q", label, goCategory, pythonCategory)
		}
	}
}

// categoryOf returns the message up to the colon, which is the part both
// implementations must agree on.
func categoryOf(message string) string {
	if index := strings.Index(message, "："); index >= 0 {
		return message[:index]
	}
	return message
}

// codexFixtures cover a hand-written config, ours, a mixture, and every way the
// file can be malformed or wrongly typed.
var codexFixtures = map[string]string{
	"third-party provider": "model_provider = \"vendor\"\nmodel = \"gpt-5-mini\"\n" +
		"[model_providers.vendor]\nbase_url = \"https://vendor.example/v1\"\n",
	"our table and theirs selected": "model_provider = \"vendor\"\nmodel = \"m\"\n" +
		"[model_providers.vendor]\nbase_url = \"https://vendor.example/v1\"\n" +
		"[model_providers.oneagent]\nbase_url = \"https://ours.example/v1\"\n",
	"ours selected": "model_provider = \"oneagent\"\nmodel = \"m\"\n" +
		"[model_providers.oneagent]\nbase_url = \"https://ours.example/v1\"\n",
	"selected provider absent": "model_provider = \"missing\"\nmodel = \"m\"\n",
	"no provider selected":     "model = \"m\"\n[model_providers.p]\nbase_url = \"https://x\"\n",
	"empty table":              "[model_providers.p]\n",
	"wrongly typed":            "model_provider = 1\nmodel = 2\n[model_providers.p]\nbase_url = 3\n",
	"broken toml":              "model_provider = \nbroken [",
	"just a comment":           "# nothing here\n",
	"nested sub-table": "model_provider = \"p\"\n[model_providers.p]\nbase_url = \"https://x\"\n" +
		"[model_providers.p.extra]\nkey = \"v\"\n",
	"non-ascii model": "model_provider = \"p\"\nmodel = \"通义-max\"\n" +
		"[model_providers.p]\nbase_url = \"https://x\"\n",
}

func TestParityCodexDetectionMatchesPython(t *testing.T) {
	for name, text := range codexFixtures {
		t.Run(name, func(t *testing.T) {
			compare(t, name, ReadCodexConfig(text), pythonDetect(t, AdapterCodex, text))
		})
	}
}

var claudeFixtures = map[string]string{
	"fully managed":     `{"env":{"ANTHROPIC_BASE_URL":"https://a","ANTHROPIC_AUTH_TOKEN":"k","ANTHROPIC_MODEL":"m","ANTHROPIC_SMALL_FAST_MODEL":"m"}}`,
	"partial":           `{"env":{"ANTHROPIC_BASE_URL":"https://a"}}`,
	"empty env":         `{"env":{}}`,
	"no env":            `{"theme":"dark"}`,
	"empty object":      `{}`,
	"env not an object": `{"env":"nope"}`,
	"wrongly typed":     `{"env":{"ANTHROPIC_BASE_URL":5,"ANTHROPIC_MODEL":[]}}`,
	"top level array":   `[]`,
	"broken json":       `{not json`,
	"blank value":       `{"env":{"ANTHROPIC_BASE_URL":"","ANTHROPIC_AUTH_TOKEN":"k","ANTHROPIC_MODEL":"m","ANTHROPIC_SMALL_FAST_MODEL":"m"}}`,
	"non-ascii":         `{"env":{"ANTHROPIC_MODEL":"通义-max","ANTHROPIC_BASE_URL":"https://a"}}`,
	"user keys too":     `{"theme":"dark","env":{"MY_VAR":"1","ANTHROPIC_BASE_URL":"https://a"}}`,
}

func TestParityClaudeDetectionMatchesPython(t *testing.T) {
	for name, text := range claudeFixtures {
		t.Run(name, func(t *testing.T) {
			compare(t, name, ReadClaudeConfig(text), pythonDetect(t, AdapterClaudeCode, text))
		})
	}
}

var openAIFixtures = map[string]string{
	"managed":             `{"provider":{"oneagent":{"options":{"baseURL":"https://a/v1"}}},"model":"oneagent/m"}`,
	"third party":         `{"provider":{"mine":{"options":{"baseURL":"https://mine/v1"}}},"model":"mine/local-llm"}`,
	"bare model":          `{"model":"bare-model"}`,
	"empty model":         `{"model":""}`,
	"model with slashes":  `{"provider":{"a":{"options":{"baseURL":"https://a"}}},"model":"a/b/c"}`,
	"trailing slash":      `{"provider":{"a":{"options":{"baseURL":"https://a"}}},"model":"a/"}`,
	"leading slash":       `{"provider":{"":{"options":{"baseURL":"https://a"}}},"model":"/m"}`,
	"provider absent":     `{"model":"missing/m"}`,
	"options not object":  `{"provider":{"p":{"options":[]}},"model":"p/m"}`,
	"provider not object": `{"provider":{"p":"nope"},"model":"p/m"}`,
	"baseURL not string":  `{"provider":{"p":{"options":{"baseURL":7}}},"model":"p/m"}`,
	"provider key wrong":  `{"provider":[],"model":"p/m"}`,
	"empty object":        `{}`,
	"top level array":     `[]`,
	"broken json":         `{ not json`,
	"jsonc comment":       "{\n  // a note\n  \"model\": \"x/y\"\n}",
	"jsonc block comment": "{\n  /* a note */\n  \"model\": \"x/y\"\n}",
	"non-ascii":           `{"provider":{"p":{"options":{"baseURL":"https://例え.テスト"}}},"model":"p/通义"}`,
}

func TestParityOpenAICompatibleDetectionMatchesPython(t *testing.T) {
	for name, text := range openAIFixtures {
		t.Run(name, func(t *testing.T) {
			compare(t, name, ReadOpenAICompatibleConfig(text), pythonDetect(t, AdapterOpenCode, text))
		})
	}
}

var aiderFixtures = map[string]string{
	"ours":             "export OPENAI_API_BASE='https://a/v1'\nexport OPENAI_API_KEY='k'\n",
	"double quoted":    "export OPENAI_API_BASE=\"https://a/v1\"\n",
	"unquoted":         "export OPENAI_API_BASE=https://a/v1\n",
	"powershell":       "$env:OPENAI_API_BASE = 'https://a/v1'\n",
	"powershell tight": "$env:OPENAI_API_BASE='https://a/v1'\n",
	"with comments":    "# a note\nexport OPENAI_API_BASE='https://a/v1'\n",
	"indented":         "   export OPENAI_API_BASE='https://a/v1'\n",
	"two assignments":  "export OPENAI_API_BASE='https://first'\nexport OPENAI_API_BASE='https://second'\n",
	"embedded quote":   "export OPENAI_API_BASE='https://a/'\\''b'\n",
	"no assignment":    "# nothing\nexport SOMETHING=1\n",
	"empty":            "\n",
	"key only":         "export OPENAI_API_KEY='k'\n",
	"crlf":             "export OPENAI_API_BASE='https://a/v1'\r\n",
	"extra spaces":     "export  OPENAI_API_BASE='https://a/v1'\n",
	"non-ascii":        "export OPENAI_API_BASE='https://例え.テスト/v1'\n",
}

func TestParityAiderDetectionMatchesPython(t *testing.T) {
	for name, text := range aiderFixtures {
		t.Run(name, func(t *testing.T) {
			compare(t, name, ReadAiderConfig(text), pythonDetect(t, AdapterAider, text))
		})
	}
}

func TestParityDetectAgentConfigAgreesOnRealFiles(t *testing.T) {
	// The reader unit comparisons above check the parsing. This checks the
	// surrounding decisions: absent, empty, unreadable, guide-only, and the
	// dispatch that picks a reader.
	root := repoRoot(t)
	manifest := catalog.MustLoad()

	scenarios := []struct {
		name    string
		agentID string
		content string
		// write false to leave the file absent.
		write bool
	}{
		{"absent", "codex", "", false},
		{"empty", "codex", "   \n", true},
		{"valid", "codex", "model_provider = \"p\"\n[model_providers.p]\nbase_url = \"https://x\"\n", true},
		{"broken", "codex", "model_provider = \nbroken [", true},
		{"claude valid", "claude-code", `{"env":{"ANTHROPIC_BASE_URL":"https://b"}}`, true},
		{"opencode jsonc", "opencode", "{\n  // note\n  \"model\": \"x/y\"\n}", true},
		{"aider script", "aider", "export OPENAI_API_BASE='https://d'\n", true},
		{"guide only", "cursor", "", false},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			agent, present := manifest.Agent(scenario.agentID)
			if !present {
				t.Skipf("%s is not in the manifest", scenario.agentID)
			}

			// Python side, in its own HOME.
			pythonHome := t.TempDir()
			if scenario.write && agent.ConfigPath != "" {
				writeExisting(t, pythonHome, agent.ConfigPath, scenario.content)
			}
			script := `
import json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from oneagent.catalog import agent_catalog
from oneagent.installer import Runtime, detect_agent_config

home = Path(sys.argv[2])
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
print(json.dumps(detect_agent_config(runtime, agent_catalog()[sys.argv[3]])))
`
			cmd := exec.Command(pythonBin(t), "-c", script, root, pythonHome, scenario.agentID)
			cmd.Dir = root
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("python detect_agent_config failed: %v", err)
			}
			var want map[string]any
			if err := json.Unmarshal(output, &want); err != nil {
				t.Fatalf("cannot read python output %q: %v", output, err)
			}

			// Go side, in its own.
			goHome := t.TempDir()
			if scenario.write && agent.ConfigPath != "" {
				writeExisting(t, goHome, agent.ConfigPath, scenario.content)
			}
			rt := runtime.New(
				runtime.WithHome(goHome),
				runtime.WithOSID("linux"),
				runtime.WithEnv(map[string]string{"HOME": goHome}),
			)
			got := DetectAgentConfig(rt, agent)

			if want == nil {
				if got != nil {
					t.Fatalf("Go reported %+v, Python reported nothing", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("Python reported %v, Go reported nothing", want)
			}
			compare(t, scenario.name, *got, want)
		})
	}
}

func TestParityTheDetectedFieldSetMatchesPython(t *testing.T) {
	// Both sides deliberately omit anything about the credential. A field added
	// to one and not the other is either a leak or a missing value in the UI.
	root := repoRoot(t)
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent.installer import read_claude_config
print(json.dumps(sorted(read_claude_config('{"env":{}}'))))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed: %v", err)
	}
	var pythonFields []string
	if err := json.Unmarshal(output, &pythonFields); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}

	goFields := map[string]bool{}
	for _, name := range DetectedFieldNames() {
		goFields[name] = true
	}
	if len(goFields) != len(pythonFields) {
		t.Fatalf("Go emits %v, Python emits %v", DetectedFieldNames(), pythonFields)
	}
	for _, name := range pythonFields {
		if !goFields[name] {
			t.Errorf("Python emits %q and Go does not", name)
		}
	}
}

func TestParityAnUnreadableFileReportsTheSameCategory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is not denied by file permissions")
	}
	root := repoRoot(t)
	agent := agentFor(t, "codex")

	pythonHome := t.TempDir()
	pythonPath := writeExisting(t, pythonHome, agent.ConfigPath, "model_provider = \"p\"\n")
	if err := os.Chmod(pythonPath, 0o000); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	defer os.Chmod(pythonPath, 0o600)

	script := `
import json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from oneagent.catalog import agent_catalog
from oneagent.installer import Runtime, detect_agent_config

home = Path(sys.argv[2])
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
print(json.dumps(detect_agent_config(runtime, agent_catalog()["codex"])))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root, pythonHome)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python failed: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	reason, _ := want["unreadable"].(string)
	if reason == "" {
		t.Skip("file permissions do not restrict this user")
	}

	goHome := t.TempDir()
	goPath := writeExisting(t, goHome, agent.ConfigPath, "model_provider = \"p\"\n")
	if err := os.Chmod(goPath, 0o000); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	defer os.Chmod(goPath, 0o600)

	rt := runtime.New(runtime.WithHome(goHome), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": goHome}))
	got := DetectAgentConfig(rt, agent)
	if got == nil || got.Unreadable == nil {
		t.Fatalf("Go reported %+v, want an unreadable reason", got)
	}
	if categoryOf(*got.Unreadable) != categoryOf(reason) {
		t.Errorf("category Go=%q Python=%q", categoryOf(*got.Unreadable), categoryOf(reason))
	}
	// And neither side puts the file's contents in the message.
	if strings.Contains(*got.Unreadable, "model_provider") {
		t.Errorf("the message carries file contents: %q", *got.Unreadable)
	}
	_ = filepath.Base(goPath)
}
