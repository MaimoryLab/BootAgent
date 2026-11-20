package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// firstTaskRunner stands in for the Agent. `act` plays its part: it receives the
// directory the command was started in and returns what the command printed.
type firstTaskRunner struct {
	available map[string]bool
	act       func(directory string) (process.Result, error)
	argv      []string
	input     string
	directory string
	timeout   time.Duration
}

func (r *firstTaskRunner) LookPath(command string) (string, bool) {
	return "/bin/" + command, r.available[command]
}

func (r *firstTaskRunner) Run(context.Context, []string, map[string]string, time.Duration) (process.Result, error) {
	return process.Result{}, errors.New("the task prompt must not travel on argv")
}

func (r *firstTaskRunner) RunPrivateInput(ctx context.Context, argv []string, _ map[string]string, timeout time.Duration, input string) (process.Result, error) {
	r.argv = append([]string(nil), argv...)
	r.input = input
	r.timeout = timeout
	r.directory = process.WorkingDirectory(ctx)
	if r.act == nil {
		return process.Result{}, nil
	}
	return r.act(r.directory)
}

func firstTaskCore(t *testing.T, runner *firstTaskRunner) *UseCases {
	t.Helper()
	return NewUseCases(StatusOptions{
		Home:        t.TempDir(),
		Platform:    platform.For("linux", "amd64"),
		Runner:      runner,
		Environment: map[string]string{"PATH": "/usr/bin"},
	})
}

func writesPage(directory string) (process.Result, error) {
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		return process.Result{}, err
	}
	return process.Result{ExitCode: 0, Stdout: "Created index.html"}, nil
}

// removeLater cleans up a directory a successful run deliberately keeps.
func removeLater(t *testing.T, directory string) {
	t.Helper()
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
}

func TestFirstTaskAgentsListsOnlyInstalledAgentsThatCanRunUnattended(t *testing.T) {
	runner := &firstTaskRunner{available: map[string]bool{"codex": true, "claude": true, "opencode": true, "dsh": true}}
	agents, err := firstTaskCore(t, runner).FirstTaskAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0].ID != "codex" || agents[1].ID != "claude-code" {
		t.Fatalf("agents = %#v", agents)
	}
	runner.available = map[string]bool{"claude": true}
	agents, err = firstTaskCore(t, runner).FirstTaskAgents(context.Background())
	if err != nil || len(agents) != 1 || agents[0].ID != "claude-code" {
		t.Fatalf("agents = %#v, err=%v", agents, err)
	}
}

func TestRunFirstTaskConfinesCodexToItsOwnTemporaryDirectory(t *testing.T) {
	runner := &firstTaskRunner{available: map[string]bool{"codex": true}, act: writesPage}
	result, err := firstTaskCore(t, runner).RunFirstTask(context.Background(), FirstTaskRequest{AgentID: "codex", Locale: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	removeLater(t, result.Directory)
	if !result.OK || !slices.Equal(result.Files, []string{"index.html"}) {
		t.Fatalf("result = %#v", result)
	}
	if filepath.Dir(result.Directory) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(result.Directory), "bootagent-first-task-") {
		t.Fatalf("task ran in %q, want a fresh directory under %q", result.Directory, os.TempDir())
	}
	if runner.directory != result.Directory {
		t.Fatalf("command started in %q, want %q", runner.directory, result.Directory)
	}
	argv := strings.Join(runner.argv, " ")
	for _, want := range []string{
		"/bin/codex exec",
		"--sandbox workspace-write",
		// A user's own config.toml can turn network access on for this sandbox.
		"-c sandbox_workspace_write.network_access=false",
		"-C " + result.Directory,
	} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv %q is missing %q", argv, want)
		}
	}
	if strings.Contains(argv, "danger") || runner.argv[len(runner.argv)-1] != "-" {
		t.Fatalf("argv = %q", argv)
	}
	if !strings.Contains(runner.input, "index.html") || strings.Contains(argv, "index.html") {
		t.Fatalf("the prompt belongs on stdin: input=%q argv=%q", runner.input, argv)
	}
	if runner.timeout <= 0 || runner.timeout > 10*time.Minute {
		t.Fatalf("timeout = %s", runner.timeout)
	}
}

// Claude Code has no flag for where to work, and without Bash it has no way to
// run a command or reach the network.
func TestRunFirstTaskGivesClaudeCodeOnlyTheWriteTool(t *testing.T) {
	runner := &firstTaskRunner{available: map[string]bool{"claude": true}, act: writesPage}
	result, err := firstTaskCore(t, runner).RunFirstTask(context.Background(), FirstTaskRequest{AgentID: "claude-code", Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	removeLater(t, result.Directory)
	if !result.OK || runner.directory != result.Directory {
		t.Fatalf("result = %#v, started in %q", result, runner.directory)
	}
	argv := strings.Join(runner.argv, " ")
	for _, want := range []string{"/bin/claude -p", "--tools Write", "--permission-mode acceptEdits", "--no-session-persistence"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv %q is missing %q", argv, want)
		}
	}
	if strings.Contains(argv, "Bash") || strings.Contains(argv, "bypassPermissions") {
		t.Fatalf("argv = %q", argv)
	}
	if !strings.Contains(runner.input, "index.html") || strings.ContainsAny(runner.input, "的页") {
		t.Fatalf("English prompt expected, got %q", runner.input)
	}
}

func TestRunFirstTaskReportsAFailedRunWithoutLeakingTheKey(t *testing.T) {
	const key = "sk-first-task-secret-0123456789"
	var worked string
	runner := &firstTaskRunner{available: map[string]bool{"codex": true}, act: func(directory string) (process.Result, error) {
		worked = directory
		return process.Result{ExitCode: 1, Stderr: "401 Unauthorized: invalid api key " + key}, nil
	}}
	core := firstTaskCore(t, runner)
	if err := core.providers.SaveKey(context.Background(), "ppio", key); err != nil {
		t.Fatal(err)
	}
	result, err := core.RunFirstTask(context.Background(), FirstTaskRequest{AgentID: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Directory != "" || len(result.Files) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(result.Log, key) || !strings.Contains(result.Log, "401 Unauthorized") {
		t.Fatalf("log = %q", result.Log)
	}
	// Nothing was produced, so there is nothing to keep.
	if _, statErr := os.Stat(worked); !os.IsNotExist(statErr) {
		t.Fatalf("empty task directory %q was left behind: %v", worked, statErr)
	}
}

func TestRunFirstTaskDoesNotCallAnEmptyRunASuccess(t *testing.T) {
	runner := &firstTaskRunner{available: map[string]bool{"codex": true}, act: func(string) (process.Result, error) {
		return process.Result{ExitCode: 0, Stdout: "Done."}, nil
	}}
	result, err := firstTaskCore(t, runner).RunFirstTask(context.Background(), FirstTaskRequest{AgentID: "codex"})
	if err != nil || result.OK || result.Directory != "" {
		t.Fatalf("result = %#v, err=%v", result, err)
	}
}

func TestRunFirstTaskListsWhatTheAgentWrote(t *testing.T) {
	runner := &firstTaskRunner{available: map[string]bool{"codex": true}, act: func(directory string) (process.Result, error) {
		if err := os.MkdirAll(filepath.Join(directory, "assets"), 0o700); err != nil {
			return process.Result{}, err
		}
		for _, name := range []string{"index.html", filepath.Join("assets", "style.css")} {
			if err := os.WriteFile(filepath.Join(directory, name), []byte("x"), 0o600); err != nil {
				return process.Result{}, err
			}
		}
		return process.Result{ExitCode: 0}, nil
	}}
	result, err := firstTaskCore(t, runner).RunFirstTask(context.Background(), FirstTaskRequest{AgentID: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	removeLater(t, result.Directory)
	if !slices.Equal(result.Files, []string{"assets/style.css", "index.html"}) {
		t.Fatalf("files = %#v", result.Files)
	}
}

func TestRunFirstTaskRejectsAgentsItCannotDrive(t *testing.T) {
	runner := &firstTaskRunner{available: map[string]bool{"opencode": true}}
	core := firstTaskCore(t, runner)
	for agent, want := range map[string]string{
		"opencode": oneerrors.InvalidRequest,
		"":         oneerrors.InvalidRequest,
		"codex":    oneerrors.PrerequisiteMissing,
	} {
		_, err := core.RunFirstTask(context.Background(), FirstTaskRequest{AgentID: agent})
		var failure *oneerrors.BootAgentError
		if !errors.As(err, &failure) || failure.Code != want {
			t.Fatalf("RunFirstTask(%q) = %v, want %s", agent, err, want)
		}
	}
	if runner.argv != nil {
		t.Fatalf("a rejected request still ran %q", runner.argv)
	}
}
