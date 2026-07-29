//go:build !windows

package securefs

import "os"

// replace publishes the temporary file over the target.
//
// On POSIX os.Rename already replaces an existing target atomically, which is
// what os.replace does in Python. Windows needs an explicit flag, so that lives
// in replace_windows.go rather than being assumed equivalent here.
func replace(temporary, target string) error {
	return os.Rename(temporary, target)
}
