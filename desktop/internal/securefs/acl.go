package securefs

import (
	"context"
	"os/user"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// aclTimeout bounds each icacls call. A hung ACL tool would otherwise stall a
// config write with no way for the user to tell what happened.
const aclTimeout = 30 * time.Second

// systemSID is the well-known SID for NT AUTHORITY\SYSTEM, granted alongside the
// user so Windows servicing and backup still function. Named by SID rather than
// by name because the display name is localised.
const systemSID = "*S-1-5-18"

// currentUser is injectable so a test can exercise the branch where neither the
// environment nor the OS can name the user.
var currentUser = user.Current

// secureWindows breaks ACL inheritance and grants only the current user and
// SYSTEM.
//
// icacls is called through the injectable runner with an argv list -- never a
// shell string -- because the path comes from a config location that a user can
// influence. Two calls rather than one: /reset drops inherited and stale
// entries, and only then does /inheritance:r /grant:r describe the full ACL.
// Granting without resetting would leave whatever was already there.
func (f *FS) secureWindows(path string, directory bool) error {
	icacls, found := f.Runtime.Which("icacls")
	if !found {
		// A hard failure rather than a warning: continuing would leave a
		// credential file readable by every account on the machine.
		return oerr.New("CONFIG_WRITE_FAILED", "Windows ACL tool icacls was not found")
	}

	username, err := f.username()
	if err != nil {
		return err
	}

	userGrant := username + ":F"
	systemGrant := systemSID + ":F"
	if directory {
		// (OI)(CI) makes the grant inherit to files and subdirectories, so a
		// file created later in this directory is not left with a wider ACL.
		userGrant = username + ":(OI)(CI)F"
		systemGrant = systemSID + ":(OI)(CI)F"
	}

	commands := [][]string{
		{icacls, path, "/reset"},
		{icacls, path, "/inheritance:r", "/grant:r", userGrant, systemGrant},
	}
	for _, argv := range commands {
		result, runErr := f.Runtime.Run(context.Background(), argv, runOptions(f, aclTimeout))
		if runErr != nil {
			return oerr.Newf("CONFIG_WRITE_FAILED", "Failed to secure Windows ACL for %s: %v", path, runErr)
		}
		if result.ExitCode != 0 {
			// The message deliberately omits icacls output: it echoes the path
			// and can be long, and the actionable fact is which path failed.
			return oerr.Newf("CONFIG_WRITE_FAILED", "Failed to secure Windows ACL for %s", path)
		}
	}
	return nil
}

// username names the principal to grant. USERNAME is preferred because it is
// what the session actually runs as; user.Current is the fallback.
func (f *FS) username() (string, error) {
	if value := f.Runtime.Env["USERNAME"]; value != "" {
		return value, nil
	}
	current, err := currentUser()
	if err != nil || current.Username == "" {
		return "", oerr.New("CONFIG_WRITE_FAILED", "Cannot determine the current Windows user to grant access to")
	}
	return current.Username, nil
}

// runOptions builds the subprocess options for an ACL call. The environment is
// passed explicitly rather than inherited so the child sees exactly what the
// Runtime holds -- a test can then prove no credential reached it.
func runOptions(f *FS, timeout time.Duration) runtime.RunOptions {
	return runtime.RunOptions{Env: f.Runtime.Env, Timeout: timeout}
}
