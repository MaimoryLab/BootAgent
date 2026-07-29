package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/profile"
	"github.com/MaimoryLab/OneAgent/desktop/internal/provider"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/testutil"
)

// This is the layer where everything below it is composed, so the comparison is on
// what a whole request produces: the response the frontend switches on, and every
// file the run left on disk. A difference in either is a difference the user sees.
//
// Nothing here reaches a provider or a package manager. The subprocess runner and
// the HTTP transport are both replaced, which is what makes an install request
// reproducible enough to compare byte for byte.

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

// installCase is one request, in the field names both sides read.
type installCase struct {
	Name           string   `json:"name"`
	Agents         []string `json:"agents"`
	ProfileAgents  []string `json:"profile_agents"`
	Provider       string   `json:"provider"`
	APIBaseURL     string   `json:"api_base_url"`
	APIKey         string   `json:"api_key"`
	Model          string   `json:"model"`
	SmallFastModel string   `json:"small_fast_model"`
	Configure      bool     `json:"configure"`
	CheckAgentOnly bool     `json:"check_agent_only"`
	SkipTest       bool     `json:"skip_test"`
	// ProbeStatus is what the fake endpoint answers every probe with, so a
	// refused key or an unsupported protocol can be compared too.
	ProbeStatus int    `json:"probe_status"`
	ProbeBody   string `json:"probe_body"`
}

var installCases = []installCase{
	{
		Name: "one agent configured", Agents: []string{"codex"}, Provider: "ppio",
		APIKey: "sk-test", Model: "gpt-5-mini", Configure: true, SkipTest: true,
		ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name:   "several agents across protocols",
		Agents: []string{"codex", "claude-code", "opencode"}, Provider: "ppio",
		APIKey: "sk-test", Model: "gpt-5-mini", Configure: true, SkipTest: true,
		ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "guide-only alongside auto", Agents: []string{"codex", "gemini-cli"},
		Provider: "ppio", APIKey: "sk-test", Model: "m", Configure: true, SkipTest: true,
		ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "existing account", Agents: []string{"codex"}, Provider: "ppio",
		Model: "m", Configure: false, SkipTest: true, ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "check only", Agents: []string{"codex", "aider"}, Provider: "ppio",
		APIKey: "sk-test", Model: "m", Configure: true, CheckAgentOnly: true,
		SkipTest: true, ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "profile wider than the install", Agents: []string{"codex"},
		ProfileAgents: []string{"codex", "claude-code", "aider"}, Provider: "ppio",
		APIKey: "sk-test", Model: "m", Configure: true, SkipTest: true,
		ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "custom endpoint", Agents: []string{"codex"}, Provider: "custom",
		APIBaseURL: "https://vendor.example/v1", APIKey: "sk-test", Model: "qwen3",
		Configure: true, SkipTest: true, ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "small fast model", Agents: []string{"claude-code"}, Provider: "ppio",
		APIKey: "sk-test", Model: "claude-sonnet-4", SmallFastModel: "claude-haiku-4",
		Configure: true, SkipTest: true, ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "aider writes a shell script", Agents: []string{"aider"}, Provider: "ppio",
		APIKey: "sk-test", Model: "m", Configure: true, SkipTest: true,
		ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "probe passes", Agents: []string{"codex"}, Provider: "ppio",
		APIKey: "sk-test", Model: "m", Configure: true, ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "key rejected", Agents: []string{"codex"}, Provider: "ppio",
		APIKey: "sk-bad", Model: "m", Configure: true,
		ProbeStatus: 401, ProbeBody: `{"error":"bad key"}`,
	},
	{
		Name: "protocol unsupported", Agents: []string{"codex"}, Provider: "ppio",
		APIKey: "sk-test", Model: "m", Configure: true,
		ProbeStatus: 404, ProbeBody: "",
	},
	{
		Name: "server error", Agents: []string{"codex"}, Provider: "ppio",
		APIKey: "sk-test", Model: "m", Configure: true,
		ProbeStatus: 500, ProbeBody: "boom",
	},
	{
		Name: "non-ascii model", Agents: []string{"codex"}, Provider: "ppio",
		APIKey: "sk-test", Model: "通义千问", Configure: true, SkipTest: true,
		ProbeStatus: 200, ProbeBody: "{}",
	},
	{
		Name: "key needing quotes", Agents: []string{"codex"}, Provider: "ppio",
		APIKey: "sk-with 'quote'", Model: "m", Configure: true, SkipTest: true,
		ProbeStatus: 200, ProbeBody: "{}",
	},
}

// pythonResponse is install_many's return value, decoded.
type pythonResponse struct {
	OK      bool `json:"ok"`
	Code    int  `json:"code"`
	Results []struct {
		Agent         string `json:"agent"`
		Status        string `json:"status"`
		Config        string `json:"config"`
		Installed     bool   `json:"installed"`
		Version       string `json:"version"`
		LockedVersion string `json:"lockedVersion"`
		Code          int    `json:"code"`
		ErrorCode     string `json:"error_code"`
		Message       string `json:"message"`
		Retryable     bool   `json:"retryable"`
	} `json:"results"`
	Log  string `json:"log"`
	Next string `json:"next"`
}

const installScript = `
import json, sys
sys.path.insert(0, sys.argv[1])
from pathlib import Path
from unittest.mock import patch
from urllib.error import HTTPError
import io, subprocess

from oneagent import providers
from oneagent.installer import InstallOptions, Runtime, install_many

home = Path(sys.argv[2])
case = json.loads(sys.argv[3])
status, body = case["probe_status"], case["probe_body"]


class FakeResponse:
    def __init__(self):
        self.status = status

    def read(self):
        return body.encode()

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False


def fake_urlopen(request, timeout=None):
    if status in {200, 204}:
        return FakeResponse()
    raise HTTPError(request.full_url, status, "err", {}, io.BytesIO(body.encode()))


def fake_runner(argv, **kwargs):
    return subprocess.CompletedProcess(argv, 0, "1.0.0", "")


runtime = Runtime(
    home=home, os_id="linux", runner=fake_runner,
    which=lambda name: f"/usr/local/bin/{name}", env={"HOME": str(home)},
)
with patch.object(providers, "urlopen", fake_urlopen):
    result = install_many(
        InstallOptions(
            agents=case["agents"],
            profile_agents=case["profile_agents"] or None,
            provider=case["provider"],
            api_base_url=case["api_base_url"],
            api_key=case["api_key"],
            model=case["model"],
            small_fast_model=case["small_fast_model"],
            configure=case["configure"],
            check_agent_only=case["check_agent_only"],
            skip_test=case["skip_test"],
            timeout=30,
        ),
        runtime,
    )
result.pop("probe", None)
result.pop("probes", None)
print(json.dumps(result))
`

func TestParityAnInstallRequestProducesTheSameResponseAndFiles(t *testing.T) {
	for _, testCase := range installCases {
		t.Run(testCase.Name, func(t *testing.T) {
			pythonHome := t.TempDir()
			want := runPythonInstall(t, pythonHome, testCase)

			goHome := t.TempDir()
			got, err := goInstall(t, goHome, testCase)
			if err != nil {
				t.Fatalf("Go refused a request Python carried out: %v", err)
			}

			compareResponse(t, got, want, goHome)
			compareTree(t, pythonHome, goHome)
		})
	}
}

func runPythonInstall(t *testing.T, home string, testCase installCase) pythonResponse {
	t.Helper()
	encoded, err := json.Marshal(testCase)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	command := exec.Command(pythonBin(t), "-c", installScript, repoRoot(t), home, string(encoded))
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
	var response pythonResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("cannot read python output %q: %v", output, err)
	}
	return response
}

// cannedTransport answers every probe with one response, matching the fake
// urlopen on the Python side.
type cannedTransport struct {
	status int
	body   string
}

func (c cannedTransport) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Header:     http.Header{},
	}, nil
}

func goInstall(t *testing.T, home string, testCase installCase) (InstallResult, error) {
	t.Helper()
	runner := testutil.NewRecordingRunner(t, testutil.Succeed("1.0.0"))
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{"HOME": home}),
		runtime.WithRunner(runner.Runner()),
		runtime.WithLookup(func(name string) (string, bool) {
			return "/usr/local/bin/" + name, true
		}),
	)
	service := &Service{
		Runtime: rt,
		Writer:  config.NewWriter(rt),
		Store:   profile.NewStore(rt),
		Probes:  &provider.Client{HTTP: cannedTransport{status: testCase.ProbeStatus, body: testCase.ProbeBody}},
	}
	return service.Install(InstallOptions{
		Agents: testCase.Agents, ProfileAgents: testCase.ProfileAgents,
		Provider: testCase.Provider, APIBaseURL: testCase.APIBaseURL,
		APIKey: testCase.APIKey, Model: testCase.Model,
		SmallFastModel: testCase.SmallFastModel, Configure: testCase.Configure,
		CheckAgentOnly: testCase.CheckAgentOnly, SkipTest: testCase.SkipTest,
		Timeout: 30 * time.Second,
	})
}

func compareResponse(t *testing.T, got InstallResult, want pythonResponse, goHome string) {
	t.Helper()
	if got.OK != want.OK {
		t.Errorf("ok Go=%v Python=%v", got.OK, want.OK)
	}
	if got.Code != want.Code {
		t.Errorf("code Go=%d Python=%d", got.Code, want.Code)
	}
	if len(got.Results) != len(want.Results) {
		t.Fatalf("results Go=%d Python=%d entries", len(got.Results), len(want.Results))
	}
	for index, result := range got.Results {
		expected := want.Results[index]
		if result.Agent != expected.Agent {
			t.Errorf("result %d agent Go=%q Python=%q", index, result.Agent, expected.Agent)
		}
		if result.Status != expected.Status {
			t.Errorf("%s status Go=%q Python=%q", result.Agent, result.Status, expected.Status)
		}
		if result.ErrorCode != expected.ErrorCode {
			t.Errorf("%s error_code Go=%q Python=%q", result.Agent, result.ErrorCode, expected.ErrorCode)
		}
		if result.Message != expected.Message {
			t.Errorf("%s message\n  Go:     %q\n  Python: %q", result.Agent, result.Message, expected.Message)
		}
		if result.Code != expected.Code {
			t.Errorf("%s code Go=%d Python=%d", result.Agent, result.Code, expected.Code)
		}
		if result.Retryable != expected.Retryable {
			t.Errorf("%s retryable Go=%v Python=%v", result.Agent, result.Retryable, expected.Retryable)
		}
		// The config path is absolute and the two homes differ, so it is compared
		// relative to each home -- which still catches an adapter writing to the
		// wrong file.
		if relativeTo(result.Config, goHome) != relativeTo(expected.Config, "") {
			t.Errorf("%s config Go=%q Python=%q", result.Agent, result.Config, expected.Config)
		}
	}
	if got.Next != want.Next {
		t.Errorf("next\n  Go:     %q\n  Python: %q", got.Next, want.Next)
	}
	if got.Log != want.Log {
		t.Errorf("log\n  Go:     %q\n  Python: %q", got.Log, want.Log)
	}
}

// relativeTo strips whichever home prefix a path carries, so the two runs'
// temporary directories do not make every comparison fail.
func relativeTo(path, home string) string {
	if path == "" {
		return ""
	}
	if home != "" && strings.HasPrefix(path, home) {
		return strings.TrimPrefix(strings.TrimPrefix(path, home), "/")
	}
	if index := strings.Index(path, "/.oneagent/"); index >= 0 {
		return path[index+1:]
	}
	// A config outside .oneagent, which is where four of the five adapters write.
	for _, marker := range []string{"/.codex/", "/.claude/", "/.config/", "/.kilocode/"} {
		if index := strings.Index(path, marker); index >= 0 {
			return path[index+1:]
		}
	}
	return path
}
