package binding

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

func availableRelease() *updater.Release {
	return &updater.Release{
		Version:      "0.5.2",
		Verification: &updater.Verification{DigestAlgo: "sha256", Digest: make([]byte, sha256.Size)},
	}
}

func checkableService(executable func() (string, error)) *UpdateService {
	service := NewUpdateService(&updateBackendFake{
		check: func(context.Context) (*updater.Release, error) { return availableRelease(), nil },
	})
	service.executable = executable
	return service
}

// The four logged failures on a real machine were all this: the app ran from a
// mounted dmg, macOS put it on a read-only AppTranslocation mount, and the
// helper could not write its backup. It returns before either restore path, so
// the app exited on "restart and update" and never came back -- with the only
// record in a temp-file log no user would find.
func TestCheckWithholdsAnUpdateFromATranslocatedApp(t *testing.T) {
	translocated := "/private/var/folders/m7/x/T/AppTranslocation/79C329CB-B290-4966-9582-D0DD9AD6FB33/d/BootAgent.app/Contents/MacOS/bootagent-desktop"
	service := checkableService(func() (string, error) { return translocated, nil })

	version, err := service.Check(context.Background())

	if version != "" {
		t.Fatalf("Check() = %q, want no version: every caller reads one as an offer to install", version)
	}
	got := oneerrors.As(err)
	if got.Code != oneerrors.UpdateLocationBlocked || got.Status != 409 {
		t.Fatalf("error = %#v", got)
	}
	if got.Retryable {
		t.Fatal("error is retryable; checking again from the same location gives the same answer")
	}
}

func TestCheckWithholdsAnUpdateFromAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to a 0o500 directory")
	}
	parent := t.TempDir()
	installed := filepath.Join(parent, "readonly")
	if err := os.Mkdir(installed, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(installed, 0o700) })

	// The bundle sits inside the unwritable directory, which is where the helper
	// would have to create its backup.
	executable := filepath.Join(installed, "BootAgent.app", "Contents", "MacOS", "bootagent-desktop")
	if runtime.GOOS != "darwin" {
		executable = filepath.Join(installed, "bootagent-desktop")
	}
	service := checkableService(func() (string, error) { return executable, nil })

	version, err := service.Check(context.Background())

	if version != "" {
		t.Fatalf("Check() = %q, want no version", version)
	}
	if got := oneerrors.As(err); got.Code != oneerrors.UpdateLocationBlocked {
		t.Fatalf("error = %#v", got)
	}
}

func TestCheckReportsAnUpdateFromAWritableInstall(t *testing.T) {
	parent := t.TempDir()
	executable := filepath.Join(parent, "BootAgent.app", "Contents", "MacOS", "bootagent-desktop")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	service := checkableService(func() (string, error) { return executable, nil })

	version, err := service.Check(context.Background())
	if err != nil || version != "0.5.2" {
		t.Fatalf("Check() = %q, %v", version, err)
	}
}

// Withholding an update because the path could not be resolved would be worse
// than attempting one: the installation may be perfectly fine.
func TestCheckIgnoresAnUnresolvableExecutable(t *testing.T) {
	for name, resolve := range map[string]func() (string, error){
		"error":        func() (string, error) { return "", errors.New("no executable") },
		"empty path":   func() (string, error) { return "", nil },
		"nil resolver": nil,
	} {
		t.Run(name, func(t *testing.T) {
			service := checkableService(resolve)

			version, err := service.Check(context.Background())
			if err != nil || version != "0.5.2" {
				t.Fatalf("Check() = %q, %v", version, err)
			}
		})
	}
}

// The probe has to leave nothing behind: it runs beside the installed
// application, every time the app checks for updates.
func TestUpdateLocationProbeRemovesItsFile(t *testing.T) {
	dir := t.TempDir()
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !directoryIsWritable(dir) {
		t.Fatal("a temp dir should be writable")
	}

	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		names := make([]string, 0, len(after))
		for _, entry := range after {
			names = append(names, entry.Name())
		}
		t.Fatalf("probe left %v behind", names)
	}
}

func TestInstallTargetResolvesTheBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("bundle paths are macOS-only")
	}
	for executable, want := range map[string]string{
		"/Applications/BootAgent.app/Contents/MacOS/bootagent-desktop": "/Applications/BootAgent.app",
		// The directory the helper writes into is the bundle's parent, so a
		// nested .app must not resolve to the inner one.
		"/Applications/BootAgent.app/Contents/Helpers/Inner.app/Contents/MacOS/x": "/Applications/BootAgent.app",
		"/usr/local/bin/bootagent-desktop":                                        "/usr/local/bin/bootagent-desktop",
	} {
		if got := installTarget(executable); got != want {
			t.Fatalf("installTarget(%q) = %q, want %q", executable, got, want)
		}
	}
}

// A .app under the translocation mount is still translocated: the check must not
// depend on where inside the bundle the executable sits.
func TestCheckUpdateLocationClassifies(t *testing.T) {
	writable := filepath.Join(t.TempDir(), "BootAgent.app", "Contents", "MacOS", "bootagent-desktop")
	if err := os.MkdirAll(filepath.Dir(writable), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := checkUpdateLocation(writable); got != locationOK {
		t.Fatalf("writable install = %q, want ok", got)
	}
	if got := checkUpdateLocation(""); got != locationOK {
		t.Fatalf("empty path = %q, want ok", got)
	}
	translocated := "/private/var/folders/x/T/AppTranslocation/ABC/d/BootAgent.app/Contents/MacOS/bootagent-desktop"
	if got := checkUpdateLocation(translocated); got != locationTranslocated {
		t.Fatalf("translocated install = %q", got)
	}
}
