//go:build darwin

package desktopapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// realToolsRunner runs ditto and plutil for real -- the extraction and the plist
// read are the behavior under test -- and stubs the signing checks, which need a
// genuinely signed bundle to pass and are covered by their own scripted tests.
type realToolsRunner struct {
	calls [][]string
}

func (r *realToolsRunner) LookPath(string) (string, bool) { return "", false }

func (r *realToolsRunner) Start([]string, map[string]string) error { return nil }

func (r *realToolsRunner) Run(ctx context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	switch argv[0] {
	case "/usr/bin/ditto", "/usr/bin/plutil":
		result, err := process.OSRunner{}.Run(ctx, argv, nil, time.Minute)
		return result, err
	case "/usr/bin/codesign":
		if len(argv) > 1 && argv[1] == "-dv" {
			return process.Result{Args: argv, ExitCode: 0, Stderr: dshCodesignIdentity}, nil
		}
		return process.Result{Args: argv, ExitCode: 0}, nil
	case "/usr/sbin/spctl":
		return process.Result{Args: argv, ExitCode: 0, Stderr: "source=Notarized Developer ID\n"}, nil
	}
	return process.Result{Args: argv, ExitCode: 0}, nil
}

// The archive the vendor publishes has the .app at its root and a space in its
// name. The install has to extract it, find it, read its real Info.plist, and
// copy it under the same name.
func TestInstallDSHExtractsThePublishedArchiveLayout(t *testing.T) {
	archive, err := os.ReadFile("testdata/dsh-fixture.zip")
	if err != nil {
		t.Skipf("zip fixture unavailable: %v", err)
	}
	feed := dshFeedYAML("0.1.7-rc.2", dshMacZipURL, dshDigest(archive), int64(len(archive)))
	downloader := &routeDownloader{routes: map[string][]byte{dshMacFeedURL: feed, dshMacZipURL: archive}}
	applications := t.TempDir()
	runner := &realToolsRunner{}
	result, err := installDSH(context.Background(), Options{
		Home: t.TempDir(), Platform: platform.For("macos", "arm64"), Runner: runner, Downloader: downloader,
		SearchRoots: []string{t.TempDir()}, ApplicationDirs: []string{applications},
	})
	if err != nil {
		t.Fatalf("installDSH: %v\ncalls: %#v", err, runner.calls)
	}
	want := filepath.Join(applications, dshDesktopAppName)
	if result.Status != "installed" || result.App.Path != want {
		t.Fatalf("installDSH = %#v", result)
	}
	if result.App.Version == nil || *result.App.Version != "0.1.7-rc.2" {
		t.Fatalf("version read from the extracted plist = %v", result.App.Version)
	}
	// ditto copied the bundle for real, so it is there under the vendor's name.
	if _, err := os.Stat(filepath.Join(want, "Contents", "Info.plist")); err != nil {
		t.Fatalf("installed bundle is missing its plist: %v", err)
	}
	// And the temporary extraction directory did not survive.
	for _, call := range runner.calls {
		if call[0] == "/usr/bin/ditto" && call[1] == "-x" {
			if _, err := os.Stat(call[4]); !os.IsNotExist(err) {
				t.Errorf("extraction directory survived: stat %s = %v", call[4], err)
			}
		}
	}
}
