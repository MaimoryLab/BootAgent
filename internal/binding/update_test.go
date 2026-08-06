package binding

import (
	"context"
	"errors"
	"reflect"
	"testing"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/process"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

type updateBackendFake struct {
	check              func(context.Context) (*updater.Release, error)
	downloadAndInstall func(context.Context) error
	restart            func(context.Context) error
}

func (f *updateBackendFake) Check(ctx context.Context) (*updater.Release, error) {
	return f.check(ctx)
}

func (f *updateBackendFake) DownloadAndInstall(ctx context.Context) error {
	return f.downloadAndInstall(ctx)
}

func (f *updateBackendFake) Restart(ctx context.Context) error {
	return f.restart(ctx)
}

func TestUpdateServiceCheckDisabledAndCurrent(t *testing.T) {
	for name, service := range map[string]*UpdateService{
		"nil service": nil,
		"nil backend": NewUpdateService(nil),
		"current": NewUpdateService(&updateBackendFake{check: func(context.Context) (*updater.Release, error) {
			return nil, nil
		}}),
	} {
		t.Run(name, func(t *testing.T) {
			version, err := service.Check(context.Background())
			if err != nil || version != "" {
				t.Fatalf("Check() = %q, %v", version, err)
			}
		})
	}

	for name, call := range map[string]func(*UpdateService) error{
		"download": func(service *UpdateService) error { return service.DownloadAndInstall(context.Background()) },
		"restart":  func(service *UpdateService) error { return service.Restart(context.Background()) },
	} {
		t.Run(name+" not configured", func(t *testing.T) {
			err := call(NewUpdateService(nil))
			got := oneerrors.As(err)
			if got.Message != "Update service is not configured" || got.Code != oneerrors.InternalError || got.Status != 501 {
				t.Fatalf("error = %#v", got)
			}
		})
	}
}

func TestUpdateServiceDelegatesWithCallerContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "caller")
	var calls []string
	backend := &updateBackendFake{
		check: func(got context.Context) (*updater.Release, error) {
			if got != ctx {
				t.Fatalf("Check context = %v, want caller context", got)
			}
			calls = append(calls, "check")
			return &updater.Release{Version: "1.2.3"}, nil
		},
		downloadAndInstall: func(got context.Context) error {
			if got != ctx {
				t.Fatalf("DownloadAndInstall context = %v, want caller context", got)
			}
			calls = append(calls, "download")
			return nil
		},
		restart: func(got context.Context) error {
			if got != ctx {
				t.Fatalf("Restart context = %v, want caller context", got)
			}
			calls = append(calls, "restart")
			return nil
		},
	}
	service := NewUpdateService(backend)

	version, err := service.Check(ctx)
	if err != nil || version != "1.2.3" {
		t.Fatalf("Check() = %q, %v", version, err)
	}
	if err := service.DownloadAndInstall(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"check", "download", "restart"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestUpdateServiceRejectsCancelledContextBeforeDelegation(t *testing.T) {
	calls := 0
	backend := &updateBackendFake{
		check:              func(context.Context) (*updater.Release, error) { calls++; return nil, nil },
		downloadAndInstall: func(context.Context) error { calls++; return nil },
		restart:            func(context.Context) error { calls++; return nil },
	}
	service := NewUpdateService(backend)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, checkErr := service.Check(ctx)
	errs := []error{checkErr, service.DownloadAndInstall(ctx), service.Restart(ctx)}
	for _, err := range errs {
		got := oneerrors.As(err)
		if got.Code != oneerrors.Timeout || got.Message != "Request was cancelled" || !got.Retryable || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %#v, cause = %v", got, errors.Unwrap(got))
		}
	}
	if calls != 0 {
		t.Fatalf("backend calls = %d, want 0", calls)
	}
}

func TestUpdateServiceConvertsBackendFailures(t *testing.T) {
	tests := []struct {
		name    string
		message string
		call    func(*UpdateService) error
	}{
		{"check", "Unable to check for updates", func(service *UpdateService) error { _, err := service.Check(context.Background()); return err }},
		{"download", "Unable to download the OneAgent update", func(service *UpdateService) error { return service.DownloadAndInstall(context.Background()) }},
		{"restart", "Unable to restart OneAgent for update", func(service *UpdateService) error { return service.Restart(context.Background()) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("private backend detail")
			service := NewUpdateService(&updateBackendFake{
				check:              func(context.Context) (*updater.Release, error) { return nil, cause },
				downloadAndInstall: func(context.Context) error { return cause },
				restart:            func(context.Context) error { return cause },
			})
			err := test.call(service)
			got := oneerrors.As(err)
			if got.Code != oneerrors.InternalError || got.Message != test.message || got.Status != 500 || !got.Retryable || !errors.Is(err, cause) {
				t.Fatalf("error = %#v, cause = %v", got, errors.Unwrap(got))
			}

			cancelled := NewUpdateService(&updateBackendFake{
				check:              func(context.Context) (*updater.Release, error) { return nil, context.DeadlineExceeded },
				downloadAndInstall: func(context.Context) error { return context.DeadlineExceeded },
				restart:            func(context.Context) error { return context.DeadlineExceeded },
			})
			err = test.call(cancelled)
			got = oneerrors.As(err)
			if got.Code != oneerrors.Timeout || got.Message != test.message+" was cancelled" || !got.Retryable || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancellation error = %#v, cause = %v", got, errors.Unwrap(got))
			}
		})
	}
}

func TestUpdateServiceProgressOutput(t *testing.T) {
	want := process.Output{Kind: "progress", Target: UpdateProgressTarget, Received: 12, Total: 0}
	progress := updater.Progress{Written: 12}
	for name, payload := range map[string]any{"value": progress, "pointer": &progress} {
		t.Run(name, func(t *testing.T) {
			got, ok := UpdateProgressOutput(payload)
			if !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("UpdateProgressOutput() = %#v, %t", got, ok)
			}
		})
	}
	var nilProgress *updater.Progress
	for name, payload := range map[string]any{"unrelated": "progress", "nil pointer": nilProgress} {
		t.Run(name, func(t *testing.T) {
			if got, ok := UpdateProgressOutput(payload); ok || !reflect.DeepEqual(got, process.Output{}) {
				t.Fatalf("UpdateProgressOutput() = %#v, %t", got, ok)
			}
		})
	}
}
