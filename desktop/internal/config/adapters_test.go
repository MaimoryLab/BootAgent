package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/securefs"
)

// newWriter builds a Writer for the given platform. In Windows mode it supplies
// a fake icacls, because hardening is a hard requirement there -- a write that
// cannot set the ACL fails rather than proceeding, which is the intended
// behaviour and would otherwise make every Windows test fail for that reason
// instead of the one it is checking.
func newWriter(t *testing.T, osID string) (*Writer, string) {
	t.Helper()
	home := t.TempDir()
	options := []runtime.Option{
		runtime.WithHome(home),
		runtime.WithOSID(osID),
		runtime.WithEnv(map[string]string{"HOME": home, "USERNAME": "tester"}),
	}
	if osID == "windows" {
		options = append(options,
			runtime.WithLookup(func(name string) (string, bool) {
				return `C:\Windows\System32\icacls.exe`, name == "icacls"
			}),
			runtime.WithRunner(func(_ context.Context, _ []string, _ runtime.RunOptions) (runtime.Result, error) {
				return runtime.Result{}, nil
			}),
		)
	}
	rt := runtime.New(options...)
	return &Writer{Runtime: rt, FS: securefs.New(rt)}, home
}

func agentFor(t *testing.T, id string) catalog.Agent {
	t.Helper()
	agent, present := catalog.MustLoad().Agent(id)
	if !present {
		t.Fatalf("%s is not in the manifest", id)
	}
	return agent
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	var oneAgentErr *oerr.Error
	if !errors.As(err, &oneAgentErr) {
		t.Fatalf("err = %v, want an *oerr.Error", err)
	}
	if oneAgentErr.Code != want {
		t.Errorf("code = %q, want %q", oneAgentErr.Code, want)
	}
}

func writeExisting(t *testing.T, home, relative, content string) string {
	t.Helper()
	full := filepath.Join(home, relative)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	return full
}

var settings = Settings{
	AgentID:      "codex",
	ProviderName: "PPIO",
	BaseURL:      "https://api.ppio.com/openai",
	APIKey:       "sk-test",
	Model:        "m",
}

func TestEveryAutoAgentHasAnAdapterThatCanWriteIt(t *testing.T) {
	// The lock may name an adapter before an implementation exists. Reporting
	// such an Agent as configured is the failure this prevents.
	supported := map[string]bool{}
	for _, adapter := range SupportedAdapters() {
		supported[adapter] = true
	}
	manifest := catalog.MustLoad()
	for _, id := range manifest.AutoAgents() {
		agent, _ := manifest.Agent(id)
		if !supported[agent.ConfigAdapter] {
			t.Errorf("%s declares adapter %q, which nothing can write", id, agent.ConfigAdapter)
		}
	}
}

func TestAnUnknownAdapterIsRefusedRatherThanSilentlySkipped(t *testing.T) {
	writer, _ := newWriter(t, "linux")
	agent := agentFor(t, "codex")
	agent.ConfigAdapter = "some-future-format"
	if _, err := writer.Write(agent, settings); err == nil {
		t.Fatal("an unimplemented adapter must be refused")
	} else {
		assertCode(t, err, "INVALID_REQUEST")
	}
}

func TestDispatchIsKeyedOnTheAdapterNotTheAgentId(t *testing.T) {
	// Two Agents share the OpenAI-compatible format, and an Agent added with an
	// existing format must need no code change. Proven by giving codex's entry
	// the opencode adapter and checking it takes that path.
	writer, home := newWriter(t, "linux")
	agent := agentFor(t, "codex")
	agent.ConfigAdapter = AdapterOpenCode

	path, err := writer.Write(agent, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	if !strings.Contains(string(raw), "opencode.ai/config.json") {
		t.Errorf("the opencode adapter did not run; file was:\n%s", raw)
	}
	_ = home
}

func TestTheTwoOpenAICompatibleAgentsGetDifferentSchemas(t *testing.T) {
	// One adapter, two schemas. Deriving the schema from the adapter name would
	// point Kilo at OpenCode's.
	for id, want := range map[string]string{
		"opencode": "https://opencode.ai/config.json",
		"kilo-cli": "https://app.kilo.ai/config.json",
	} {
		writer, _ := newWriter(t, "linux")
		path, err := writer.Write(agentFor(t, id), Settings{AgentID: id, ProviderName: "P", BaseURL: "https://x.test", Model: "m"})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", id, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read: %v", err)
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s: schema %q not found in:\n%s", id, want, raw)
		}
	}
}

func TestInvalidExistingTOMLIsRefusedRatherThanOverwritten(t *testing.T) {
	// The user has something there. Replacing a file we cannot parse would
	// discard content without knowing what it was.
	writer, home := newWriter(t, "linux")
	agent := agentFor(t, "codex")
	original := "model_provider = \n[[[ broken"
	writeExisting(t, home, agent.ConfigPath, original)

	if _, err := writer.Write(agent, settings); err == nil {
		t.Fatal("invalid TOML must be refused")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}
	raw, err := os.ReadFile(filepath.Join(home, agent.ConfigPath))
	if err != nil {
		t.Fatalf("the file is gone: %v", err)
	}
	if string(raw) != original {
		t.Errorf("the unparseable file was modified: %q", raw)
	}
}

func TestAJSONCFileWithCommentsIsRefusedByNameRatherThanAsBrokenJSON(t *testing.T) {
	// Refusing is right -- the rewrite cannot preserve comments -- but telling
	// the user their valid JSONC is invalid JSON leaves them with nothing to act
	// on.
	writer, home := newWriter(t, "linux")
	agent := agentFor(t, "opencode")
	if !strings.HasSuffix(agent.ConfigPath, ".jsonc") {
		t.Skipf("opencode's config is %s, not .jsonc", agent.ConfigPath)
	}
	writeExisting(t, home, agent.ConfigPath, "{\n  // my note\n  \"theme\": \"dark\"\n}\n")

	_, err := writer.Write(agent, Settings{AgentID: "opencode", ProviderName: "P", BaseURL: "https://x.test", Model: "m"})
	if err == nil {
		t.Fatal("a JSONC file with comments must be refused")
	}
	assertCode(t, err, "CONFIG_WRITE_FAILED")
	if !strings.Contains(err.Error(), "JSONC") {
		t.Errorf("message = %q, want it to name JSONC comments", err.Error())
	}
}

func TestBrokenJSONWithoutCommentsIsReportedAsInvalidJSON(t *testing.T) {
	writer, home := newWriter(t, "linux")
	agent := agentFor(t, "opencode")
	writeExisting(t, home, agent.ConfigPath, "{ not json")

	_, err := writer.Write(agent, Settings{AgentID: "opencode", ProviderName: "P", BaseURL: "https://x.test", Model: "m"})
	if err == nil {
		t.Fatal("broken JSON must be refused")
	}
	if strings.Contains(err.Error(), "JSONC") {
		t.Errorf("message = %q, want it not to blame comments that are not there", err.Error())
	}
}

func TestAnEnvKeyHoldingSomethingElseIsRefused(t *testing.T) {
	// Replacing it with an object would lose whatever the user had.
	writer, home := newWriter(t, "linux")
	agent := agentFor(t, "claude-code")
	writeExisting(t, home, agent.ConfigPath, `{"env":"not-an-object"}`)

	if _, err := writer.Write(agent, Settings{AgentID: "claude-code", BaseURL: "https://x.test", APIKey: "k", Model: "m"}); err == nil {
		t.Fatal("a non-object env must be refused")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}
}

func TestAProviderKeyHoldingSomethingElseIsRefused(t *testing.T) {
	writer, home := newWriter(t, "linux")
	agent := agentFor(t, "opencode")
	writeExisting(t, home, agent.ConfigPath, `{"provider":[]}`)

	if _, err := writer.Write(agent, Settings{AgentID: "opencode", ProviderName: "P", BaseURL: "https://x.test", Model: "m"}); err == nil {
		t.Fatal("a non-object provider must be refused")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}
}

func TestAnEmptyOrWhitespaceOnlyFileIsTreatedAsAbsent(t *testing.T) {
	for _, existing := range []string{"", "   \n\t\n"} {
		writer, home := newWriter(t, "linux")
		agent := agentFor(t, "claude-code")
		writeExisting(t, home, agent.ConfigPath, existing)
		if _, err := writer.Write(agent, Settings{AgentID: "claude-code", BaseURL: "https://x.test", APIKey: "k", Model: "m"}); err != nil {
			t.Errorf("existing=%q: unexpected error %v", existing, err)
		}
	}
}

func TestTheCredentialReachesOnlyTheFilesThatAreMeantToHoldIt(t *testing.T) {
	// Codex and the OpenAI-compatible formats reference the key by variable
	// name. Writing it into them would put a plaintext credential in a file
	// OneAgent does not harden as a secret.
	const secret = "sk-must-not-appear"
	for _, id := range []string{"codex", "opencode", "kilo-cli"} {
		writer, _ := newWriter(t, "linux")
		path, err := writer.Write(agentFor(t, id), Settings{
			AgentID: id, ProviderName: "P", BaseURL: "https://x.test", APIKey: secret, Model: "m",
		})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", id, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read: %v", err)
		}
		if strings.Contains(string(raw), secret) {
			t.Errorf("%s wrote the credential into %s", id, path)
		}
		if !strings.Contains(string(raw), AgentEnvVar(id, "API_KEY")) {
			t.Errorf("%s does not reference its credential variable", id)
		}
	}
}

func TestTheAdaptersThatMustHoldTheKeyDoHoldIt(t *testing.T) {
	// The counterpart: Claude Code and Aider have nowhere else to put it, and a
	// config that silently lacks the key reports success and cannot
	// authenticate.
	const secret = "sk-expected-here"
	for _, id := range []string{"claude-code", "aider"} {
		writer, _ := newWriter(t, "linux")
		path, err := writer.Write(agentFor(t, id), Settings{
			AgentID: id, ProviderName: "P", BaseURL: "https://x.test", APIKey: secret, Model: "m",
		})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", id, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read: %v", err)
		}
		if !strings.Contains(string(raw), secret) {
			t.Errorf("%s did not write the credential it needs", id)
		}
	}
}

func TestAFileHoldingTheKeyIsWrittenOnTheSecretPath(t *testing.T) {
	// The secret path hardens the backup too, and deletes it rather than leaving
	// a readable copy. A config carrying a credential must take it.
	for _, id := range []string{"claude-code", "aider"} {
		writer, home := newWriter(t, "linux")
		agent := agentFor(t, id)
		// An existing file so a backup is taken.
		writeExisting(t, home, agent.ConfigPath, "{}")
		if _, err := writer.Write(agent, Settings{AgentID: id, BaseURL: "https://x.test", APIKey: "sk-x", Model: "m"}); err != nil {
			t.Fatalf("%s: unexpected error: %v", id, err)
		}
		entries, err := os.ReadDir(filepath.Dir(filepath.Join(home, agent.ConfigPath)))
		if err != nil {
			t.Fatalf("cannot read: %v", err)
		}
		for _, entry := range entries {
			if !strings.Contains(entry.Name(), ".backup-") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				t.Fatalf("cannot stat: %v", err)
			}
			if mode := info.Mode().Perm(); mode != 0o600 {
				t.Errorf("%s: backup has mode %04o, want 0600", id, mode)
			}
		}
	}
}

func TestAiderUsesPowerShellSyntaxOnWindows(t *testing.T) {
	// The file is sourced by a shell, so the wrong syntax makes it unusable
	// rather than merely unusual.
	writer, _ := newWriter(t, "windows")
	path, err := writer.Write(agentFor(t, "aider"), Settings{
		AgentID: "aider", BaseURL: "https://x.test", APIKey: "sk-x", Model: "m",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	if !strings.Contains(string(raw), "$env:OPENAI_API_KEY") {
		t.Errorf("content = %q, want PowerShell assignments", raw)
	}
	if strings.Contains(string(raw), "export ") {
		t.Errorf("content = %q, want no POSIX exports", raw)
	}
}

func TestAiderHonoursTheWindowsConfigPathFromTheManifest(t *testing.T) {
	// Aider is the one Agent declaring a Windows-specific location. Ignoring it
	// would write to the POSIX path, where nothing looks.
	agent := agentFor(t, "aider")
	if agent.WindowsConfigPath == "" {
		t.Skip("aider no longer declares a Windows config path")
	}
	writer, home := newWriter(t, "windows")
	path, err := writer.Write(agent, Settings{AgentID: "aider", BaseURL: "https://x.test", APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, filepath.FromSlash(agent.WindowsConfigPath))
	if path != want {
		t.Fatalf("wrote %s, want %s", path, want)
	}
}

func TestUserFieldsSurviveEveryJSONAdapter(t *testing.T) {
	// A product promise: OneAgent manages its own keys and must not discard the
	// rest. Stated per adapter so a failure names which one dropped them.
	for id, existing := range map[string]string{
		"claude-code": `{"theme":"dark","permissions":{"allow":["Bash(ls)"]},"env":{"KEEP":"yes"}}`,
		"opencode":    `{"theme":"dark","provider":{"mine":{"npm":"x"}},"keep":true}`,
		"kilo-cli":    `{"theme":"dark","keep":[1,2,3]}`,
	} {
		writer, home := newWriter(t, "linux")
		agent := agentFor(t, id)
		writeExisting(t, home, agent.ConfigPath, existing)
		path, err := writer.Write(agent, Settings{AgentID: id, ProviderName: "P", BaseURL: "https://x.test", APIKey: "k", Model: "m"})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", id, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read: %v", err)
		}
		for _, fragment := range []string{`"theme": "dark"`} {
			if !strings.Contains(string(raw), fragment) {
				t.Errorf("%s dropped %s:\n%s", id, fragment, raw)
			}
		}
	}
}

func TestUserTablesAndCommentsSurviveTheCodexMerge(t *testing.T) {
	writer, home := newWriter(t, "linux")
	agent := agentFor(t, "codex")
	writeExisting(t, home, agent.ConfigPath,
		"# my notes\nmodel = \"old\"\n\n[tui]\ntheme = \"dark\"  # inline comment\n")

	path, err := writer.Write(agent, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	content := string(raw)
	for _, fragment := range []string{"# my notes", "[tui]", `theme = "dark"`, "# inline comment"} {
		if !strings.Contains(content, fragment) {
			t.Errorf("the merge dropped %q:\n%s", fragment, content)
		}
	}
	// And the key OneAgent owns was replaced rather than duplicated.
	if strings.Count(content, "model_provider =") != 1 {
		t.Errorf("model_provider appears %d times:\n%s", strings.Count(content, "model_provider ="), content)
	}
	if strings.Contains(content, `model = "old"`) {
		t.Errorf("the old model survived:\n%s", content)
	}
}
