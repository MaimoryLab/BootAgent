package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
)

func TestActivatingOneAgentLeavesEveryOtherAgentAlone(t *testing.T) {
	// This is what makes repointing safe: only one Agent's config and env file
	// change, so a failure cannot leave two Agents disagreeing and there is no
	// cross-file rollback to get right.
	home := t.TempDir()
	service := serviceFor(t, home, true, nil)

	first, err := service.Install(InstallOptions{
		Agents: []string{"codex", "opencode"}, ProfileAgents: []string{"codex", "opencode"},
		Provider: "ppio", APIKey: "sk-first", Model: "model-one",
		Configure: true, SkipTest: true, Timeout: 30 * time.Second,
	})
	if err != nil || !first.OK {
		t.Fatalf("cannot set up: %v %+v", err, first)
	}
	before := read(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc"))

	result, err := service.Activate(ActivateOptions{
		AgentID: "codex", Provider: "novita", APIKey: "sk-second", Model: "model-two",
	})
	if err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	if !result.OK || result.Model != "model-two" || result.Provider != "novita" {
		t.Errorf("result = %+v, want the new provider and model", result)
	}
	if result.Binding == nil {
		t.Fatal("no binding was returned")
	}
	if got := result.Binding.GetString("model"); got != "model-two" {
		t.Errorf("binding model = %q", got)
	}
	// The binding carries no credential, which is why it is safe to return.
	encoded := binding(t, result)
	if strings.Contains(encoded, "sk-second") {
		t.Errorf("the binding carries the key: %s", encoded)
	}

	if after := read(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc")); after != before {
		t.Error("activating codex rewrote opencode's config")
	}
	if !strings.Contains(read(t, filepath.Join(home, ".codex", "config.toml")), "model-two") {
		t.Error("codex's config was not repointed")
	}
	// The restart hint is reported because an Agent reads its config at startup,
	// so a rewrite is invisible to a process that is already running.
	if result.Restart == "" {
		t.Error("no restart hint was given, so a user would think the switch took effect")
	}
}

func TestActivateRefusesWhatItCannotDo(t *testing.T) {
	service := serviceFor(t, t.TempDir(), true, nil)
	cases := map[string]ActivateOptions{
		"unknown agent":    {AgentID: "not-an-agent", Provider: "ppio", APIKey: "sk-a", Model: "m"},
		"illegal agent id": {AgentID: "../escape", Provider: "ppio", APIKey: "sk-a", Model: "m"},
		"guide-only agent": {AgentID: "gemini-cli", Provider: "ppio", APIKey: "sk-a", Model: "m"},
		"no key":           {AgentID: "codex", Provider: "ppio", Model: "m"},
		"unknown provider": {AgentID: "codex", Provider: "nowhere", APIKey: "sk-a", Model: "m"},
		"custom without an endpoint": {
			AgentID: "codex", Provider: "custom", APIKey: "sk-a", Model: "m",
		},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Activate(options); err == nil {
				t.Error("the request was accepted")
			}
		})
	}
}

func TestActivateWritesTheEnvFileBeforeTheConfigThatReferencesIt(t *testing.T) {
	// Claude Code reads its credential from its own variable names, so the config
	// alone leaves it starting unauthenticated. The env file has to exist for the
	// config to be usable at all.
	home := t.TempDir()
	service := serviceFor(t, home, true, nil)
	if _, err := service.Activate(ActivateOptions{
		AgentID: "claude-code", Provider: "ppio", APIKey: "sk-a",
		Model: "claude-sonnet-4", SmallFastModel: "claude-haiku-4",
	}); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	envPath := filepath.Join(home, ".oneagent", "agents", "claude-code.env")
	envText := read(t, envPath)
	if !strings.Contains(envText, "ANTHROPIC_AUTH_TOKEN") {
		t.Errorf("the env file does not define the Agent's own variable: %q", envText)
	}
	if !strings.Contains(envText, "claude-haiku-4") {
		t.Errorf("the small fast model did not reach the env file: %q", envText)
	}
	// And the env file holding the key is private.
	if os.Getuid() != 0 {
		info, err := os.Stat(envPath)
		if err != nil {
			t.Fatalf("cannot stat: %v", err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("the env file is %v, want 0600", mode)
		}
	}
}

func TestActivateResolvesTheModelWhenTheCallerOmitsIt(t *testing.T) {
	// Writing "" into a config produces a file that looks configured and cannot
	// answer a request, so an omitted model is resolved rather than passed through.
	service := serviceFor(t, t.TempDir(), true, &routedTransport{
		modelsBody: `{"data":[{"id":"text-embedding-3"},{"id":"chat-model"}]}`,
	})
	result, err := service.Activate(ActivateOptions{
		AgentID: "codex", Provider: "ppio", APIKey: "sk-a",
	})
	if err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	if result.Model != "chat-model" {
		t.Errorf("model = %q, want the first chat-capable id from discovery", result.Model)
	}
}

func TestTheAnthropicAgentGetsTheAnthropicEndpoint(t *testing.T) {
	// The config write is keyed on the protocol, not the Agent id: the managed
	// providers serve Anthropic Messages on a different route, and writing the
	// OpenAI base into Claude's config produces an Agent that cannot answer.
	home := t.TempDir()
	service := serviceFor(t, home, true, nil)
	if _, err := service.Activate(ActivateOptions{
		AgentID: "claude-code", Provider: "ppio", APIKey: "sk-a", Model: "m",
	}); err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	settings := read(t, filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(settings, "/anthropic") {
		t.Errorf("claude's config does not name the anthropic route: %q", settings)
	}

	if _, err := service.Activate(ActivateOptions{
		AgentID: "codex", Provider: "ppio", APIKey: "sk-a", Model: "m",
	}); err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	if got := read(t, filepath.Join(home, ".codex", "config.toml")); strings.Contains(got, "/anthropic") {
		t.Errorf("codex's config names the anthropic route: %q", got)
	}
}

func TestEveryManagedAgentCanBeActivated(t *testing.T) {
	// Derived from the manifest rather than a list here, so an Agent added later is
	// covered without this test being updated -- which is the same reason the
	// production code reads the manifest.
	manifest := catalog.MustLoad()
	for _, agentID := range manifest.AutoAgents() {
		t.Run(agentID, func(t *testing.T) {
			home := t.TempDir()
			service := serviceFor(t, home, true, nil)
			agent, _ := manifest.Agent(agentID)
			result, err := service.Activate(ActivateOptions{
				AgentID: agentID, Provider: "ppio", APIKey: "sk-a", Model: "m",
			})
			if err != nil {
				t.Fatalf("activate failed: %v", err)
			}
			if result.Config == "" {
				t.Error("no config path was reported")
			}
			if _, err := os.Stat(result.Config); err != nil {
				t.Errorf("the reported config does not exist: %v", err)
			}
			if config.NeedsEnvFile(agent) {
				path, err := config.AgentEnvPath(service.Runtime, agentID)
				if err != nil {
					t.Fatalf("cannot resolve the env path: %v", err)
				}
				if _, err := os.Stat(path); err != nil {
					t.Errorf("the Agent needs an env file and none was written: %v", err)
				}
			}
			if result.Next == "" {
				t.Error("no next step was reported")
			}
		})
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	return string(raw)
}

func binding(t *testing.T, result ActivateResult) string {
	t.Helper()
	encoded, err := result.Binding.MarshalJSON()
	if err != nil {
		t.Fatalf("cannot encode the binding: %v", err)
	}
	return string(encoded)
}
