package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A command that prints nothing but keeps writing under a watched path is
// working, not hung -- the shape of npm fetching a large tarball. With only
// output to go on, the watchdog stopped it at the stall timeout.
func TestOSRunnerTreatsGrowthUnderAWatchedPathAsActivity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "download.part")
	ctx := WithActivityPaths(context.Background(), dir)
	runner := OSRunner{StallTimeout: 400 * time.Millisecond}
	// Silent for 1.5s -- several stall timeouts -- while the file grows.
	script := `i=0; while [ $i -lt 15 ]; do printf x >> "$1"; sleep 0.1; i=$((i+1)); done`
	result, err := runner.Run(ctx, []string{"/bin/sh", "-c", script, "sh", target}, nil, time.Minute)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: the file grew the whole time", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d", result.ExitCode)
	}
}

// Watching a path must not keep a genuinely hung command alive.
func TestOSRunnerStillStopsASilentCommandWhoseWatchedPathIsIdle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing"), []byte("left by an earlier install"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := WithActivityPaths(context.Background(), dir, filepath.Join(dir, "does-not-exist"))
	runner := OSRunner{StallTimeout: 300 * time.Millisecond}
	started := time.Now()
	_, err := runner.Run(ctx, []string{"/bin/sh", "-c", "sleep 30"}, nil, time.Minute)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("Run() error = %v, want ErrStalled", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("stall took %s to detect", elapsed.Round(time.Millisecond))
	}
}

func TestWithActivityPathsAccumulatesAndIgnoresBlanks(t *testing.T) {
	ctx := WithActivityPaths(context.Background(), " ", "")
	if got := activityPaths(ctx); len(got) != 0 {
		t.Fatalf("blank paths = %v, want none", got)
	}
	ctx = WithActivityPaths(ctx, "/a")
	ctx = WithActivityPaths(ctx, "/b/../c")
	if got := activityPaths(ctx); len(got) != 2 || got[0] != filepath.Clean("/a") || got[1] != filepath.Clean("/c") {
		t.Fatalf("paths = %v, want [/a /c]", got)
	}
}
