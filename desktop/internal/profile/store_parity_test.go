package profile

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/testutil"
)

// These files persist across runs and decide where a plaintext key is written, so
// the comparison is on the bytes rather than on a parsed shape: a reordered key or
// a rewritten timestamp is a file the other implementation did not produce, and
// both implementations have to keep reading each other's output during migration.

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

// runPythonScript executes a snippet against a real HOME and returns its stdout.
func runPythonScript(t *testing.T, script, home string, payload any) string {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	cmd := exec.Command(pythonBin(t), "-c", script, repoRoot(t), home, string(encoded))
	cmd.Dir = repoRoot(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python failed: %v\n%s", err, output)
	}
	return string(output)
}

// fixedNow pins the timestamps so the only differences left are structural. The
// format itself is compared separately, because pinning it here would hide a
// disagreement about how the time is rendered.
func fixedStore(t *testing.T, home string) *Store {
	t.Helper()
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{"HOME": home}),
		runtime.WithRunner(testutil.NewRecordingRunner(t).Runner()),
	)
	return NewStore(rt)
}

func TestParityASavedProfileIsByteIdenticalToPython(t *testing.T) {
	cases := []struct {
		name    string
		request SaveRequest
	}{
		{"managed provider", SaveRequest{
			ID: "team-ppio", Provider: "ppio", Model: "gpt-5-mini",
			AgentIDs: []string{"codex", "claude-code"}, APIKey: "sk-secret",
		}},
		{"custom endpoint", SaveRequest{
			ID: "local", Label: "Local vLLM", Provider: "custom",
			APIBaseURL: "https://vllm.internal/v1", Model: "qwen3",
			AgentIDs: []string{"opencode"}, APIKey: "sk-local",
		}},
		{"no key", SaveRequest{
			ID: "keyless", Provider: "novita", Model: "m", AgentIDs: []string{"codex"},
		}},
		{"duplicate agents", SaveRequest{
			ID: "dupes", Provider: "ppio", Model: "m",
			AgentIDs: []string{"codex", "codex", "aider"},
		}},
		{"no agents", SaveRequest{
			ID: "empty", Provider: "ppio", Model: "m", AgentIDs: []string{},
		}},
		{"non-ascii label", SaveRequest{
			ID: "cn", Label: "团队配置", Provider: "ppio", Model: "通义",
			AgentIDs: []string{"codex"}, APIKey: "sk-cn",
		}},
	}

	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from pathlib import Path
from oneagent.installer import Runtime, save_profile
import subprocess

home = Path(sys.argv[2])
request = json.loads(sys.argv[3])
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
save_profile(
    runtime,
    profile_id=request["id"],
    label=request.get("label", ""),
    provider=request["provider"],
    api_base_url=request.get("api_base_url", ""),
    model=request["model"],
    agent_ids=request["agent_ids"],
    api_key=request.get("api_key", ""),
)
`
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			pythonHome := t.TempDir()
			runPythonScript(t, script, pythonHome, map[string]any{
				"id": testCase.request.ID, "label": testCase.request.Label,
				"provider": testCase.request.Provider, "api_base_url": testCase.request.APIBaseURL,
				"model": testCase.request.Model, "agent_ids": testCase.request.AgentIDs,
				"api_key": testCase.request.APIKey,
			})

			goHome := t.TempDir()
			store := fixedStore(t, goHome)
			if _, err := store.Save(testCase.request); err != nil {
				t.Fatalf("Go refused a request Python accepted: %v", err)
			}

			compareTree(t, pythonHome, goHome)
		})
	}
}

func TestParityAnActivatedProfileIsByteIdenticalToPython(t *testing.T) {
	cases := []struct {
		name    string
		request ActivateRequest
	}{
		{"configured", ActivateRequest{
			AgentIDs: []string{"codex"}, Configure: true, Provider: "ppio",
			BaseURL: "https://api.ppio.com/openai/v1", Model: "gpt-5-mini", APIKey: "sk-a",
		}},
		{"existing account", ActivateRequest{
			AgentIDs: []string{"codex", "aider"}, Configure: false,
		}},
		{"several agents", ActivateRequest{
			AgentIDs: []string{"kilo-cli", "codex", "opencode"}, Configure: true,
			Provider: "novita", BaseURL: "https://api.novita.ai/openai/v1", Model: "m", APIKey: "sk-b",
		}},
		{"no key configured", ActivateRequest{
			AgentIDs: []string{"codex"}, Configure: true, Provider: "ppio",
			BaseURL: "https://api.ppio.com/openai/v1", Model: "m",
		}},
	}

	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from pathlib import Path
from oneagent.installer import Runtime, write_profile

home = Path(sys.argv[2])
request = json.loads(sys.argv[3])
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
write_profile(
    runtime,
    agents=request["agents"],
    configure=request["configure"],
    provider=request["provider"],
    base_url=request["base_url"],
    model=request["model"],
    api_key=request.get("api_key", ""),
)
`
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			pythonHome := t.TempDir()
			runPythonScript(t, script, pythonHome, map[string]any{
				"agents": testCase.request.AgentIDs, "configure": testCase.request.Configure,
				"provider": testCase.request.Provider, "base_url": testCase.request.BaseURL,
				"model": testCase.request.Model, "api_key": testCase.request.APIKey,
			})

			goHome := t.TempDir()
			store := fixedStore(t, goHome)
			if _, err := store.Activate(testCase.request); err != nil {
				t.Fatalf("Go refused a request Python accepted: %v", err)
			}

			compareTree(t, pythonHome, goHome)
		})
	}
}

func TestParityAnAgentBindingIsByteIdenticalToPython(t *testing.T) {
	cases := []struct {
		name                                       string
		agentID, provider, baseURL, model, profile string
	}{
		{"codex", "codex", "PPIO", "https://api.ppio.com/openai/v1", "gpt-5-mini", ""},
		{"with profile ref", "claude-code", "PPIO", "https://api.ppio.com/anthropic", "claude-sonnet-4", "team"},
		{"non-ascii model", "opencode", "Custom", "https://vendor.example/v1", "通义千问", ""},
		{"empty model", "aider", "PPIO", "https://api.ppio.com/openai/v1", "", ""},
	}

	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from pathlib import Path
from oneagent.installer import Runtime, write_agent_binding

home = Path(sys.argv[2])
request = json.loads(sys.argv[3])
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
write_agent_binding(
    runtime,
    request["agent_id"],
    provider=request["provider"],
    base_url=request["base_url"],
    model=request["model"],
    profile_ref=request["profile_ref"],
)
`
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			pythonHome := t.TempDir()
			runPythonScript(t, script, pythonHome, map[string]any{
				"agent_id": testCase.agentID, "provider": testCase.provider,
				"base_url": testCase.baseURL, "model": testCase.model,
				"profile_ref": testCase.profile,
			})

			goHome := t.TempDir()
			store := fixedStore(t, goHome)
			if _, err := store.WriteBinding(
				testCase.agentID, testCase.provider, testCase.baseURL, testCase.model, testCase.profile,
			); err != nil {
				t.Fatalf("Go refused what Python accepted: %v", err)
			}

			compareTree(t, pythonHome, goHome)
		})
	}
}
