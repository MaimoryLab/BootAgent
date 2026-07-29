// Package install orchestrates Agent installation.
//
// Nothing here fetches a package itself: it decides what to install, from where,
// and refuses to proceed when the bytes do not match what the manifest pinned.
// Every subprocess goes through the injectable runner with an argv list, so a
// value from a config file or a provider response cannot become a command.
package install

import (
	"regexp"
	"strings"
)

// Redact replaces each secret with a placeholder. Applied to anything derived
// from subprocess output before it is shown or logged, because npm echoes its
// environment on some failures.
func Redact(text string, secrets []string) string {
	output := text
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		output = strings.ReplaceAll(output, secret, "[redacted]")
	}
	return output
}

// secretMarkers name the environment variables whose values must never reach a
// failure summary. Matched as substrings of the upper-cased name so a variable
// added later is covered without this list growing.
var secretMarkers = []string{"KEY", "TOKEN", "SECRET", "PASSWORD"}

// SecretsIn returns the values in an environment that look like credentials.
func SecretsIn(env map[string]string) []string {
	secrets := []string{}
	for name, value := range env {
		if value == "" {
			continue
		}
		upper := strings.ToUpper(name)
		for _, marker := range secretMarkers {
			if strings.Contains(upper, marker) {
				secrets = append(secrets, value)
				break
			}
		}
	}
	return secrets
}

// ansiEscape matches the colour codes npm emits, which would otherwise reach a
// message shown in the UI.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// failureDetailLimit bounds the summary. An npm failure can run to hundreds of
// lines, and the actionable part is at the end.
const failureDetailLimit = 600

// FailureDetail summarises why an installer failed, in a form safe to show.
//
// Redacted first, then stripped of colour codes, then reduced to the last three
// non-empty lines: npm puts the cause at the end and the preamble is noise. The
// order matters -- redacting after truncation could leave half a key in the
// output.
func FailureDetail(env map[string]string, stdout, stderr string) string {
	text := stderr + "\n" + stdout
	text = Redact(text, SecretsIn(env))
	text = ansiEscape.ReplaceAllString(text, "")

	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	joined := strings.Join(lines, " | ")
	// Truncated by code point, not by byte. Python's [:600] slices a str, so the
	// limit counts characters; len() on a Go string counts bytes. A Chinese npm
	// error -- which is what npmmirror returns -- would otherwise be cut to a
	// third of the text Python keeps, and cutting mid-character would leave an
	// invalid UTF-8 tail that encoding/json rewrites to U+FFFD.
	if runes := []rune(joined); len(runes) > failureDetailLimit {
		joined = string(runes[:failureDetailLimit])
	}
	return joined
}

// versionPattern finds a semantic version in an Agent's --version output. The
// negative lookbehind Python uses is not available in RE2, so the guard against
// matching inside a longer number is applied after the match instead.
var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

// VersionFromOutput extracts the version an Agent reports.
//
// Agents print it in many shapes -- "codex-cli 0.145.0", "1.2.3 (build 4)",
// several lines of banner first -- so this looks for the first thing shaped like
// a version rather than parsing any particular format.
func VersionFromOutput(text string) string {
	for _, match := range versionPattern.FindAllStringIndex(text, -1) {
		start := match[0]
		// Python's (?<!\d) prevents matching the tail of a longer number, so
		// "12345.6.7" is not read as "345.6.7". RE2 has no lookbehind, so the
		// preceding byte is checked here instead.
		if start > 0 && text[start-1] >= '0' && text[start-1] <= '9' {
			continue
		}
		return text[match[0]:match[1]]
	}
	return ""
}
