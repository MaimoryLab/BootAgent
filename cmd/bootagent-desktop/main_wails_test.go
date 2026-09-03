//go:build wails

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/binding"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

type updateProviderFake struct {
	checks, downloads int
	// checkErr fails Check, and noRelease answers "nothing newer" -- the two
	// outcomes the fallback has to tell apart.
	checkErr  error
	noRelease bool
}

func (p *updateProviderFake) Name() string { return "fake" }
func (p *updateProviderFake) Check(context.Context, updater.CheckRequest) (*updater.Release, error) {
	p.checks++
	if p.checkErr != nil {
		return nil, p.checkErr
	}
	if p.noRelease {
		return nil, nil
	}
	return &updater.Release{}, nil
}
func (p *updateProviderFake) Download(context.Context, *updater.Release, io.Writer, func(int64, int64)) error {
	p.downloads++
	return nil
}

func TestUpdateProviderKeepsCheckedSourceForDownload(t *testing.T) {
	official, mirror := &updateProviderFake{}, &updateProviderFake{}
	preferMirror := true
	provider := updateProvider{official: official, mirror: mirror, preferMirror: func(context.Context) bool { return preferMirror }}

	release, err := provider.Check(context.Background(), updater.CheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	preferMirror = false
	if err := provider.Download(context.Background(), release, &bytes.Buffer{}, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	if mirror.checks != 1 || mirror.downloads != 1 || official.checks != 0 || official.downloads != 0 {
		t.Fatalf("official checks/downloads = %d/%d, mirror = %d/%d", official.checks, official.downloads, mirror.checks, mirror.downloads)
	}
}

// Gitee answered 403 on roughly a third of consecutive SHA256SUMS fetches and is
// missing the windows/arm64 OTA asset outright. Both surfaced as a Check error,
// which the automatic updater treats as "no update", so mirror users silently
// stopped being offered releases.
func TestUpdateCheckFallsBackToOfficialWhenTheMirrorFails(t *testing.T) {
	mirrorErr := errors.New("checksum sidecar HTTP 403")
	official, mirror := &updateProviderFake{}, &updateProviderFake{checkErr: mirrorErr}
	provider := updateProvider{official: official, mirror: mirror, preferMirror: func(context.Context) bool { return true }}

	release, err := provider.Check(context.Background(), updater.CheckRequest{})
	if err != nil {
		t.Fatalf("Check() after a mirror failure = %v, want the official result", err)
	}
	if mirror.checks != 1 || official.checks != 1 {
		t.Fatalf("mirror checks = %d, official checks = %d, want 1 and 1", mirror.checks, official.checks)
	}
	// The download has to follow the host that answered, or it would look for the
	// official asset on the mirror.
	if err := provider.Download(context.Background(), release, &bytes.Buffer{}, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	if official.downloads != 1 || mirror.downloads != 0 {
		t.Fatalf("official downloads = %d, mirror downloads = %d, want 1 and 0", official.downloads, mirror.downloads)
	}
}

func TestUpdateCheckReportsBothFailuresWhenNeitherSourceAnswers(t *testing.T) {
	mirrorErr, officialErr := errors.New("mirror is unreachable"), errors.New("official is unreachable")
	official, mirror := &updateProviderFake{checkErr: officialErr}, &updateProviderFake{checkErr: mirrorErr}
	provider := updateProvider{official: official, mirror: mirror, preferMirror: func(context.Context) bool { return true }}

	release, err := provider.Check(context.Background(), updater.CheckRequest{})
	if release != nil {
		t.Fatalf("Check() release = %#v, want nil when both sources fail", release)
	}
	// Joined rather than replaced: the mirror's failure is what the user's
	// configured route did, and updateError matches sentinels through the join.
	for _, want := range []error{mirrorErr, officialErr} {
		if !errors.Is(err, want) {
			t.Errorf("Check() error = %v, want it to wrap %v", err, want)
		}
	}
}

// A mirror that answers "nothing newer" is compared with official metadata so
// a lagging mirror cannot hide a release.
func TestUpdateCheckComparesOfficialWhenTheMirrorHasNoUpdate(t *testing.T) {
	official, mirror := &updateProviderFake{noRelease: true}, &updateProviderFake{noRelease: true}
	provider := updateProvider{official: official, mirror: mirror, preferMirror: func(context.Context) bool { return true }}

	release, err := provider.Check(context.Background(), updater.CheckRequest{})
	if release != nil || err != nil {
		t.Fatalf("Check() = %#v, %v, want nil, nil", release, err)
	}
	if official.checks != 1 {
		t.Errorf("official checks = %d, want 1 to detect mirror lag", official.checks)
	}
}

// Cancelling is the caller's decision. Falling back would reopen the request that
// was just abandoned, and the task centre's cancel button would not stick.
func TestUpdateCheckDoesNotFallBackAfterCancellation(t *testing.T) {
	official, mirror := &updateProviderFake{}, &updateProviderFake{checkErr: context.Canceled}
	provider := updateProvider{official: official, mirror: mirror, preferMirror: func(context.Context) bool { return true }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := provider.Check(ctx, updater.CheckRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Check() error = %v, want context.Canceled", err)
	}
	if official.checks != 0 {
		t.Errorf("official checks = %d, want 0 after cancellation", official.checks)
	}
}

// An official API outage (including GitHub rate limiting) should not prevent an
// update when the configured mirror can answer. The selected source is recorded
// on the release, so a later download still uses the source that answered.
func TestUpdateCheckFallsBackToMirrorWhenOfficialFails(t *testing.T) {
	officialErr := errors.New("official is unreachable")
	official, mirror := &updateProviderFake{checkErr: officialErr}, &updateProviderFake{}
	provider := updateProvider{official: official, mirror: mirror, preferMirror: func(context.Context) bool { return false }}

	if _, err := provider.Check(context.Background(), updater.CheckRequest{}); err != nil {
		t.Fatalf("Check() after an official failure = %v, want the mirror result", err)
	}
	if mirror.checks != 1 {
		t.Errorf("mirror checks = %d, want 1", mirror.checks)
	}
}

type restartBackend struct {
	binding.UpdateBackend
	err   error
	calls int
}

func (b *restartBackend) Restart(context.Context) error {
	b.calls++
	return b.err
}

// The Dock's Quit, Cmd+Q, the app menu and a logout all arrive here. Answering
// false turns into NSTerminateCancel on macOS, which is why the Dock's Quit left
// the process running: the flag it gated on was only ever set by the tray's own
// Quit item.
func TestQuitIsAcceptedWhateverAskedForIt(t *testing.T) {
	var quitting atomic.Bool
	if !acceptQuit(&quitting) {
		t.Fatal("a quit request was refused; the Dock and Cmd+Q cannot quit the app")
	}
	// The WindowClosing hook reads this to tell a real quit from a window close,
	// so without it the window would be hidden mid-shutdown instead of closing.
	if !quitting.Load() {
		t.Fatal("quit intent was not recorded")
	}
}

// The tray's Quit sets the flag before asking, and a restart sets it too; asking
// again must not undo either.
func TestQuitStaysAcceptedWhenAlreadyQuitting(t *testing.T) {
	var quitting atomic.Bool
	quitting.Store(true)
	if !acceptQuit(&quitting) || !quitting.Load() {
		t.Fatal("an in-flight quit was reversed")
	}
}

func TestQuitAwareUpdater(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "restart", want: true},
		{name: "failure", err: errors.New("restart failed"), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var quitting atomic.Bool
			var restarting atomic.Bool
			backend := &restartBackend{err: test.err}
			updater := quitAwareUpdater{UpdateBackend: backend, quitting: &quitting, restarting: &restarting}

			if err := updater.Restart(context.Background()); !errors.Is(err, test.err) {
				t.Fatalf("Restart() error = %v, want %v", err, test.err)
			}
			if got := quitting.Load(); got != test.want {
				t.Fatalf("quitting = %v, want %v", got, test.want)
			}
			if err := updater.Restart(context.Background()); !errors.Is(err, test.err) {
				t.Fatalf("second Restart() error = %v, want %v", err, test.err)
			}
			wantCalls := 1
			if test.err != nil {
				wantCalls = 2
			}
			if backend.calls != wantCalls {
				t.Fatalf("backend Restart() calls = %d, want %d", backend.calls, wantCalls)
			}
		})
	}
}
