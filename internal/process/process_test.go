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
	// Interleaves both streams so the runner's stdout and stderr copiers are
	// active at the same time. Real installs look like this — npm reports progress
	// on stderr while printing results on stdout.
	if os.Getenv("ONEAGENT_PROCESS_BOTH_STREAMS") == "1" {
		for index := 0; index < 50; index++ {
			os.Stdout.WriteString("o")
			os.Stderr.WriteString("e")
		}
		os.Exit(0)
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
	/* These cases re-exec the test binary as the helper process, so under `go test
	   -cover` the child inherits the coverage instrumentation. Without a place to
	   write its profile it prints "warning: GOCOVERDIR not set" to stderr, which
	   lands in the captured output and in the listener — failing assertions about
	   what the command produced for a reason that has nothing to do with the
	   runner. Giving the child a scratch directory keeps its stderr its own. */
	runner := New(map[string]string{
		"ONEAGENT_PROCESS_HELPER": "1",
		"GOCOVERDIR":              t.TempDir(),
	})
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

func TestOSRunnerStreamsOutputWithoutChangingResult(t *testing.T) {
	runner := helperRunner(t)
	outputs := make([]Output, 0)
	result, err := runner.RunWithOutput(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"ONEAGENT_PROCESS_VALUE": "stream-value",
	}, helperTimeout, func(output Output) { outputs = append(outputs, output) })
	if err != nil || result.ExitCode != 0 || result.Stdout != "stream-value" {
		t.Fatalf("streamed process result = %#v, err=%v", result, err)
	}
	if len(outputs) != 1 || outputs[0].Kind != "output" || outputs[0].Stream != "stdout" || outputs[0].Text != "stream-value" {
		t.Fatalf("streamed outputs = %#v", outputs)
	}
}

// The listener here appends without locking, exactly as the install runtime's
// does (internal/app/install.go redacts and forwards). stdout and stderr are
// copied by separate goroutines, so without serialisation inside the runner this
// is a data race in production, not just in a test — it surfaces whenever a
// command writes to both streams, which npm does on every install.
func TestOSRunnerSerialisesListenerAcrossStreams(t *testing.T) {
	runner := helperRunner(t)
	outputs := make([]Output, 0)
	result, err := runner.RunWithOutput(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"ONEAGENT_PROCESS_BOTH_STREAMS": "1",
	}, helperTimeout, func(output Output) { outputs = append(outputs, output) })
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("interleaved process result = %#v, err=%v", result, err)
	}
	var stdout, stderr int
	for _, output := range outputs {
		switch output.Stream {
		case "stdout":
			stdout += len(output.Text)
		case "stderr":
			stderr += len(output.Text)
		}
	}
	// Both streams have to reach the listener; asserting only the total would
	// pass even if one stream's chunks were being dropped.
	if stdout != 50 || stderr != 50 {
		t.Fatalf("streamed %d stdout and %d stderr bytes, want 50 of each", stdout, stderr)
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
