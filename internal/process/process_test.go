package process

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	if os.Getenv("ONEAGENT_PROCESS_HELPER") != "1" {
		return
	}
	if os.Getenv("ONEAGENT_PROCESS_EXIT") == "1" {
		os.Stderr.WriteString("helper stderr")
		os.Exit(7)
	}
	if os.Getenv("ONEAGENT_PROCESS_WAIT") == "1" {
		// Long enough that the caller's deadline always fires first; the runner
		// kills this process, so the sleep never runs to completion.
		<-time.After(10 * time.Second)
	}
	os.Stdout.WriteString(os.Getenv("ONEAGENT_PROCESS_VALUE"))
	os.Exit(0)
}

// helperTimeout is generous on purpose. These cases re-exec the test binary as
// a helper process, and a race-instrumented binary needs well over a second to
// start; a tight budget here fails the run for timing rather than behavior.
// Cases that assert timeout handling set their own short deadline.
const helperTimeout = 60 * time.Second

func helperRunner(t *testing.T) OSRunner {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := New(map[string]string{"ONEAGENT_PROCESS_HELPER": "1"})
	runner.Lookup = func(command string) (string, bool) {
		if command == "helper" {
			return path, true
		}
		return "", false
	}
	return runner
}

func TestOSRunnerUsesArgvAndMergesEnvironment(t *testing.T) {
	runner := helperRunner(t)
	result, err := runner.Run(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"ONEAGENT_PROCESS_VALUE": "safe-value",
	}, helperTimeout)
	if err != nil || result.ExitCode != 0 || result.Stdout != "safe-value" {
		t.Fatalf("process result = %#v, err=%v", result, err)
	}
	if len(result.Args) != 2 || result.Args[1] != "-test.run=TestProcessHelper" {
		t.Fatalf("argv was changed: %#v", result.Args)
	}
}

func TestOSRunnerReturnsExitCodeAndCapturesOutput(t *testing.T) {
	runner := helperRunner(t)
	result, err := runner.Run(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"ONEAGENT_PROCESS_EXIT": "1",
	}, helperTimeout)
	if err != nil || result.ExitCode != 7 || result.Stderr != "helper stderr" {
		t.Fatalf("non-zero result = %#v, err=%v", result, err)
	}
}

func TestOSRunnerHonorsCancellationAndRejectsEmptyArgv(t *testing.T) {
	runner := helperRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runner.Run(ctx, []string{os.Args[0], "-test.run=TestProcessHelper"}, nil, helperTimeout)
	if err == nil || err != context.Canceled {
		t.Fatalf("cancelled process error = %v", err)
	}
	if _, err := runner.Run(context.Background(), nil, nil, helperTimeout); err == nil {
		t.Fatal("empty argv unexpectedly succeeded")
	}
}

func TestOSRunnerHonorsTimeout(t *testing.T) {
	runner := helperRunner(t)
	_, err := runner.Run(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"ONEAGENT_PROCESS_WAIT": "1",
	}, 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestOSRunnerLookPathCanBeInjected(t *testing.T) {
	runner := helperRunner(t)
	if path, ok := runner.LookPath("helper"); !ok || path == "" {
		t.Fatalf("injected lookup = %q, %v", path, ok)
	}
	if _, ok := runner.LookPath("missing"); ok {
		t.Fatal("missing command unexpectedly found")
	}
}

func TestBoundedBufferDoesNotBlockProducer(t *testing.T) {
	buffer := &boundedBuffer{limit: 4}
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 {
		t.Fatalf("bounded write = %d, %v", written, err)
	}
	if got := buffer.String(); got != "abcd\n[output truncated]" {
		t.Fatalf("bounded output = %q", got)
	}
}

func TestOSRunnerUsesExecutableWithoutShell(t *testing.T) {
	runner := helperRunner(t)
	result, err := runner.Run(context.Background(), []string{exec.Command("true").Path}, nil, helperTimeout)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("direct executable result = %#v, err=%v", result, err)
	}
}
