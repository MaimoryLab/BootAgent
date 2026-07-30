package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The exit code is the one part of this program with a consumer that cannot read
// a message: install.sh branches on it, and so do CI scripts. Every other parity
// file compares what a layer computes; this one compares what the process
// returns, because the argument parsing and the code mapping sit above every
// layer those files reach.
//
// It was verified by hand once, during the review that first ran the Go core as a
// program. Verifying a contract once and leaving no gate is the shape this whole
// methodology exists to remove -- a comment two files over already claimed the
// truncation "had to agree" while no case exercised it. So the comparison runs
// here, on both real entry points.
//
// Nothing here reaches a provider or a package manager: --skip-test opts out of
// every round trip and no case passes --install-agent.

// exitCase is one invocation, in the argument form both entry points accept.
type exitCase struct {
	name string
	argv []string
	// why records what the code means to a script reading it, so a change that
	// collapses two reasons into one code has to be argued for rather than
	// silently accepted by a table update.
	why string
}

var exitCases = []exitCase{
	{"no agent named", []string{"--skip-test"}, "nothing to do, so the request is unusable"},
	{"unknown agent", []string{"--agent", "not-an-agent", "--api-key", "sk-a", "--skip-test"},
		"an id outside the lock: the manifest is the only source of ids"},
	{"agent id escaping its directory", []string{"--agent", "../escape", "--api-key", "sk-a", "--skip-test"},
		"a path traversal attempt has to be refused, not resolved"},
	{"locked and latest together", []string{"--agent", "codex", "--api-key", "sk-a", "--skip-test", "--locked-version", "--latest"},
		"contradictory version intent, so neither can be assumed"},
	{"http registry", []string{"--agent", "codex", "--api-key", "sk-a", "--skip-test", "--registry", "http://m.example/"},
		"a plaintext mirror would carry the package and the credential in the clear"},
	{"registry carrying credentials", []string{"--agent", "codex", "--api-key", "sk-a", "--skip-test",
		"--registry", "https://user:pw@m.example/"}, "a credential in a URL reaches argv and logs"},
	{"no key while configuring", []string{"--agent", "codex", "--skip-test"},
		"configuring without a credential produces an Agent that cannot authenticate"},
}

// I expected a guide-only Agent asked to be configured to be a failure, and wrote
// it into the table above. Both implementations return 0 and print the guide --
// which is right: config_mode=guide means there is nothing to write, not that the
// request was wrong. The "no longer tests a failure" assertion caught my own bad
// assumption before the table did, so the case moved here rather than being
// deleted; 0 is as much a contract as 2 is.
func TestParityAGuideOnlyAgentSucceedsOnBothSides(t *testing.T) {
	python := pythonParityBin(t)
	goBinary := buildCLI(t)
	root := repoRootFrom(t)
	argv := []string{"--agent", "cursor", "--api-key", "sk-a", "--model", "m", "--skip-test"}

	goCode, goErr := runForCode(t, goBinary, append(argv, "--home", t.TempDir()), nil)
	pyCode, pyErr := runForCode(t, python, append([]string{"-m", "oneagent.cli"}, argv...),
		[]string{"HOME=" + t.TempDir(), "PYTHONPATH=" + root, "ONEAGENT_API_KEY="})

	if goCode != 0 || pyCode != 0 {
		t.Errorf("a guide-only request failed: Go = %d (%s), Python = %d (%s)",
			goCode, goErr, pyCode, pyErr)
	}
}

func TestParityTheExitCodeForEveryFailureMatchesPython(t *testing.T) {
	python := pythonParityBin(t)
	goBinary := buildCLI(t)
	root := repoRootFrom(t)

	for _, testCase := range exitCases {
		t.Run(testCase.name, func(t *testing.T) {
			goCode, goErr := runForCode(t, goBinary, append(testCase.argv, "--home", t.TempDir()), nil)
			pyCode, pyErr := runForCode(t, python,
				append([]string{"-m", "oneagent.cli"}, testCase.argv...),
				[]string{"HOME=" + t.TempDir(), "PYTHONPATH=" + root, "ONEAGENT_API_KEY="})

			if goCode != pyCode {
				t.Errorf("exit code Go = %d, Python = %d\n  why: %s\n  Go said: %s\n  Python said: %s",
					goCode, pyCode, testCase.why, goErr, pyErr)
			}
			// A code with no message leaves a user with nothing, and a script with
			// nothing to log. Both sides have to say something.
			if strings.TrimSpace(goErr) == "" {
				t.Error("the Go CLI reported nothing on stderr")
			}
			if strings.TrimSpace(pyErr) == "" {
				t.Error("the Python CLI reported nothing on stderr")
			}
			// Not vacuous: these are failures, so a zero here means the case stopped
			// exercising what it names.
			if goCode == 0 {
				t.Errorf("%q was accepted by both, so this case no longer tests a failure", testCase.name)
			}
		})
	}
}

func TestParityTheJSONErrorShapeMatchesPython(t *testing.T) {
	// Scripts read --json rather than stderr, so the shape is as much a contract as
	// the code. The six keys are fixed by the transport contract; exit_code is
	// deliberately not among them.
	python := pythonParityBin(t)
	goBinary := buildCLI(t)
	root := repoRootFrom(t)
	argv := []string{"--agent", "codex", "--skip-test", "--json"}

	goOut, _ := outputOf(t, goBinary, append(argv, "--home", t.TempDir()), nil)
	pyOut, _ := outputOf(t, python, append([]string{"-m", "oneagent.cli"}, argv...),
		[]string{"HOME=" + t.TempDir(), "PYTHONPATH=" + root, "ONEAGENT_API_KEY="})

	goKeys := sortedKeys(t, goOut)
	pyKeys := sortedKeys(t, pyOut)
	if strings.Join(goKeys, ",") != strings.Join(pyKeys, ",") {
		t.Errorf("error payload keys differ\n  Go:     %v\n  Python: %v", goKeys, pyKeys)
	}
	if len(goKeys) == 0 {
		t.Fatal("no keys were compared, so this proves nothing")
	}
}

// runForCode runs one invocation and returns its exit code and stderr. Every case
// here is a failure, so a non-zero status is the expected outcome rather than an
// error to report.
func runForCode(t *testing.T, binary string, argv []string, env []string) (int, string) {
	t.Helper()
	command := exec.Command(binary, argv...)
	if env != nil {
		command.Env = env
	} else {
		command.Env = append(os.Environ(), "ONEAGENT_API_KEY=")
	}
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	code := 0
	if err := command.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("cannot run %s: %v", binary, err)
		}
		code = exitErr.ExitCode()
	}
	return code, stderr.String()
}

func outputOf(t *testing.T, binary string, argv []string, env []string) (string, string) {
	t.Helper()
	command := exec.Command(binary, argv...)
	if env != nil {
		command.Env = env
	} else {
		command.Env = append(os.Environ(), "ONEAGENT_API_KEY=")
	}
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("cannot run %s: %v", binary, err)
		}
	}
	return stdout.String(), stderr.String()
}

func sortedKeys(t *testing.T, payload string) []string {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, payload)
	}
	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// buildCLI compiles the real entry point, because the parsing and the code
// mapping are what this compares -- calling run() in-process would skip both the
// binary's own argument handling and the status it actually returns.
func buildCLI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "oneagent")
	build := exec.Command("go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("cannot build the CLI: %v\n%s", err, output)
	}
	return binary
}

func pythonParityBin(t *testing.T) string {
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

func repoRootFrom(t *testing.T) string {
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
