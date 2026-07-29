package profile

import (
	"os"
	"regexp"
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/shellquote"
)

// secretPatterns extract the key back out of the env file it was written to. Two
// shells, two quoting rules, so two patterns rather than one loose one.
var secretPatterns = map[string]*regexp.Regexp{
	"windows": regexp.MustCompile(`(?m)^\$env:ONEAGENT_API_KEY\s*=\s*'(.*)'$`),
	"posix":   regexp.MustCompile(`(?m)^export ONEAGENT_API_KEY=(.*)$`),
}

// WriteSecret stores a profile's key, in the shell syntax that lets the file be
// sourced. Writing nothing when there is no key: an empty file would read back as
// "a key is held" from HasSecret and offer an activation that cannot work.
func (s *Store) WriteSecret(profileID, apiKey, baseURL string) error {
	if apiKey == "" {
		return nil
	}
	path, err := SecretPath(s.Runtime, profileID)
	if err != nil {
		return err
	}
	content := envAssignments(s.Runtime.OSID, [][2]string{
		{"ONEAGENT_API_KEY", apiKey},
		{"ONEAGENT_API_BASE_URL", baseURL},
	})
	// secret=true: the write hardens the temporary file before publishing it, so
	// the key is never briefly readable at the final path.
	_, err = s.FS.Write(path, content, true)
	return err
}

// ReadSecret returns the key stored for a profile, or "" when none is held.
//
// This is what lets an Agent be pointed back at a saved provider without the user
// re-pasting the key. The value is returned only to a caller about to write a
// config with it -- it never reaches a response, and the only thing any payload
// says about it is the hasKey boolean.
func (s *Store) ReadSecret(profileID string) (string, error) {
	path, err := SecretPath(s.Runtime, profileID)
	if err != nil {
		return "", err
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", nil
		}
		return "", oerr.Newf(
			"CONFIG_WRITE_FAILED",
			"Cannot read stored key for profile %s: %v", profileID, readErr,
		)
	}

	shell := "posix"
	if s.Runtime.OSID == "windows" {
		shell = "windows"
	}
	match := secretPatterns[shell].FindStringSubmatch(string(raw))
	if match == nil {
		return "", nil
	}
	if shell == "windows" {
		// PowerShell escapes a single quote by doubling it, so undo that.
		return strings.ReplaceAll(match[1], "''", "'"), nil
	}
	// The POSIX side was written with shlex.quote, so reverse it with the same
	// splitting rules rather than trimming quotes: a key containing a quote would
	// otherwise come back with the escape sequence still in it.
	//
	// An unbalanced quote reports no key held rather than failing. Only a hand
	// edit produces one, and the caller then reports "API key is required", which
	// names something the user can fix -- where the Python original let the
	// underlying ValueError escape to the CLI as a traceback and the server as a
	// 500. That has been fixed on both sides.
	return shellquote.FirstPosixField(match[1]), nil
}

// envAssignments renders shell assignments. A slice of pairs rather than a map
// because the file's line order is the insertion order on the Python side, and a
// map would emit them sorted -- a different file for the same inputs.
func envAssignments(osID string, values [][2]string) string {
	var builder strings.Builder
	for _, pair := range values {
		if osID == "windows" {
			builder.WriteString("$env:" + pair[0] + " = " + shellquote.PowerShell(pair[1]) + "\n")
		} else {
			builder.WriteString("export " + pair[0] + "=" + shellquote.Posix(pair[1]) + "\n")
		}
	}
	return builder.String()
}
