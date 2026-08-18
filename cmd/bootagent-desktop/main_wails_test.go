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
}

func (p *updateProviderFake) Name() string { return "fake" }
func (p *updateProviderFake) Check(context.Context, updater.CheckRequest) (*updater.Release, error) {
	p.checks++
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

type restartBackend struct {
	binding.UpdateBackend
	err   error
	calls int
}

func (b *restartBackend) Restart(context.Context) error {
	b.calls++
	return b.err
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
