package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The CLI is the only way to execute the Go core today, and its exit codes are an
// external contract: install.sh and CI scripts branch on them. These drive the real
// entry point rather than the layer beneath it, because the argument parsing and the
// code mapping are the parts nothing else covers.
//
// Nothing here reaches a provider or a package manager: --skip-test opts out of
// every round trip, and no test passes --install-agent.

// capture runs the CLI with stdout and stderr redirected to files, and returns both
// along with the exit code.
func capture(t *testing.T, argv ...string) (stdout, stderr string, code int) {
	t.Helper()
	outFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("cannot create a temporary file: %v", err)
	}
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("cannot create a temporary file: %v", err)
	}
	code = run(argv, outFile, errFile)
	outFile.Close()
	errFile.Close()
	outBytes, _ := os.ReadFile(outFile.Name())
	errBytes, _ := os.ReadFile(errFile.Name())
	return string(outBytes), string(errBytes), code
}

func TestAnInstallWritesTheConfigAndExitsZero(t *testing.T) {
	home := t.TempDir()
	stdout, stderr, code := capture(t,
		"--agent", "codex", "--provider", "ppio", "--api-key", "sk-cli-test",
		"--model", "gpt-5-mini", "--skip-test", "--home", home)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "codex") || !strings.Contains(stdout, "configured") {
		t.Errorf("stdout does not report the outcome: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Errorf("no config was written: %v", err)
	}
	// The next step tells the user how to start the Agent against what was written.
	if !strings.Contains(stdout, "next:") {
		t.Errorf("stdout gives no next step: %q", stdout)
	}
}

func TestTheKeyIsNeverPrintedEvenWhenAnErrorQuotesTheInput(t *testing.T) {
	// A registry URL can carry a credential, and the message names the URL. This is
	// the path where a naive error report would echo it.
	const key = "sk-must-not-be-printed"
	_, stderr, code := capture(t,
		"--agent", "codex", "--api-key", key, "--model", "m", "--skip-test",
		"--registry", "https://user:"+key+"@mirror.example.com/", "--home", t.TempDir())
	if code == 0 {
		t.Fatal("a registry carrying credentials was accepted")
	}
	if strings.Contains(stderr, key) {
		t.Errorf("stderr carries the credential: %q", stderr)
	}
	if !strings.Contains(stderr, "credentials") {
		t.Errorf("stderr does not say what was wrong: %q", stderr)
	}
}

func TestTheKeyCanArriveThroughTheEnvironmentRatherThanArgv(t *testing.T) {
	// argv is visible in a process list, so a key passed that way is readable by
	// any other user on the machine. The environment is the documented route.
	t.Setenv("ONEAGENT_API_KEY", "sk-from-environment")
	home := t.TempDir()
	_, stderr, code := capture(t,
		"--agent", "codex", "--model", "m", "--skip-test", "--home", home)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	env, err := os.ReadFile(filepath.Join(home, ".oneagent", "agents", "codex.env"))
	if err != nil {
		t.Fatalf("no env file was written: %v", err)
	}
	if !strings.Contains(string(env), "sk-from-environment") {
		t.Error("the key from the environment did not reach the env file")
	}
}

func TestExitCodesCarryTheReasonRatherThanAGenericFailure(t *testing.T) {
	// install.sh and CI branch on these, so a code that collapses to 1 makes a
	// script unable to tell a bad request from an unreachable provider.
	cases := []struct {
		name string
		argv []string
		want int
	}{
		{"no agent named", []string{"--skip-test"}, 2},
		{"unknown agent", []string{"--agent", "not-an-agent", "--api-key", "sk-a", "--skip-test"}, 2},
		{"locked and latest together", []string{
			"--agent", "codex", "--api-key", "sk-a", "--skip-test", "--locked-version", "--latest",
		}, 2},
		{"http registry", []string{
			"--agent", "codex", "--api-key", "sk-a", "--skip-test", "--registry", "http://m.example/",
		}, 2},
		{"no key while configuring", []string{"--agent", "codex", "--skip-test"}, 2},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Cleared so the environment fallback does not satisfy the last case.
			t.Setenv("ONEAGENT_API_KEY", "")
			_, stderr, code := capture(t, append(testCase.argv, "--home", t.TempDir())...)
			if code != testCase.want {
				t.Errorf("exit code = %d, want %d (stderr %q)", code, testCase.want, stderr)
			}
			if stderr == "" {
				t.Error("a failure printed nothing to stderr")
			}
		})
	}
}

func TestStatusPrintsThePayloadWithoutCredentialMaterial(t *testing.T) {
	home := t.TempDir()
	if _, stderr, code := capture(t,
		"--agent", "codex", "--api-key", "sk-status-probe", "--model", "m",
		"--skip-test", "--home", home); code != 0 {
		t.Fatalf("setup failed: %s", stderr)
	}

	stdout, stderr, code := capture(t, "status", "--home", home)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "sk-status-probe") {
		t.Error("the status output carries the key")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("status did not print valid JSON: %v", err)
	}
	for _, key := range []string{"apiVersion", "platform", "agents", "catalog", "profiles"} {
		if _, present := payload[key]; !present {
			t.Errorf("the payload is missing %q", key)
		}
	}
	// hasKey is the only thing the contract allows it to say about a credential.
	profiles, _ := payload["profiles"].([]any)
	if len(profiles) == 0 {
		t.Fatal("the install recorded no profile")
	}
	first, _ := profiles[0].(map[string]any)
	if truth, _ := first["hasKey"].(bool); !truth {
		t.Error("the stored profile does not report that a key is held")
	}
}

func TestAgentListAndSetDriveOneAgentAtATime(t *testing.T) {
	home := t.TempDir()
	if _, stderr, code := capture(t,
		"--agent", "codex,opencode", "--api-key", "sk-a", "--model", "first-model",
		"--skip-test", "--home", home); code != 0 {
		t.Fatalf("setup failed: %s", stderr)
	}

	stdout, _, code := capture(t, "agent", "list", "--home", home)
	if code != 0 {
		t.Fatalf("list failed with %d", code)
	}
	for _, id := range []string{"codex", "opencode"} {
		if !strings.Contains(stdout, id) {
			t.Errorf("list omits %s: %q", id, stdout)
		}
	}

	// Repointing one Agent must leave the other alone, which is the whole point of
	// per-Agent credentials.
	before, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.jsonc"))
	if err != nil {
		t.Fatalf("cannot read opencode's config: %v", err)
	}
	if _, stderr, code := capture(t,
		"agent", "set", "codex", "--provider", "novita", "--api-key", "sk-b",
		"--model", "second-model", "--home", home); code != 0 {
		t.Fatalf("set failed with %d: %s", code, stderr)
	}
	after, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.jsonc"))
	if err != nil {
		t.Fatalf("cannot re-read opencode's config: %v", err)
	}
	if string(before) != string(after) {
		t.Error("repointing codex rewrote opencode's config")
	}
	codex, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("cannot read codex's config: %v", err)
	}
	if !strings.Contains(string(codex), "second-model") {
		t.Error("codex was not repointed")
	}
}

func TestAgentSetRefusesAnUnusableRequest(t *testing.T) {
	for _, argv := range [][]string{
		{"agent", "set", "gemini-cli", "--api-key", "sk-a", "--model", "m"},
		{"agent", "set", "../escape", "--api-key", "sk-a", "--model", "m"},
		{"agent", "set", "codex", "--model", "m"},
		{"agent", "bogus"},
		{"agent"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			t.Setenv("ONEAGENT_API_KEY", "")
			_, stderr, code := capture(t, append(argv, "--home", t.TempDir())...)
			if code == 0 {
				t.Error("the request was accepted")
			}
			if stderr == "" {
				t.Error("nothing was reported")
			}
		})
	}
}

func TestJSONOutputIsMachineReadableOnBothSuccessAndFailure(t *testing.T) {
	// Scripts read this, so a failure has to be JSON too rather than a bare line
	// on stderr.
	stdout, _, code := capture(t,
		"--agent", "codex", "--api-key", "sk-a", "--model", "m", "--skip-test",
		"--json", "--home", t.TempDir())
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	var success map[string]any
	if err := json.Unmarshal([]byte(stdout), &success); err != nil {
		t.Fatalf("success output is not JSON: %v\n%s", err, stdout)
	}
	if truth, _ := success["ok"].(bool); !truth {
		t.Errorf("ok = false on a successful run: %s", stdout)
	}

	t.Setenv("ONEAGENT_API_KEY", "")
	failure, _, code := capture(t, "--agent", "codex", "--skip-test", "--json", "--home", t.TempDir())
	if code == 0 {
		t.Fatal("a request with no key was accepted")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(failure), &payload); err != nil {
		t.Fatalf("failure output is not JSON: %v\n%s", err, failure)
	}
	// The six-key error shape the transport contract fixes.
	for _, key := range []string{"ok", "error", "message", "status", "error_code", "retryable"} {
		if _, present := payload[key]; !present {
			t.Errorf("the error payload is missing %q: %s", key, failure)
		}
	}
	if _, present := payload["exit_code"]; present {
		t.Error("exit_code is not part of the response shape")
	}
}

func TestCheckOnlyReportsWithoutWritingAnyConfig(t *testing.T) {
	home := t.TempDir()
	stdout, stderr, code := capture(t,
		"--agent", "codex", "--check-agent-only", "--skip-test", "--home", home)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "configured") {
		t.Errorf("a check-only run claimed to configure something: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); err == nil {
		t.Error("a check-only run wrote an Agent config")
	}
}
