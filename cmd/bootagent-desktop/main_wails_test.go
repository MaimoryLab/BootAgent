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
	err error
}

func (b restartBackend) Restart(context.Context) error { return b.err }

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
			updater := quitAwareUpdater{UpdateBackend: restartBackend{err: test.err}, quitting: &quitting}

			if err := updater.Restart(context.Background()); !errors.Is(err, test.err) {
				t.Fatalf("Restart() error = %v, want %v", err, test.err)
			}
			if got := quitting.Load(); got != test.want {
				t.Fatalf("quitting = %v, want %v", got, test.want)
			}
		})
	}
}
