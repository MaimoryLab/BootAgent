package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// The defect these exist for: status_payload reported configured with provider,
// model and baseUrl all null for a config OneAgent had not written itself. The
// file was seen but never read, so the overview said "not configured" while a
// configuration was live -- and Apply would have silently overwritten it.

func TestCodexReadsTheProviderTheFileActuallySelects(t *testing.T) {
	got := ReadCodexConfig(
		"model_provider = \"someone-else\"\nmodel = \"gpt-5-mini\"\n" +
			"[model_providers.someone-else]\nbase_url = \"https://api.other-vendor.com/v1\"\n" +
			"env_key = \"OTHER_KEY\"\n",
	)
	if got.BaseURL != "https://api.other-vendor.com/v1" {
		t.Errorf("baseUrl = %q", got.BaseURL)
	}
	if got.Model != "gpt-5-mini" {
		t.Errorf("model = %q", got.Model)
	}
	if got.ManagedByOneAgent {
		t.Error("a third-party config must not be reported as ours")
	}
}

func TestCodexIgnoresOurTableWhenTheFileSelectsAnother(t *testing.T) {
	// Reading [model_providers.oneagent] unconditionally would report a
	// configuration the Agent is not using.
	got := ReadCodexConfig(
		"model_provider = \"vendor\"\nmodel = \"m\"\n" +
			"[model_providers.vendor]\nbase_url = \"https://vendor.example/v1\"\n" +
			"[model_providers.oneagent]\nbase_url = \"https://ours.example/v1\"\n",
	)
	if got.BaseURL != "https://vendor.example/v1" {
		t.Errorf("baseUrl = %q, want the selected provider's", got.BaseURL)
	}
	// Our table is present, so the file has been through OneAgent at some point.
	if !got.ManagedByOneAgent {
		t.Error("our table is present, so the file has been through OneAgent")
	}
}

func TestClaudeRequiresEveryDeclaredVariableToCountAsManaged(t *testing.T) {
	// A file holding only a base URL was configured by someone else. Claiming it
	// as ours would make Apply look safe when it would overwrite their work.
	agent, present := catalog.MustLoad().Agent("claude-code")
	if !present || len(agent.EnvVars) == 0 {
		t.Skip("claude-code declares no env_vars")
	}

	partial, _ := json.Marshal(map[string]any{"env": map[string]string{agent.EnvVars["base_url"]: "https://x.example"}})
	got := ReadClaudeConfig(string(partial))
	if got.BaseURL != "https://x.example" {
		t.Errorf("baseUrl = %q", got.BaseURL)
	}
	if got.ManagedByOneAgent {
		t.Error("a partial config must not be reported as managed")
	}

	full := map[string]string{}
	for _, name := range agent.EnvVars {
		full[name] = "v"
	}
	complete, _ := json.Marshal(map[string]any{"env": full})
	if !ReadClaudeConfig(string(complete)).ManagedByOneAgent {
		t.Error("a config with every declared variable should read as managed")
	}
}

func TestOpenAICompatibleStripsTheProviderPrefixFromTheModel(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"provider": map[string]any{"mine": map[string]any{"options": map[string]any{"baseURL": "https://mine.example/v1"}}},
		"model":    "mine/local-llm",
	})
	got := ReadOpenAICompatibleConfig(string(raw))
	if got.BaseURL != "https://mine.example/v1" {
		t.Errorf("baseUrl = %q", got.BaseURL)
	}
	if got.Model != "local-llm" {
		t.Errorf("model = %q, want the bare model", got.Model)
	}
}

func TestAModelWithNoProviderPrefixStillReadsAsAModel(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"model": "bare-model"})
	if got := ReadOpenAICompatibleConfig(string(raw)).Model; got != "bare-model" {
		t.Errorf("model = %q", got)
	}
}

func TestJSONCCommentsAreNamedAsSuchRatherThanBrokenJSON(t *testing.T) {
	// Telling the user their valid JSONC is invalid JSON leaves them nothing to
	// act on.
	got := ReadOpenAICompatibleConfig("{\n  // a comment\n  \"model\": \"x/y\"\n}")
	if got.Unreadable == nil || !strings.Contains(*got.Unreadable, "JSONC") {
		t.Errorf("unreadable = %v, want it to name JSONC", got.Unreadable)
	}
}

func TestAiderIsParsedLineByLineAndNeverExecuted(t *testing.T) {
	// The file holds the key and is user-editable; executing it would both leak
	// the credential and run arbitrary shell.
	got := ReadAiderConfig(
		"export OPENAI_API_BASE='https://hand.example/v1'\n" +
			"export OPENAI_API_KEY='sk-must-not-be-read'\n",
	)
	if got.BaseURL != "https://hand.example/v1" {
		t.Errorf("baseUrl = %q", got.BaseURL)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "sk-must-not-be-read") {
		t.Errorf("the credential reached the result: %s", encoded)
	}
}

func TestAiderNeverClaimsToKnowWhoWroteItsScript(t *testing.T) {
	// A hand-written script and ours are the same two exports, so there is no
	// marker to tell them apart. Reporting either as managed would be a guess.
	ours := ReadAiderConfig("export OPENAI_API_BASE='https://a.example/v1'\nexport OPENAI_API_KEY='k'\n")
	theirs := ReadAiderConfig("export OPENAI_API_BASE='https://a.example/v1'\n")
	if ours.ManagedByOneAgent || theirs.ManagedByOneAgent {
		t.Error("aider cannot distinguish its own script from a hand-written one")
	}
}

func TestPowerShellAssignmentsAreUnderstoodToo(t *testing.T) {
	got := ReadAiderConfig("$env:OPENAI_API_BASE = 'https://win.example/v1'\n")
	if got.BaseURL != "https://win.example/v1" {
		t.Errorf("baseUrl = %q", got.BaseURL)
	}
}

func TestAnAiderScriptIsReadWhateverItsQuoting(t *testing.T) {
	for input, want := range map[string]string{
		`export OPENAI_API_BASE="https://theirs.example/v1"`:       "https://theirs.example/v1",
		"# a note\nexport OPENAI_API_BASE=https://bare.example/v1": "https://bare.example/v1",
		"export OPENAI_API_BASE='https://quoted.example/v1'":       "https://quoted.example/v1",
	} {
		if got := ReadAiderConfig(input).BaseURL; got != want {
			t.Errorf("input %q: baseUrl = %q, want %q", input, got, want)
		}
	}
}

func TestAReaderReportsAReasonInsteadOfFailing(t *testing.T) {
	// One broken config must not take down the whole status request.
	for _, got := range []Detected{
		ReadCodexConfig("model_provider = \nbroken ["),
		ReadClaudeConfig("{not json"),
		ReadOpenAICompatibleConfig("[]"),
		ReadOpenAICompatibleConfig("{ not json"),
		ReadClaudeConfig("[]"),
	} {
		if got.Unreadable == nil || *got.Unreadable == "" {
			t.Errorf("%+v: expected a reason", got)
		}
		if got.BaseURL != "" {
			t.Errorf("%+v: an unreadable file must report no endpoint", got)
		}
	}
}

func TestWronglyTypedFieldsAreIgnoredRatherThanReportedAsValues(t *testing.T) {
	// These files are user-editable, so any field can be the wrong shape. A
	// non-string endpoint must read as absent, not crash and not stringify.
	codex := ReadCodexConfig("model_provider = 1\nmodel = 2\n[model_providers.p]\nbase_url = 3\n")
	if codex.BaseURL != "" || codex.Model != "" {
		t.Errorf("codex = %+v, want empty fields", codex)
	}

	for _, raw := range []string{
		`{"provider":{"p":"nope"},"model":"p/m"}`,
		`{"provider":{"p":{"options":[]}},"model":"p/m"}`,
		`{"provider":{"p":{"options":{"baseURL":7}}},"model":"p/m"}`,
	} {
		if got := ReadOpenAICompatibleConfig(raw).BaseURL; got != "" {
			t.Errorf("%s: baseUrl = %q, want empty", raw, got)
		}
	}
	if got := ReadClaudeConfig(`{"env":{"ANTHROPIC_BASE_URL":5}}`).BaseURL; got != "" {
		t.Errorf("baseUrl = %q, want empty", got)
	}
	if got := ReadClaudeConfig(`{"env":"not-an-object"}`).BaseURL; got != "" {
		t.Errorf("baseUrl = %q, want empty", got)
	}
}

func TestDetectedCarriesNoFieldAboutTheCredentialAtAll(t *testing.T) {
	// Even a boolean would say whether this machine has a key configured. The
	// assertion is on the serialised field set because that is what reaches the
	// frontend.
	want := map[string]bool{"baseUrl": true, "model": true, "managedByOneAgent": true, "unreadable": true}
	got := DetectedFieldNames()
	if len(got) != len(want) {
		t.Fatalf("fields = %v, want exactly %v", got, want)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected field %q in the detected payload", name)
		}
	}
}

func TestNoConfigFormatCanPutItsKeyIntoTheDetectedResult(t *testing.T) {
	// Three of the five formats hold the credential in plain text. This reads a
	// real file of each shape and proves the key does not come back -- while also
	// proving the endpoints do, so it is not passing by reading nothing.
	const secret = "sk-detected-must-never-surface"
	home := t.TempDir()
	rt := runtime.New(runtime.WithHome(home), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": home}))
	manifest := catalog.MustLoad()

	files := map[string]string{
		"codex": "model_provider = \"p\"\nmodel = \"m\"\n[model_providers.p]\n" +
			"base_url = \"https://a.example\"\napi_key = \"" + secret + "\"\n",
		"claude-code": `{"env":{"ANTHROPIC_BASE_URL":"https://b.example","ANTHROPIC_AUTH_TOKEN":"` + secret + `"}}`,
		"opencode": `{"provider":{"p":{"options":{"baseURL":"https://c.example","apiKey":"` + secret +
			`"}}},"model":"p/m"}`,
		"aider": "export OPENAI_API_BASE='https://d.example'\nexport OPENAI_API_KEY='" + secret + "'\n",
	}
	expectedEndpoints := map[string]string{
		"codex":       "https://a.example",
		"claude-code": "https://b.example",
		"opencode":    "https://c.example",
		"aider":       "https://d.example",
	}

	for id, content := range files {
		agent, present := manifest.Agent(id)
		if !present {
			t.Fatalf("%s is not in the manifest", id)
		}
		writeExisting(t, home, agent.ConfigPath, content)
	}

	for id, wantEndpoint := range expectedEndpoints {
		agent, _ := manifest.Agent(id)
		got := DetectAgentConfig(rt, agent)
		if got == nil {
			t.Errorf("%s: no configuration detected", id)
			continue
		}
		encoded, _ := json.Marshal(got)
		if strings.Contains(string(encoded), secret) {
			t.Errorf("%s leaked the credential: %s", id, encoded)
		}
		if got.BaseURL != wantEndpoint {
			t.Errorf("%s: baseUrl = %q, want %q", id, got.BaseURL, wantEndpoint)
		}
	}
}

func TestAHandWrittenConfigIsReportedInsteadOfNulls(t *testing.T) {
	// The defect this work exists for.
	home := t.TempDir()
	rt := runtime.New(runtime.WithHome(home), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": home}))
	agent := agentFor(t, "codex")
	writeExisting(t, home, agent.ConfigPath,
		"model_provider = \"vendor\"\nmodel = \"gpt-5-mini\"\n"+
			"[model_providers.vendor]\nbase_url = \"https://api.other-vendor.com/v1\"\n")

	got := DetectAgentConfig(rt, agent)
	if got == nil {
		t.Fatal("a hand-written config was not detected")
	}
	if got.BaseURL != "https://api.other-vendor.com/v1" || got.Model != "gpt-5-mini" {
		t.Errorf("detected = %+v", got)
	}
	if got.ManagedByOneAgent {
		t.Error("OneAgent did not write this file")
	}
}

func TestGuideOnlyAgentsReportNothingDetected(t *testing.T) {
	// OneAgent never writes their config, so it has nothing to say about it.
	home := t.TempDir()
	rt := runtime.New(runtime.WithHome(home), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": home}))
	manifest := catalog.MustLoad()
	for id, agent := range manifest.Agents {
		if !agent.GuideOnly() {
			continue
		}
		if got := DetectAgentConfig(rt, agent); got != nil {
			t.Errorf("%s is guide-only but reported %+v", id, got)
		}
	}
}

func TestAnAgentWithNoReaderSaysSoRatherThanGuessing(t *testing.T) {
	// A lock entry can name an adapter before a reader for it exists.
	home := t.TempDir()
	rt := runtime.New(runtime.WithHome(home), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": home}))
	agent := agentFor(t, "codex")
	agent.ConfigAdapter = "some-future-format"

	got := DetectAgentConfig(rt, agent)
	if got == nil || got.Unreadable == nil {
		t.Fatalf("detected = %+v, want a reason", got)
	}
	if !strings.Contains(*got.Unreadable, "解析器") {
		t.Errorf("unreadable = %q, want it to say no reader exists", *got.Unreadable)
	}
	if got.BaseURL != "" {
		t.Errorf("baseUrl = %q, want no guess", got.BaseURL)
	}
}

func TestAnAbsentAndAnEmptyConfigAreDistinguished(t *testing.T) {
	home := t.TempDir()
	rt := runtime.New(runtime.WithHome(home), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": home}))
	agent := agentFor(t, "codex")

	// Absent: nothing to report.
	if got := DetectAgentConfig(rt, agent); got != nil {
		t.Errorf("detected = %+v, want nil for an absent file", got)
	}
	// Present but empty: worth saying so, since the file exists.
	writeExisting(t, home, agent.ConfigPath, "   \n")
	got := DetectAgentConfig(rt, agent)
	if got == nil || got.Unreadable == nil || !strings.Contains(*got.Unreadable, "空") {
		t.Errorf("detected = %+v, want an empty-file reason", got)
	}
}

func TestAnUnreadableFileReportsTheReasonNotTheContents(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is not denied by file permissions")
	}
	home := t.TempDir()
	rt := runtime.New(runtime.WithHome(home), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": home}))
	agent := agentFor(t, "codex")
	path := writeExisting(t, home, agent.ConfigPath, "model_provider = \"p\"\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	defer os.Chmod(path, 0o600)

	got := DetectAgentConfig(rt, agent)
	if got == nil || got.Unreadable == nil {
		t.Skip("file permissions do not restrict this user")
	}
	if !strings.Contains(*got.Unreadable, "无法读取") {
		t.Errorf("unreadable = %q, want it to say the file cannot be read", *got.Unreadable)
	}
}

func TestAParseFailureNeverEchoesWhatTheParserRead(t *testing.T) {
	// This message travels through the status payload into React state and onto
	// the screen, so anything the parser quotes back is published. BurntSushi/toml
	// quotes the offending token: an unquoted `api_key = sk-...` produced
	// `expected value but found "sk" instead`, and a dotted value came back whole
	// inside `Invalid float value: "..."`. A hand-edited file with an unquoted key
	// is uncommon, but broken user files are the only reason this reader exists.
	//
	// Python's tomllib and json report a position and never the content, so
	// dropping the detail is also the closer parity position -- and the positions
	// themselves disagree, so reporting line and column would introduce a new one.
	const secret = "sk-probe-abcdefghijklmnop"
	cases := []struct {
		name   string
		read   func(string) Detected
		text   string
		prefix string
	}{
		{"unquoted TOML value", ReadCodexConfig, "api_key = " + secret + "\n", "TOML 无法解析"},
		{"dotted TOML value", ReadCodexConfig, "api_key = 1.2." + secret + "\n", "TOML 无法解析"},
		{"unquoted JSON value", ReadClaudeConfig, `{"env": {"KEY": ` + secret + `}}`, "JSON 无法解析"},
		{"unquoted JSON key", ReadOpenAICompatibleConfig, `{` + secret + `: 1}`, "JSON 无法解析"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.read(testCase.text)
			if got.Unreadable == nil {
				t.Fatalf("the file is invalid but read as %+v", got)
			}
			if *got.Unreadable != testCase.prefix {
				t.Errorf("unreadable = %q, want exactly %q with no parser detail",
					*got.Unreadable, testCase.prefix)
			}
			// Checked separately from the equality above so a future change that
			// appends anything at all still names the leak as the reason.
			if strings.Contains(*got.Unreadable, secret) {
				t.Errorf("the message carries credential material: %q", *got.Unreadable)
			}
			for _, fragment := range []string{"sk-", "sk", "1.2."} {
				if strings.Contains(strings.TrimPrefix(*got.Unreadable, testCase.prefix), fragment) {
					t.Errorf("the message echoes what the parser read: %q", *got.Unreadable)
				}
			}
		})
	}
}

func TestAReaderThatPanicsCannotBreakTheStatusRequest(t *testing.T) {
	// The guarantee matters more than any single reader being correct: one
	// unexpected shape must not blank the whole UI.
	home := t.TempDir()
	rt := runtime.New(runtime.WithHome(home), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": home}))
	agent := agentFor(t, "codex")
	writeExisting(t, home, agent.ConfigPath, "model_provider = \"p\"\n")

	original := configReaders[AdapterCodex]
	configReaders[AdapterCodex] = func(string) Detected { panic("unexpected shape") }
	defer func() { configReaders[AdapterCodex] = original }()

	got := DetectAgentConfig(rt, agent)
	if got == nil || got.Unreadable == nil {
		t.Fatalf("detected = %+v, want a reason rather than a panic", got)
	}
	if *got.Unreadable != "配置解析失败" {
		t.Errorf("unreadable = %q", *got.Unreadable)
	}
}

func TestADirectoryWhereAConfigShouldBeReportsNothing(t *testing.T) {
	home := t.TempDir()
	rt := runtime.New(runtime.WithHome(home), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{"HOME": home}))
	agent := agentFor(t, "codex")
	if err := os.MkdirAll(filepath.Join(home, agent.ConfigPath), 0o700); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	if got := DetectAgentConfig(rt, agent); got != nil {
		t.Errorf("detected = %+v, want nil", got)
	}
}

func TestWhatWeWriteIsWhatWeReadBack(t *testing.T) {
	// The round trip is the assertion worth having: it holds both directions to
	// the same contract, so a change to either is caught here.
	manifest := catalog.MustLoad()
	for _, id := range manifest.AutoAgents() {
		agent, _ := manifest.Agent(id)
		t.Run(id, func(t *testing.T) {
			writer, home := newWriter(t, "linux")
			rt := writer.Runtime
			if _, err := writer.Write(agent, Settings{
				AgentID:      id,
				ProviderName: "PPIO",
				BaseURL:      "https://api.ppio.com/openai",
				APIKey:       "K-ROUNDTRIP",
				Model:        "round-trip-model",
			}); err != nil {
				t.Fatalf("write failed: %v", err)
			}
			_ = home

			got := DetectAgentConfig(rt, agent)
			if got == nil {
				t.Fatal("what we wrote was not detected")
			}
			if got.Unreadable != nil {
				t.Fatalf("our own file reads as unreadable: %q", *got.Unreadable)
			}
			if got.BaseURL == "" {
				t.Error("no endpoint read back")
			}
			if id == "aider" {
				// Its config is a script we own outright, with no marker a
				// hand-written equivalent would lack, and the model is a launch
				// argument rather than a field.
				if got.ManagedByOneAgent {
					t.Error("aider cannot be identified as ours")
				}
				return
			}
			if !got.ManagedByOneAgent {
				t.Error("we wrote this file but it does not look managed")
			}
			if got.Model != "round-trip-model" {
				t.Errorf("model = %q", got.Model)
			}
		})
	}
}
