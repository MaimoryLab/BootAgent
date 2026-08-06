package binding

import (
	"context"
	"crypto/sha256"
	"errors"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/process"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

const UpdateProgressTarget = "oneagent-update"

type UpdateBackend interface {
	Check(context.Context) (*updater.Release, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
}

type UpdateService struct {
	backend UpdateBackend
}

func NewUpdateService(backend UpdateBackend) *UpdateService {
	return &UpdateService{backend: backend}
}

func (s *UpdateService) Check(ctx context.Context) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if s == nil || s.backend == nil {
		return "", nil
	}
	release, err := s.backend.Check(ctx)
	if err != nil {
		return "", updateError(err, "Unable to check for updates")
	}
	if release == nil {
		return "", nil
	}
	if release.Verification == nil || release.Verification.DigestAlgo != "sha256" || len(release.Verification.Digest) != sha256.Size {
		return "", updateError(errors.New("invalid update verification"), "Unable to check for updates")
	}
	return release.Version, nil
}

func (s *UpdateService) DownloadAndInstall(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.backend == nil {
		return notReady("Update service is not configured")
	}
	if err := s.backend.DownloadAndInstall(ctx); err != nil {
		return updateError(err, "Unable to download the OneAgent update")
	}
	return nil
}

func (s *UpdateService) Restart(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.backend == nil {
		return notReady("Update service is not configured")
	}
	if err := s.backend.Restart(ctx); err != nil {
		return updateError(err, "Unable to restart OneAgent for update")
	}
	return nil
}

func updateError(err error, message string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return oneerrors.New(oneerrors.Timeout, message+" was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return oneerrors.New(oneerrors.InternalError, message, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
}

func UpdateProgressOutput(payload any) (process.Output, bool) {
	var progress updater.Progress
	switch value := payload.(type) {
	case updater.Progress:
		progress = value
	case *updater.Progress:
		if value == nil {
			return process.Output{}, false
		}
		progress = *value
	default:
		return process.Output{}, false
	}
	return process.Output{Kind: "progress", Target: UpdateProgressTarget, Received: progress.Written, Total: progress.Total}, true
}
