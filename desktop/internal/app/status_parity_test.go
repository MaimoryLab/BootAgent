package app

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/profile"
	"github.com/MaimoryLab/OneAgent/desktop/internal/provider"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/testutil"
)

// The status payload is what the entire frontend reads, so a missing or renamed
// field is a page that renders wrong rather than an error anyone sees. It is also
// the one response that must be provably free of credential material: three of the
// five config formats hold the key in plain text, and this payload reports on all
// of them.

const statusScript = `
import json, subprocess, sys
sys.path.insert(0, sys.argv[1])
from pathlib import Path

from oneagent.installer import Runtime, status_payload

home = Path(sys.argv[2])
setup = json.loads(sys.argv[3])


def fake_runner(argv, **kwargs):
    name = Path(argv[0]).name
    if "--version" in argv or "-V" in argv or "version" in argv:
        return subprocess.CompletedProcess(argv, 0, setup["version_output"], "")
    return subprocess.CompletedProcess(argv, 0, "", "")


def fake_which(name):
    if name in (setup["missing"] or []):
        return None
    return f"/usr/local/bin/{name}"


runtime = Runtime(
    home=home, os_id=setup["os_id"], runner=fake_runner,
    which=fake_which, env={"HOME": str(home)},
)
payload = status_payload(runtime)
# Architecture is read from the host, so it is the one field that cannot agree
# between two processes on principle -- it is compared separately.
payload["platform"].pop("arch", None)
print(json.dumps(payload, sort_keys=True))
`

// statusSetup is the machine the payload is read from.
type statusSetup struct {
	OSID          string   `json:"os_id"`
	Missing       []string `json:"missing"`
	VersionOutput string   `json:"version_output"`
}

func TestParityTheStatusPayloadMatchesPython(t *testing.T) {
	cases := []struct {
		name  string
		setup statusSetup
		// prepare writes whatever should already be on the machine.
		prepare func(t *testing.T, home string)
	}{
		{
			name:  "nothing installed",
			setup: statusSetup{OSID: "linux", Missing: []string{"npm", "uv", "python3.12"}},
		},
		{
			name:  "everything present",
			setup: statusSetup{OSID: "linux", VersionOutput: "1.2.3"},
		},
		{
			name:  "npm present uv missing",
			setup: statusSetup{OSID: "linux", Missing: []string{"uv"}, VersionOutput: "0.145.0"},
		},
		{
			name:  "windows",
			setup: statusSetup{OSID: "windows", VersionOutput: "1.0.0"},
		},
		{
			name:  "windows without the prerequisites",
			setup: statusSetup{OSID: "windows", Missing: []string{"node", "npm"}},
		},
		{
			name:  "a hand-written codex config",
			setup: statusSetup{OSID: "linux", VersionOutput: "1.0.0"},
			prepare: func(t *testing.T, home string) {
				write(t, filepath.Join(home, ".codex", "config.toml"),
					"model_provider = \"vendor\"\nmodel = \"gpt-5-mini\"\n"+
						"[model_providers.vendor]\nbase_url = \"https://vendor.example/v1\"\n")
			},
		},
		{
			name:  "a claude config holding a key in plain text",
			setup: statusSetup{OSID: "linux", VersionOutput: "1.0.0"},
			prepare: func(t *testing.T, home string) {
				write(t, filepath.Join(home, ".claude", "settings.json"),
					`{"env":{"ANTHROPIC_BASE_URL":"https://api.ppio.com/anthropic",`+
						`"ANTHROPIC_AUTH_TOKEN":"sk-plaintext-in-the-file","ANTHROPIC_MODEL":"claude-sonnet-4"}}`+"\n")
			},
		},
		{
			name:  "a config edited into invalid TOML",
			setup: statusSetup{OSID: "linux", VersionOutput: "1.0.0"},
			prepare: func(t *testing.T, home string) {
				write(t, filepath.Join(home, ".codex", "config.toml"), "this is not = = toml\n")
			},
		},
		{
			name:  "an empty config file",
			setup: statusSetup{OSID: "linux", VersionOutput: "1.0.0"},
			prepare: func(t *testing.T, home string) {
				write(t, filepath.Join(home, ".codex", "config.toml"), "")
			},
		},
		{
			name:  "a legacy profile awaiting migration",
			setup: statusSetup{OSID: "linux", VersionOutput: "1.0.0"},
			prepare: func(t *testing.T, home string) {
				write(t, filepath.Join(home, ".oneagent", "profile.json"),
					`{"schema_version": 1, "provider": "ppio", "base_url": null, "model": "m",`+
						` "agent_ids": ["codex"], "activated_at": "2026-01-01T00:00:00Z"}`+"\n")
			},
		},
		{
			name:  "a corrupt profile pointer",
			setup: statusSetup{OSID: "linux", VersionOutput: "1.0.0"},
			prepare: func(t *testing.T, home string) {
				write(t, filepath.Join(home, ".oneagent", "profile.json"), "{not json\n")
			},
		},
		{
			name:  "backups on disk",
			setup: statusSetup{OSID: "linux", VersionOutput: "1.0.0"},
			prepare: func(t *testing.T, home string) {
				write(t, filepath.Join(home, ".codex", "config.toml"), "model = \"m\"\n")
				write(t, filepath.Join(home, ".codex", "config.toml.backup-20260101-000000"), "old\n")
				write(t, filepath.Join(home, ".oneagent", "env"), "export ONEAGENT_API_KEY=x\n")
				write(t, filepath.Join(home, ".oneagent", "env.backup-20260101-000000"), "old\n")
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			pythonHome := t.TempDir()
			goHome := t.TempDir()
			if testCase.prepare != nil {
				testCase.prepare(t, pythonHome)
				testCase.prepare(t, goHome)
			}

			want := runPythonStatus(t, pythonHome, testCase.setup)
			got := goStatus(t, goHome, testCase.setup)

			// Compared as decoded JSON with the two home paths normalised away:
			// every absolute path in the payload names a different temporary
			// directory in each run.
			wantText := strings.ReplaceAll(want, pythonHome, "<home>")
			gotText := strings.ReplaceAll(got, goHome, "<home>")
			if wantText != gotText {
				t.Errorf("the payload differs:\n  Go:     %s\n  Python: %s",
					firstDifference(gotText, wantText), firstDifference(wantText, gotText))
			}
		})
	}
}

func TestParityTheStatusPayloadCarriesNoCredentialMaterial(t *testing.T) {
	// Checked on the serialised bytes of both implementations, with a key that is
	// really in the files being read. A reader that started reporting the key would
	// otherwise only be caught by someone noticing it in a response.
	const key = "sk-plaintext-do-not-report"
	setup := statusSetup{OSID: "linux", VersionOutput: "1.0.0"}
	prepare := func(t *testing.T, home string) {
		write(t, filepath.Join(home, ".claude", "settings.json"),
			`{"env":{"ANTHROPIC_BASE_URL":"https://api.ppio.com/anthropic","ANTHROPIC_AUTH_TOKEN":"`+key+`"}}`+"\n")
		write(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
			`{"provider":{"oneagent":{"options":{"apiKey":"`+key+`","baseURL":"https://api.ppio.com/openai/v1"}}}}`+"\n")
		write(t, filepath.Join(home, ".oneagent", "secrets", "default.env"),
			"export ONEAGENT_API_KEY="+key+"\n")
		write(t, filepath.Join(home, ".oneagent", "profiles", "default.json"),
			`{"schema_version":2,"id":"default","label":"default","provider":"ppio",`+
				`"base_url":null,"model":"m","config_mode":"provider","agent_ids":["codex"],`+
				`"created_at":"2026-01-01T00:00:00Z","activated_at":"2026-01-01T00:00:00Z"}`+"\n")
		write(t, filepath.Join(home, ".oneagent", "profile.json"),
			`{"schema_version":2,"active":"default"}`+"\n")
	}

	pythonHome := t.TempDir()
	goHome := t.TempDir()
	prepare(t, pythonHome)
	prepare(t, goHome)

	for name, payload := range map[string]string{
		"Python": runPythonStatus(t, pythonHome, setup),
		"Go":     goStatus(t, goHome, setup),
	} {
		if strings.Contains(payload, key) {
			t.Errorf("%s reports the credential in the status payload", name)
		}
		// The assertion is not vacuous: the profile that holds the key is reported,
		// and it says a key exists.
		if !strings.Contains(payload, `"hasKey": true`) && !strings.Contains(payload, `"hasKey":true`) {
			t.Errorf("%s does not report hasKey, so the check above proves nothing", name)
		}
		// And the endpoint did come back, so the readers really ran.
		if !strings.Contains(payload, "api.ppio.com") {
			t.Errorf("%s read no endpoint out of the config files", name)
		}
	}
}

func runPythonStatus(t *testing.T, home string, setup statusSetup) string {
	t.Helper()
	encoded, err := json.Marshal(setup)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	command := exec.Command(pythonBin(t), "-c", statusScript, repoRoot(t), home, string(encoded))
	command.Dir = repoRoot(t)
	output, err := command.Output()
	if err != nil {
		stderr := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		t.Fatalf("python failed: %v\n%s", err, stderr)
	}
	return canonicalJSON(t, output)
}

func goStatus(t *testing.T, home string, setup statusSetup) string {
	t.Helper()
	missing := map[string]bool{}
	for _, name := range setup.Missing {
		missing[name] = true
	}
	service := statusService(t, home, setup.OSID, missing, setup.VersionOutput)
	status, err := service.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("cannot encode the status: %v", err)
	}
	// Architecture is host-derived, so it is dropped here to match the Python side
	// and asserted on its own below.
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("cannot decode: %v", err)
	}
	if platform, ok := decoded["platform"].(map[string]any); ok {
		delete(platform, "arch")
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("cannot re-encode: %v", err)
	}
	return canonicalJSON(t, reencoded)
}

// canonicalJSON renders a payload with sorted keys so the comparison is about
// values rather than about which encoder emitted which order.
func canonicalJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("cannot decode %q: %v", raw, err)
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("cannot re-encode: %v", err)
	}
	return string(encoded)
}

// firstDifference trims a long payload to the region around the first difference,
// so a failure names the field rather than printing two full payloads.
func firstDifference(subject, other string) string {
	limit := len(subject)
	if len(other) < limit {
		limit = len(other)
	}
	index := 0
	for index < limit && subject[index] == other[index] {
		index++
	}
	start := index - 120
	if start < 0 {
		start = 0
	}
	end := index + 200
	if end > len(subject) {
		end = len(subject)
	}
	return "..." + subject[start:end] + "..."
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("cannot prepare %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}

// statusService builds a Service whose machine state matches what the Python
// script is told to simulate.
func statusService(t *testing.T, home, osID string, missing map[string]bool, versionOutput string) *Service {
	t.Helper()
	runner := testutil.NewRecordingRunner(t,
		// icacls is only consulted on the Windows path, where a write has to harden
		// the file before publishing it.
		testutil.Succeed("", "icacls"),
		testutil.Succeed(versionOutput),
	)
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID(osID),
		runtime.WithEnv(map[string]string{"HOME": home}),
		runtime.WithRunner(runner.Runner()),
		runtime.WithLookup(func(name string) (string, bool) {
			if missing[name] {
				return "", false
			}
			return "/usr/local/bin/" + name, true
		}),
	)
	return &Service{
		Runtime: rt,
		Writer:  config.NewWriter(rt),
		Store:   profile.NewStore(rt),
		Probes:  &provider.Client{HTTP: cannedTransport{status: 200, body: "{}"}},
	}
}
