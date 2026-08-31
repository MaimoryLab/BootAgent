package process

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOutputIncludesDownloadSourceAndPhase(t *testing.T) {
	payload, err := json.Marshal(Output{Kind: "source", Source: "https://example.test/runtime.zip"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(payload); got != `{"kind":"source","source":"https://example.test/runtime.zip"}` {
		t.Fatalf("source output JSON = %s", got)
	}
	payload, err = json.Marshal(Output{Kind: "phase", Phase: "verified"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(payload); got != `{"kind":"phase","phase":"verified"}` {
		t.Fatalf("phase output JSON = %s", got)
	}
}

func TestProcessHelper(_ *testing.T) {
	if os.Getenv("TEST_PROCESS_HELPER") != "1" {
		return
	}
	if os.Getenv("TEST_PROCESS_EXIT") == "1" {
		os.Stderr.WriteString("helper stderr")
		os.Exit(7)
	}
	if os.Getenv("TEST_PROCESS_WAIT") == "1" {
		if os.Getenv("TEST_PROCESS_READY") == "1" {
			os.Stdout.WriteString("ready")
		}
		// Long enough that the caller's deadline always fires first; the runner
		// kills this process, so the sleep never runs to completion.
		<-time.After(10 * time.Second)
	}
	// Keeps talking for longer than the caller's stall timeout while never going
	// quiet for as long as it. A healthy slow install looks like this, and it must
	// not be killed.
	if os.Getenv("TEST_PROCESS_DRIP") == "1" {
		// Total runtime (~3s) must exceed the caller's stall window while each
		// individual gap (300ms) stays well inside it. Otherwise the case would
		// pass simply by finishing before the watchdog first looked.
		for range 10 {
			os.Stdout.WriteString("tick ")
			<-time.After(300 * time.Millisecond)
		}
		os.Exit(0)
	}
	// Writes past MaxOutputBytes so boundedBuffer starts discarding and the
	// listener stops being called, then keeps writing. Liveness must still be
	// observed, or a chatty install dies once it crosses 1 MB.
	if os.Getenv("TEST_PROCESS_FLOOD") == "1" {
		chunk := strings.Repeat("x", 64*1024)
		// 1.5 MB up front to push boundedBuffer past MaxOutputBytes, so everything
		// after this point is written while the buffer accepts nothing and the
		// listener is no longer called.
		for range 24 {
			os.Stdout.WriteString(chunk)
		}
		// Then keep writing past the caller's stall window. Only an activity
		// signal taken before the buffer decides what to keep can see these.
		for range 10 {
			os.Stdout.WriteString(chunk)
			<-time.After(300 * time.Millisecond)
		}
		os.Exit(0)
	}
	// Interleaves both streams so the runner's stdout and stderr copiers are
	// active at the same time. Real installs look like this — npm reports progress
	// on stderr while printing results on stdout.
	if os.Getenv("TEST_PROCESS_BOTH_STREAMS") == "1" {
		for range 50 {
			os.Stdout.WriteString("o")
			os.Stderr.WriteString("e")
		}
		os.Exit(0)
	}
	os.Stdout.WriteString(os.Getenv("TEST_PROCESS_VALUE"))
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
	runner := OSRunner{Env: map[string]string{
		"TEST_PROCESS_HELPER": "1",
		"GOCOVERDIR":          t.TempDir(),
	}}
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
		"TEST_PROCESS_VALUE": "safe-value",
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
		"TEST_PROCESS_VALUE": "stream-value",
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
		"TEST_PROCESS_BOTH_STREAMS": "1",
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
		"TEST_PROCESS_EXIT": "1",
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

func TestOSRunnerKillsARunningProcessWhenCancelled(t *testing.T) {
	runner := helperRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := runner.RunWithOutput(ctx, []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
			"TEST_PROCESS_WAIT":  "1",
			"TEST_PROCESS_READY": "1",
		}, helperTimeout, func(output Output) {
			if output.Text == "ready" {
				select {
				case started <- struct{}{}:
				default:
				}
			}
		})
		done <- err
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("helper process did not start")
	}
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("cancelled running process error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled helper process was not killed")
	}
}

func TestOSRunnerHonorsTimeout(t *testing.T) {
	runner := helperRunner(t)
	_, err := runner.Run(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"TEST_PROCESS_WAIT": "1",
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

func TestMacOSBundleRestoresLoginShellPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS login-shell behavior is platform-specific")
	}
	if !macOSBundleExecutable("/Applications/BootAgent.app/Contents/MacOS/bootagent-desktop") || macOSBundleExecutable("/tmp/bootagent-desktop") {
		t.Fatal("macOS bundle executable detection is incorrect")
	}
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'startup noise\\036/custom/bin:/usr/bin\\037trailing noise'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if path := loginShellPath(map[string]string{"SHELL": shell, "PATH": "/usr/bin"}); path != "/custom/bin:/usr/bin" {
		t.Fatalf("login shell PATH = %q", path)
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

// The point of stall detection: a command that keeps producing output runs to
// completion even though it takes far longer than the stall window, because the
// limit is on silence, not on elapsed time.
func TestOSRunnerLetsASlowButTalkingCommandFinish(t *testing.T) {
	runner := helperRunner(t)
	// Has to absorb process start-up, not just the gaps between writes: a
	// race-instrumented helper needs a few hundred ms before it prints anything,
	// and that silence counts against the stall window like any other.
	runner.StallTimeout = 2 * time.Second
	result, err := runner.RunWithOutput(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"TEST_PROCESS_DRIP": "1",
	}, helperTimeout, nil)
	if err != nil {
		t.Fatalf("slow but talking command error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("slow but talking command result = %#v", result)
	}
	// 12 ticks at 50ms is ~600ms, comfortably past the 200ms stall window, so a
	// pass here cannot be explained by the command finishing before the watchdog
	// ever looked.
	if count := strings.Count(result.Stdout, "tick"); count != 10 {
		t.Fatalf("drip output had %d ticks, want all 10: %q", count, result.Stdout)
	}
}

// Guards the trap this change is most likely to introduce: boundedBuffer stops
// accepting at MaxOutputBytes and streamWriter then stops calling the listener,
// so keying liveness off accepted bytes or off listener calls would kill a
// healthy command the moment its output passed 1 MB.
func TestOSRunnerDoesNotMistakeAFloodedBufferForASilentCommand(t *testing.T) {
	runner := helperRunner(t)
	// Same start-up allowance as the drip case above.
	runner.StallTimeout = 2 * time.Second
	events := 0
	result, err := runner.RunWithOutput(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"TEST_PROCESS_FLOOD": "1",
	}, helperTimeout, func(Output) { events++ })
	if err != nil {
		t.Fatalf("flooding command error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("flooding command result exit = %d", result.ExitCode)
	}
	// Confirms the command really did exceed the buffer, so this test would in
	// fact catch a liveness signal that depends on accepted bytes.
	if !strings.Contains(result.Stdout, "[output truncated]") {
		t.Fatal("helper did not exceed MaxOutputBytes, so the case proves nothing")
	}
	if events == 0 {
		t.Fatal("listener never fired")
	}
}

// A genuinely hung command still ends, and reports ErrStalled rather than the
// bare context error, so callers can tell a stall from a user cancellation.
func TestOSRunnerStopsACommandThatGoesSilent(t *testing.T) {
	runner := helperRunner(t)
	runner.StallTimeout = 300 * time.Millisecond
	started := time.Now()
	_, err := runner.RunWithOutput(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"TEST_PROCESS_WAIT":  "1",
		"TEST_PROCESS_READY": "1",
	}, helperTimeout, nil)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("stalled command error = %v, want ErrStalled", err)
	}
	// The helper sleeps 10s and helperTimeout is 60s, so finishing quickly is the
	// evidence that the stall watchdog ended it rather than either deadline.
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("stall detection took %v, so a deadline ended this instead", elapsed)
	}
}

// Stall detection must not change what cancellation looks like. The Task Center's
// stop button is the only way out of a long install now that the wall-clock
// budget is an hour, so this staying context.Canceled is load-bearing.
func TestOSRunnerStillReportsCancellationDistinctlyFromAStall(t *testing.T) {
	runner := helperRunner(t)
	runner.StallTimeout = 30 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := runner.RunWithOutput(ctx, []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
			"TEST_PROCESS_WAIT":  "1",
			"TEST_PROCESS_READY": "1",
		}, helperTimeout, func(output Output) {
			if strings.Contains(output.Text, "ready") {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		})
		done <- err
	}()
	select {
	case <-ready:
		cancel()
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("helper process did not start")
	}
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled command error = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrStalled) {
		t.Fatal("a user cancellation was reported as a stall")
	}
}

// stallTimeout's contract: zero takes the default, negative disables. Disabling
// matters for the version probes, which have their own short deadline and would
// be pointless to also watch for silence.
func TestStallTimeoutZeroTakesTheDefaultAndNegativeDisables(t *testing.T) {
	if got := (OSRunner{}).stallTimeout(); got != DefaultStallTimeout {
		t.Fatalf("zero stallTimeout = %v, want %v", got, DefaultStallTimeout)
	}
	if got := (OSRunner{StallTimeout: -1}).stallTimeout(); got >= 0 {
		t.Fatalf("negative stallTimeout = %v, want it preserved as negative", got)
	}
	runner := helperRunner(t)
	runner.StallTimeout = -1
	// With the watchdog off, a silent command is bounded only by the deadline.
	_, err := runner.Run(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"TEST_PROCESS_WAIT": "1",
	}, 300*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("disabled watchdog error = %v, want DeadlineExceeded", err)
	}
}

// A stall must still hand back what the command managed to say. The whole point
// of distinguishing a stall from a timeout is that the user gets a diagnosable
// failure, and the last few lines before a command went quiet are the only
// evidence of where it got stuck -- an npm registry URL, a partial download, a
// permissions warning. Returning a bare error throws that away.
func TestOSRunnerKeepsOutputProducedBeforeAStall(t *testing.T) {
	runner := helperRunner(t)
	runner.StallTimeout = 300 * time.Millisecond
	result, err := runner.RunWithOutput(context.Background(), []string{os.Args[0], "-test.run=TestProcessHelper"}, map[string]string{
		"TEST_PROCESS_WAIT":  "1",
		"TEST_PROCESS_READY": "1",
	}, helperTimeout, nil)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("stalled command error = %v, want ErrStalled", err)
	}
	if !strings.Contains(result.Stdout, "ready") {
		t.Fatalf("stdout before the stall was dropped: Stdout = %q", result.Stdout)
	}
}
