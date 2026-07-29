// Package securefs writes files that hold credentials.
//
// Every write goes through Write, and the order of its steps is the security
// property rather than an implementation detail: the temporary file is hardened
// before it is published, so a permission failure cannot leave the user with a
// world-readable config or destroy the file they already had.
package securefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// Modes are the only two this package sets. A config file may hold a plaintext
// credential, so the owner is the only principal that should reach it.
const (
	FileMode      os.FileMode = 0o600
	DirectoryMode os.FileMode = 0o700
)

// Clock supplies the backup timestamp. Injected so a test can force two backups
// into the same second and exercise the collision counter, which is otherwise
// unreachable without sleeping.
type Clock func() time.Time

// FS carries what a write needs: the runtime for platform decisions and
// subprocess access, and a clock for backup names.
type FS struct {
	Runtime *runtime.Runtime
	Now     Clock
}

// New builds an FS with the real clock.
func New(rt *runtime.Runtime) *FS {
	return &FS{Runtime: rt, Now: func() time.Time { return time.Now().UTC() }}
}

// timestamp formats a backup suffix. UTC so two machines in different zones
// produce comparable names, and second precision to match the Python format.
func (f *FS) timestamp() string {
	now := f.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return now().UTC().Format("20060102150405")
}

// EnsureDir creates a directory and hardens it.
func (f *FS) EnsureDir(path string) error {
	if err := os.MkdirAll(path, DirectoryMode); err != nil {
		return oerr.Newf("CONFIG_WRITE_FAILED", "Cannot create %s: %v", path, err)
	}
	// Set explicitly rather than trusting MkdirAll: umask reduces the mode it
	// applies, and an existing directory keeps whatever mode it already had.
	return f.Secure(path, true)
}

// Secure restricts a path to its owner. On Windows this breaks ACL inheritance
// through icacls; elsewhere it is a chmod.
func (f *FS) Secure(path string, directory bool) error {
	if f.Runtime.OSID == "windows" {
		return f.secureWindows(path, directory)
	}
	mode := FileMode
	if directory {
		mode = DirectoryMode
	}
	if err := os.Chmod(path, mode); err != nil {
		return oerr.Newf("CONFIG_WRITE_FAILED", "Cannot secure %s: %v", path, err)
	}
	return nil
}

// Backup copies an existing file aside and returns the copy's path. An absent
// file is not an error: there is simply nothing to preserve.
func (f *FS) Backup(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot inspect %s: %v", path, err)
	}
	if info.IsDir() {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot back up %s: it is a directory", path)
	}

	stamp := f.timestamp()
	candidate := path + ".backup-" + stamp
	// Two writes inside the same second would otherwise overwrite the first
	// backup, which is the one holding the user's original content.
	for counter := 1; exists(candidate); counter++ {
		candidate = path + ".backup-" + stamp + "-" + strconv.Itoa(counter)
	}

	if err := copyFile(path, candidate, info.Mode()); err != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot back up %s: %v", path, err)
	}
	return candidate, nil
}

// Write replaces a file's contents atomically, hardening as it goes.
//
// The order is the point, and each step exists because of a way the others can
// fail:
//
//  1. the parent directory is created and hardened, so nothing lands in a
//     world-readable place even briefly;
//  2. any existing file is backed up, because the merge that produced content
//     may have dropped something the user wanted;
//  3. if that backup holds a credential and cannot be hardened, it is deleted
//     and the original error is reported -- a readable copy of a key is worse
//     than no backup;
//  4. the new content is written to a temporary file in the same directory, so
//     the final step is a rename rather than a cross-device copy;
//  5. the temporary file is hardened *before* it is published, so a failure
//     here leaves the user's existing file untouched;
//  6. the rename publishes it;
//  7. a leftover temporary file is removed, and failing to remove one is
//     reported rather than ignored -- on the secret path it would be a
//     plaintext credential left in the config directory.
//
// It returns the backup path, or "" when there was nothing to back up.
func (f *FS) Write(path, content string, secret bool) (backup string, err error) {
	parent := filepath.Dir(path)
	if err := f.EnsureDir(parent); err != nil {
		return "", err
	}

	backup, err = f.Backup(path)
	if err != nil {
		return "", err
	}
	if backup != "" && secret {
		if secureErr := f.Secure(backup, false); secureErr != nil {
			if removeErr := os.Remove(backup); removeErr != nil {
				// Reported instead of the hardening failure: a readable copy of
				// a credential is still on disk, and that is the worse problem.
				return "", oerr.Newf(
					"CONFIG_WRITE_FAILED",
					"Cannot remove insecure secret backup %s: %v", backup, removeErr,
				)
			}
			return "", secureErr
		}
	}

	temporary, err := os.CreateTemp(parent, ".oneagent-*")
	if err != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot write %s: %v", path, err)
	}
	temporaryPath := temporary.Name()
	// Named so the deferred cleanup can tell "still here" from "published".
	published := false
	defer func() {
		if published {
			return
		}
		if removeErr := removeFile(temporaryPath); removeErr != nil && !os.IsNotExist(removeErr) {
			kind := ""
			if secret {
				kind = "secret "
			}
			cleanupErr := oerr.Newf(
				"CONFIG_WRITE_FAILED",
				"Cannot remove temporary %sfile %s: %v", kind, temporaryPath, removeErr,
			)
			// Only replaces a success. An earlier failure is the one the caller
			// needs to act on, and this would bury it.
			if err == nil {
				err = cleanupErr
				backup = ""
			}
		}
	}()

	if _, writeErr := temporary.WriteString(content); writeErr != nil {
		temporary.Close()
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot write %s: %v", path, writeErr)
	}
	// Closed before hardening and renaming: on Windows an open handle can block
	// both, and the data must be flushed before the rename publishes it.
	if closeErr := temporary.Close(); closeErr != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot write %s: %v", path, closeErr)
	}

	if secureErr := f.Secure(temporaryPath, false); secureErr != nil {
		return "", secureErr
	}

	if replaceErr := replace(temporaryPath, path); replaceErr != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot write %s: %v", path, replaceErr)
	}
	published = true
	return backup, nil
}

// removeFile is injectable so a test can reach the branch where the temporary
// file cannot be deleted. That branch is what stops a plaintext credential from
// being left in the config directory, and on a normal filesystem there is no
// portable way to make an unlink fail -- so it would otherwise be the one step
// in this package that nothing checks.
var removeFile = os.Remove

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// copyFile preserves the source mode, matching shutil.copy2. A backup of a
// 0600 config must not widen to the default mode.
func copyFile(source, destination string, mode os.FileMode) error {
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, raw, mode)
}

// describeMode renders a mode for an assertion message.
func describeMode(mode os.FileMode) string {
	return fmt.Sprintf("%04o", mode.Perm())
}
