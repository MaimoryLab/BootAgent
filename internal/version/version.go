// Package version holds the version reported by release binaries.
package version

import "strings"

// Version is replaced with the release version through Go linker flags.
var Version = "v0.0.0-dev"

// UpdaterVersion returns the release version accepted by the updater.
func UpdaterVersion() string {
	version := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if version == "" || strings.HasSuffix(version, "-dev") {
		return ""
	}
	return version
}
