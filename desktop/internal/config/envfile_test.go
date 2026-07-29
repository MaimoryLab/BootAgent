package config

import (
	"os"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
)

func envSettings() Settings {
	return Settings{
		AgentID: "codex",
		BaseURL: "https://api.ppio.com/openai",
		APIKey:  "sk-env-test",
		Model:   "m",
	}
}

func TestClaudeCodeGetsItsNativeVariablesOrItCannotAuthenticate(t *testing.T) {
	// The defect this exists for: Claude Code ignores the credential in its own
	// settings.json and answers "Not logged in" until ANTHROPIC_AUTH_TOKEN is in
	// the environment. OneAgent reported it as configured anyway.
	writer, _ := newWriter(t, "linux")
	agent := agentFor(t, "claude-code")
	settings := envSettings()
	settings.AgentID = "claude-code"

	path, err := writer.WriteAgentEnv("claude-code", agent, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	content := string(raw)
	for _, name := range agent.EnvVars {
		if name == "" {
			continue
		}
		if !strings.Contains(content, name+"=") {
			t.Errorf("the env file does not define %s:\n%s", name, content)
		}
	}
	if len(agent.EnvVars) == 0 {
		t.Fatal("claude-code declares no env_vars; the assertion would be vacuous")
	}
}

func TestEveryAgentNeedingAnEnvFileGetsItsDeclaredVariables(t *testing.T) {
	// Derived from the manifest so an Agent added later is covered without this
	// test being edited -- which is the point of declaring it there.
	manifest := catalog.MustLoad()
	for _, id := range manifest.AutoAgents() {
		agent, _ := manifest.Agent(id)
		if !NeedsEnvFile(agent) {
			continue
		}
		t.Run(id, func(t *testing.T) {
			writer, _ := newWriter(t, "linux")
			settings := envSettings()
			settings.AgentID = id
			path, err := writer.WriteAgentEnv(id, agent, settings)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("cannot read: %v", err)
			}
			content := string(raw)
			// Its own ONEAGENT_* names, whatever the delivery.
			if !strings.Contains(content, AgentEnvVar(id, "API_KEY")+"=") {
				t.Errorf("missing the per-Agent key variable:\n%s", content)
			}
			// And the shared names, for configs written before per-Agent ones.
			if !strings.Contains(content, "ONEAGENT_API_KEY=") {
				t.Errorf("missing the shared key variable:\n%s", content)
			}
			for field, name := range agent.EnvVars {
				if name == "" || field == "small_fast_model" {
					continue
				}
				if !strings.Contains(content, name+"=") {
					t.Errorf("missing declared variable %s (%s):\n%s", name, field, content)
				}
			}
		})
	}
}

func TestAnAgentWhoseCredentialLivesInItsConfigGetsNoEnvFile(t *testing.T) {
	// Aider's key is in the script the adapter writes. A second file defining it
	// would be a redundant copy of a credential.
	manifest := catalog.MustLoad()
	for _, id := range manifest.AutoAgents() {
		agent, _ := manifest.Agent(id)
		if agent.CredentialDelivery == DeliveryConfigFile && NeedsEnvFile(agent) {
			t.Errorf("%s keeps its credential in its config but is also given an env file", id)
		}
	}
}

func TestTheSharedFileIsStillWrittenForConfigsThatPredatePerAgentNames(t *testing.T) {
	writer, _ := newWriter(t, "linux")
	path, err := writer.WriteSharedEnv("sk-shared", "https://x.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "ONEAGENT_API_KEY=") || !strings.Contains(content, "ONEAGENT_API_BASE_URL=") {
		t.Errorf("content = %q, want both shared variables", content)
	}
}

func TestAnEnvFileIsWrittenOnTheSecretPath(t *testing.T) {
	// It holds the key in plain text, so the backup must be hardened and the file
	// unreadable to anyone else.
	writer, _ := newWriter(t, "linux")
	path, err := writer.WriteSharedEnv("sk-shared", "https://x.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cannot stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %04o, want 0600", mode)
	}
}

func TestWindowsEnvFilesUsePowerShellAssignments(t *testing.T) {
	writer, _ := newWriter(t, "windows")
	path, err := writer.WriteSharedEnv("sk-shared", "https://x.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	if !strings.Contains(string(raw), "$env:ONEAGENT_API_KEY = ") {
		t.Errorf("content = %q, want PowerShell assignments", raw)
	}
	if !strings.HasSuffix(path, ".ps1") {
		t.Errorf("path = %q, want a .ps1 extension", path)
	}
}

func TestASeparateSmallFastModelIsHonouredAndOtherwiseFollowsTheModel(t *testing.T) {
	agent := agentFor(t, "claude-code")
	name := agent.EnvVars["small_fast_model"]
	if name == "" {
		t.Skip("claude-code declares no small_fast_model variable")
	}

	writer, _ := newWriter(t, "linux")
	settings := envSettings()
	settings.AgentID = "claude-code"
	settings.Model = "big"
	settings.SmallFastModel = "small"
	path, err := writer.WriteAgentEnv("claude-code", agent, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), name+"=small") {
		t.Errorf("content = %q, want the explicit small model", raw)
	}

	writer2, _ := newWriter(t, "linux")
	settings.SmallFastModel = ""
	path2, err := writer2.WriteAgentEnv("claude-code", agent, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw2, _ := os.ReadFile(path2)
	if !strings.Contains(string(raw2), name+"=big") {
		t.Errorf("content = %q, want the small model to follow the main one", raw2)
	}
}

func TestAnAbsentValueDoesNotProduceAnEmptyAssignment(t *testing.T) {
	// A variable set to the empty string is not the same as unset: an Agent
	// reading it would see a configured-but-blank endpoint rather than falling
	// back to its own default.
	writer, _ := newWriter(t, "linux")
	agent := agentFor(t, "claude-code")
	settings := Settings{AgentID: "claude-code", APIKey: "sk-x", BaseURL: "https://x.test"}
	// No model, so the model variables must be absent rather than empty.
	path, err := writer.WriteAgentEnv("claude-code", agent, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if name := agent.EnvVars["model"]; name != "" && strings.Contains(string(raw), name+"=''") {
		t.Errorf("content = %q, want no empty model assignment", raw)
	}
}

func TestAnEscapingAgentIdCannotPlaceTheKeyOutsideThePrivateDirectory(t *testing.T) {
	writer, _ := newWriter(t, "linux")
	if _, err := writer.WriteAgentEnv("../escape", catalog.Agent{}, envSettings()); err == nil {
		t.Fatal("a traversing id must be refused before anything is written")
	} else {
		assertCode(t, err, "INVALID_REQUEST")
	}
}

func TestTheKeyIsQuotedSoAHostileValueCannotBecomeShell(t *testing.T) {
	// The file is sourced. A key ending its own quoting would let the remainder
	// run as commands.
	writer, _ := newWriter(t, "linux")
	settings := envSettings()
	settings.APIKey = "sk-'; touch /tmp/oneagent-should-not-exist; '"
	path, err := writer.WriteSharedEnv(settings.APIKey, settings.BaseURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "touch /tmp/oneagent-should-not-exist;\n") {
		t.Errorf("the value escaped its quoting:\n%s", raw)
	}
	// Proven properly by shellquote's own tests against a real shell; this
	// checks the value reached them at all.
	if !strings.Contains(string(raw), `'"'"'`) {
		t.Errorf("content = %q, want the embedded quote escaped", raw)
	}
}
