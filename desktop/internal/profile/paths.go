// Package profile stores what a machine is pointed at, and where the key lives.
//
// Two stores, deliberately separate. A profile records provider, endpoint and
// model and is safe to read and report; the credential lives in a sibling file
// under secrets/ that nothing serialises into a response. Every path here
// validates the id it is built from, because the id names a file holding a
// plaintext key and a traversal would place that key outside the private
// directory.
package profile

import (
	"path/filepath"
	"regexp"

	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// idPattern is the same shape an Agent id has to satisfy: one path segment, no
// case folding, no dots. Shared wording with the Agent check because both decide
// where a credential is written.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidateID rejects an id that could not be a single path segment.
func ValidateID(profileID string) (string, error) {
	if !idPattern.MatchString(profileID) {
		return "", oerr.New(
			"INVALID_REQUEST",
			"Profile ID must start with a lowercase letter or digit and use only lowercase letters, digits, '-' or '_'",
		)
	}
	return profileID, nil
}

// PointerPath is profile.json, which names the active profile rather than
// holding one. Kept at the old location so an existing installation still finds
// it after the store moved into profiles/.
func PointerPath(rt *runtime.Runtime) string {
	return filepath.Join(rt.Home, ".oneagent", "profile.json")
}

// Dir holds one file per stored profile.
func Dir(rt *runtime.Runtime) string {
	return filepath.Join(rt.Home, ".oneagent", "profiles")
}

// StorePath is one stored profile.
func StorePath(rt *runtime.Runtime, profileID string) (string, error) {
	name, err := ValidateID(profileID)
	if err != nil {
		return "", err
	}
	return filepath.Join(Dir(rt), name+".json"), nil
}

// SecretPath is where a profile's key is written, in the platform's shell syntax
// so it can be sourced.
//
// Validated here rather than only at the callers: this decides where a plaintext
// key lands, so a future caller inherits the check instead of remembering it.
func SecretPath(rt *runtime.Runtime, profileID string) (string, error) {
	name, err := ValidateID(profileID)
	if err != nil {
		return "", err
	}
	suffix := "env"
	if rt.OSID == "windows" {
		suffix = "env.ps1"
	}
	return filepath.Join(rt.Home, ".oneagent", "secrets", name+"."+suffix), nil
}

// AgentsDir holds one binding per configured Agent.
func AgentsDir(rt *runtime.Runtime) string {
	return filepath.Join(rt.Home, ".oneagent", "agents")
}
