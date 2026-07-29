//go:build windows

package securefs

import (
	"golang.org/x/sys/windows"
)

// replace publishes the temporary file over the target.
//
// os.Rename is not equivalent to Python's os.replace here: on Windows it fails
// when the target already exists, so relying on it would break every write to a
// config the user already had -- and only on Windows, which is the hardest place
// to notice. MoveFileEx with MOVEFILE_REPLACE_EXISTING is the documented
// replacement, and WRITE_THROUGH makes it durable before returning.
func replace(temporary, target string) error {
	from, err := windows.UTF16PtrFromString(temporary)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
