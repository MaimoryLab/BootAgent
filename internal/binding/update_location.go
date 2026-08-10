package binding

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// translocationMarker appears in the path macOS mounts an app at when it runs
// from a disk image or a quarantined location. The volume is read-only, so the
// updater cannot write its backup beside the bundle.
const translocationMarker = "/AppTranslocation/"

// installTarget is the path the updater helper replaces: the .app bundle on
// macOS, the executable itself elsewhere. It mirrors updater.bundleTarget,
// which is unexported.
func installTarget(executable string) string {
	if runtime.GOOS != "darwin" {
		return executable
	}
	clean := filepath.Clean(executable)
	parts := strings.Split(clean, string(os.PathSeparator))
	for i, part := range parts {
		if strings.HasSuffix(part, ".app") {
			return string(os.PathSeparator) + filepath.Join(parts[1:i+1]...)
		}
	}
	return executable
}

// updateLocationProblem describes why the running application cannot be
// replaced in place, or "" when it can.
type updateLocationProblem string

const (
	locationOK           updateLocationProblem = ""
	locationTranslocated updateLocationProblem = "translocated"
	locationUnwritable   updateLocationProblem = "unwritable"
)

// checkUpdateLocation reports whether the helper could replace the installed
// application. It probes the directory holding the target, because that is
// where the helper writes its backup and its replacement -- the bundle's own
// permissions say nothing about whether a sibling can be created.
//
// Deliberately ordered: translocation is reported even on the rare occasion the
// probe would succeed, because the path itself is a temporary mount that the
// user should be told to move out of.
func checkUpdateLocation(executable string) updateLocationProblem {
	if executable == "" {
		return locationOK
	}
	target := installTarget(executable)
	if strings.Contains(target, translocationMarker) {
		return locationTranslocated
	}
	if !directoryIsWritable(filepath.Dir(target)) {
		return locationUnwritable
	}
	return locationOK
}

// directoryIsWritable answers by writing, not by reading permission bits: a
// read-only mount, an immutable flag, or a sandbox denial all present as
// permissive modes on the directory itself. Attempting the write is the only
// reliable signal, and it is what the helper does.
//
// Any failure to create counts as not writable. The probe runs in the directory
// the helper will use, so whatever stops the probe would stop the helper.
func directoryIsWritable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".oneagent-update-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}
