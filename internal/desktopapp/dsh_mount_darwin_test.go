//go:build darwin

package desktopapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// hdiutilRunner runs hdiutil for real and stubs everything else. The mount is the
// behavior under test, so faking hdiutil would test the fake.
type hdiutilRunner struct {
	mountPoints []string
	calls       [][]string
}

func (r *hdiutilRunner) LookPath(string) (string, bool) { return "", false }

func (r *hdiutilRunner) Start([]string, map[string]string) error { return nil }

func (r *hdiutilRunner) Run(ctx context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	if len(argv) > 0 && argv[0] == "/usr/bin/hdiutil" {
		if argv[1] == "attach" {
			for index, value := range argv {
				if value == "-mountpoint" && index+1 < len(argv) {
					r.mountPoints = append(r.mountPoints, argv[index+1])
				}
			}
		}
		output, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
		result := process.Result{Args: argv, Stdout: string(output)}
		if err != nil {
			result.ExitCode = 1
			result.Stderr = string(output)
		}
		return result, nil
	}
	// codesign, spctl and ditto all report success; the install then completes
	// without copying anything into a real Applications directory.
	return process.Result{Args: argv, ExitCode: 0}, nil
}

func mountedAt(t *testing.T, path string) bool {
	t.Helper()
	output, err := exec.Command("/sbin/mount").Output()
	if err != nil {
		t.Fatalf("read mount table: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// Gone from disk entirely, so it cannot be mounted.
		return false
	}
	return strings.Contains(string(output), resolved)
}

// installDSH used to attach the image and never detach it, at a mountpoint fixed
// at $TMPDIR/mount that every attempt shared. The mount outlived the process, and
// the deferred RemoveAll could not delete a mounted volume. Two installs in a row
// is what makes the leak visible: the second used to stack another mount on the
// same path.
func TestInstallDSHDetachesTheImageAndReusesNoMountpoint(t *testing.T) {
	image, err := os.ReadFile("testdata/dsh-fixture.dmg")
	if err != nil {
		t.Skipf("mount fixture unavailable: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(image)
	}))
	defer server.Close()

	var seen []string
	for attempt := range 2 {
		runner := &hdiutilRunner{}
		options := Options{
			Home:     t.TempDir(),
			Platform: platform.For("macos", "arm64"),
			Runner:   runner,
			// PreferMirror keeps dshURL from reaching the release API; the injected
			// client then answers the mirror URL from the fixture server, so the
			// host allowlist is still what judges the URL.
			PreferMirror:    true,
			Downloader:      dmgClient{base: server.URL},
			ApplicationDirs: []string{t.TempDir()},
			SearchRoots:     []string{t.TempDir()},
		}
		if _, err := installDSH(context.Background(), options); err != nil {
			t.Fatalf("attempt %d: installDSH: %v", attempt+1, err)
		}
		if len(runner.mountPoints) != 1 {
			t.Fatalf("attempt %d: mountpoints used = %v, want exactly one", attempt+1, runner.mountPoints)
		}
		mount := runner.mountPoints[0]
		if mountedAt(t, mount) {
			// Leave nothing behind for the next test even when this one fails.
			_, _ = exec.Command("/usr/bin/hdiutil", "detach", mount, "-force").CombinedOutput()
			t.Fatalf("attempt %d: %s is still mounted after installDSH", attempt+1, mount)
		}
		if _, err := os.Stat(mount); !os.IsNotExist(err) {
			t.Errorf("attempt %d: temporary directory survived: stat %s = %v", attempt+1, mount, err)
		}
		seen = append(seen, mount)
	}
	if seen[0] == seen[1] {
		t.Errorf("both installs used the same mountpoint %q; it must be per-install", seen[0])
	}
}

// dmgClient serves the fixture for the download while leaving the release API
// unused: the test supplies DownloadURL instead.
type dmgClient struct{ base string }

func (c dmgClient) Do(request *http.Request) (*http.Response, error) {
	redirected, err := http.NewRequestWithContext(request.Context(), request.Method, c.base, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(redirected)
}
