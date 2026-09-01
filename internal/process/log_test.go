package process

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// recordingInner deliberately does not embed OSRunner: an embedded
// RunWithOutput would be promoted and actually start a process.
type recordingInner struct {
	started [][]string
	result  Result
	err     error
	input   string
}

func (r *recordingInner) Run(context.Context, []string, map[string]string, time.Duration) (Result, error) {
	return r.result, r.err
}

func (r *recordingInner) RunPrivateInput(_ context.Context, _ []string, _ map[string]string, _ time.Duration, input string) (Result, error) {
	r.input = input
	return r.result, r.err
}

func (r *recordingInner) Start(argv []string, _ map[string]string) error {
	r.started = append(r.started, argv)
	return r.err
}

func (r *recordingInner) LookPath(string) (string, bool) { return "", false }

func TestLoggingRunnerRecordsOutcomeAndHidesSecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	// The file is named for the day, so a long-lived desktop session does not
	// grow one unbounded log.
	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	inner := &recordingInner{result: Result{ExitCode: 1, Stdout: "using key sk-secret", Stderr: "boom"}}
	runner := LoggingRunner{Inner: inner, Dir: dir}

	if _, err := runner.Run(context.Background(), []string{"npm", "install", "-g", "codex"}, map[string]string{"TEST_API_KEY": "sk-secret"}, 0); err != nil {
		t.Fatal(err)
	}
	entry, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(entry)
	for _, want := range []string{"npm install -g codex", "exit 1", "stderr: boom"} {
		if !strings.Contains(text, want) {
			t.Errorf("log is missing %q: %q", want, text)
		}
	}
	// The log is the one artifact a user is likely to paste into an issue, so a
	// credential reaching it is worse than having no log at all.
	if strings.Contains(text, "sk-secret") {
		t.Fatalf("log leaked the API key: %q", text)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("log mode = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestLoggingRunnerDoesNotPersistPrivateInputOrOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	inner := &recordingInner{result: Result{ExitCode: 0, Stdout: "private recommendation response"}}
	runner := LoggingRunner{Inner: inner, Dir: dir}
	if _, err := RunPrivateInput(context.Background(), runner, []string{"codex", "exec", "-"}, nil, time.Minute, "private recommendation need"); err != nil {
		t.Fatal(err)
	}
	entry, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(entry)
	if inner.input != "private recommendation need" {
		t.Fatalf("private input did not reach the runner: %q", inner.input)
	}
	if strings.Contains(text, "private recommendation") {
		t.Fatalf("private input or output reached the log: %q", text)
	}
	if !strings.Contains(text, "private-run codex exec -") || !strings.Contains(text, "exit 0") {
		t.Fatalf("private run metadata is missing: %q", text)
	}
}

// The launch button opens a window through Start, and that is precisely the
// call with no console to report into. It must reach the log too.
func TestLoggingRunnerLogsLaunchesAndStaysTransparent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	inner := &recordingInner{}
	launcher, ok := AsLauncher(LoggingRunner{Inner: inner, Dir: dir})
	if !ok {
		t.Fatal("AsLauncher did not see through the logging decorator")
	}
	if err := launcher.Start([]string{"powershell", "-NoExit"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(inner.started) != 1 {
		t.Fatalf("inner did not receive the launch: %#v", inner.started)
	}
	entry, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry), "launch powershell -NoExit") {
		t.Fatalf("log = %q", entry)
	}

	// A runner that cannot open a window must still answer false through the
	// decorator, or the caller reports a generic failure instead of saying this
	// build has no launcher.
	if _, ok := AsLauncher(LoggingRunner{Inner: nonLauncher{}, Dir: dir}); ok {
		t.Fatal("AsLauncher claimed a launcher for a runner without Start")
	}
}

type nonLauncher struct{}

func (nonLauncher) LookPath(string) (string, bool) { return "", false }
func (nonLauncher) Run(context.Context, []string, map[string]string, time.Duration) (Result, error) {
	return Result{}, nil
}

func TestLoggingRunnerWithoutDirWritesNothing(t *testing.T) {
	directory := t.TempDir()
	runner := LoggingRunner{Inner: &recordingInner{}, Dir: ""}
	if _, err := runner.Run(context.Background(), []string{"npm", "--version"}, nil, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("an empty Dir still wrote %d entries", len(entries))
	}
}

// PATH injection has to survive the decorator: WithManagedPath goes through
// EnvSetter, and a decorator that dropped it would make a managed npm look
// missing and trigger a second runtime download.
func TestLoggingRunnerForwardsEnvironment(t *testing.T) {
	runner := LoggingRunner{Inner: OSRunner{Env: map[string]string{"PATH": "/nowhere"}}}
	updated, ok := runner.WithEnvironment(map[string]string{"PATH": "/managed"}).(LoggingRunner)
	if !ok {
		t.Fatal("WithEnvironment did not preserve the logging decorator")
	}
	inner, ok := updated.Inner.(OSRunner)
	if !ok || inner.Env["PATH"] != "/managed" {
		t.Fatalf("inner env = %#v", updated.Inner)
	}
}
