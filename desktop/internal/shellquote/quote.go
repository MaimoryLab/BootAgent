// Package shellquote quotes values for the credential files OneAgent writes.
//
// These files are shell scripts the user sources, and they carry an API key.
// Getting the quoting wrong does not merely produce a broken file: a key
// containing a quote or a dollar sign could end the string and let the rest be
// interpreted as shell. So the rules are reproduced exactly rather than
// approximated, and compared against Python's shlex.quote.
package shellquote

import "strings"

// safeCharacters is the set shlex.quote leaves unquoted. Taken from
// shlex._find_unsafe, which quotes anything outside this set -- so the list has
// to match rather than merely be a subset, or a value Python left bare would be
// quoted here and the files would differ.
const safeCharacters = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
	"0123456789" +
	"@%+=:,./-_"

// Posix quotes a value for a POSIX shell, matching shlex.quote.
//
// An empty string becomes ” rather than nothing, because bare emptiness would
// disappear from the assignment and leave the variable unset instead of empty.
func Posix(value string) string {
	if value == "" {
		return "''"
	}
	if !needsQuoting(value) {
		return value
	}
	// Single quotes protect everything except a single quote, which cannot be
	// escaped inside them. shlex closes the string, emits a quoted quote and
	// reopens -- 'a'"'"'b' -- which is what this reproduces.
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func needsQuoting(value string) bool {
	for _, char := range value {
		if !strings.ContainsRune(safeCharacters, char) {
			return true
		}
	}
	return false
}

// PowerShell quotes a value for a PowerShell single-quoted string, where the
// only escape is a doubled quote.
//
// PowerShell does not expand anything inside single quotes, so unlike the POSIX
// case there is no set of characters that would be interpreted -- everything is
// quoted unconditionally, which is also what the Python helper does.
func PowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// For returns the quoting the platform's shell needs.
func For(osID, value string) string {
	if osID == "windows" {
		return PowerShell(value)
	}
	return Posix(value)
}
