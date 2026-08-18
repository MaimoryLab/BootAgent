//go:build wails

package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/binding"
)

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
