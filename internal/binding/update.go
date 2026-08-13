package binding

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/process"
	"github.com/MaimoryLab/BootAgent/internal/version"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

const UpdateProgressTarget = "bootagent-update"

type UpdateBackend interface {
	Check(context.Context) (*updater.Release, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
	// DownloadedPath names the artifact staged by DownloadAndInstall, so the
	// service can reject one the helper cannot install.
	DownloadedPath() string
}

type UpdateService struct {
	backend UpdateBackend
	// executable resolves the running binary, so the service can tell whether
	// the installation can be replaced in place. Overridden in tests.
	executable func() (string, error)
}

func NewUpdateService(backend UpdateBackend) *UpdateService {
	return &UpdateService{backend: backend, executable: os.Executable}
}

func (s *UpdateService) Version() string {
	return version.Version
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
	// Reported here rather than at download time: the helper replaces the
	// application after this process exits, so a location it cannot write to
	// fails with no interface left to say so -- the app simply never comes back.
	// Refusing before the user spends a download is the only honest moment.
	if err := s.locationError(); err != nil {
		return "", err
	}
	return release.Version, nil
}

// locationError reports an installation the helper could not replace. A version
// is withheld rather than returned with a warning, because every caller treats a
// version as "an update is available" and offers to install it.
func (s *UpdateService) locationError() error {
	resolve := s.executable
	if resolve == nil {
		resolve = os.Executable
	}
	executable, err := resolve()
	if err != nil {
		// Nothing to conclude: leave the existing behaviour rather than
		// withholding an update from an installation that may be fine.
		return nil
	}
	switch checkUpdateLocation(executable) {
	case locationTranslocated:
		return oneerrors.New(
			oneerrors.UpdateLocationBlocked,
			"BootAgent is running from a disk image. Move BootAgent to the Applications folder, then check again.",
			oneerrors.WithStatus(409),
		)
	case locationUnwritable:
		return oneerrors.New(
			oneerrors.UpdateLocationBlocked,
			"BootAgent cannot update itself where it is installed. Move BootAgent to the Applications folder, then check again.",
			oneerrors.WithStatus(409),
		)
	}
	return nil
}

func (s *UpdateService) DownloadAndInstall(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.backend == nil {
		return notReady("Update service is not configured")
	}
	if err := s.backend.DownloadAndInstall(ctx); err != nil {
		return updateError(err, "Unable to download the BootAgent update")
	}
	// The helper swaps this artifact over the installed application after the
	// process exits, so an unusable one has to be caught here: once the user
	// accepts the restart there is no interface left to report the failure,
	// and a container file that lands on the bundle path leaves an
	// installation that cannot launch.
	if err := stagedArtifactError(s.backend.DownloadedPath()); err != nil {
		return oneerrors.New(
			oneerrors.UpdateNotInstallable,
			"The downloaded BootAgent update is not installable",
			oneerrors.WithStatus(500),
			oneerrors.WithCause(err),
		)
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
		return updateError(err, "Unable to restart BootAgent for update")
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
