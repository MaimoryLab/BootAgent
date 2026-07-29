package config

import (
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
)

// The Codex config is TOML, and OneAgent has to add its own provider table to a
// file the user may have edited by hand.
//
// The merge is line-based rather than a parse-and-reserialise, and that is
// deliberate: round-tripping through a TOML encoder would reformat the user's
// comments, spacing and key order into whatever the encoder prefers. Keeping
// their lines verbatim and using the parser only to validate is what makes the
// output byte-identical to what they had, plus our table.

var (
	// codexTopLevelKey matches the two keys OneAgent owns at the top level.
	codexTopLevelKey = regexp.MustCompile(`^\s*(model_provider|model)\s*=`)
	// codexManagedSection matches our table and any sub-table of it.
	codexManagedSection = regexp.MustCompile(`^\[model_providers\.oneagent(?:\..+)?\]$`)
	// whitespace collapses a header before comparison, so "[ a . b ]" and
	// "[a.b]" are recognised as the same table.
	whitespace = regexp.MustCompile(`\s+`)
)

// CodexManagedBlock is the table OneAgent writes. Built here so the merge and
// the tests agree on what "ours" means.
func CodexManagedBlock(providerName, baseURL, model, envVar string) string {
	var builder strings.Builder
	builder.WriteString("model_provider = \"oneagent\"\n")
	builder.WriteString("model = " + tomlString(model) + "\n\n")
	builder.WriteString("[model_providers.oneagent]\n")
	builder.WriteString("name = " + tomlString(providerName) + "\n")
	builder.WriteString("base_url = " + tomlString(baseURL) + "\n")
	builder.WriteString("env_key = " + tomlString(envVar) + "\n")
	// Fixed, not a choice: Codex speaks the Responses API and nothing else.
	builder.WriteString("wire_api = \"responses\"\n")
	return builder.String()
}

// MergeCodexTOML combines the user's existing file with our managed block.
//
// It refuses rather than guesses in two cases, both of which mean the file uses
// a syntax this line-based merge cannot see: a model_provider or model key that
// the parser found but the line scanner did not (so it is inside a multi-line or
// otherwise unusual form), and a model_providers.oneagent table declared some
// way other than a plain header. Overwriting either would silently drop what the
// user wrote.
func MergeCodexTOML(existing, managed, path string) (string, error) {
	if strings.TrimSpace(existing) == "" {
		return managed, nil
	}

	var parsed map[string]any
	if _, err := toml.Decode(existing, &parsed); err != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Existing TOML configuration is invalid: %s: %v", path, err)
	}

	topLevelKept := []string{}
	tableKept := []string{}
	removedTopLevel := map[string]bool{}
	managedSectionFound := false
	inTable := false
	skipManagedSection := false

	for _, line := range splitLines(existing) {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "[") {
			// A comment after the header is not part of the table name.
			header := whitespace.ReplaceAllString(strings.SplitN(stripped, "#", 2)[0], "")
			inTable = true
			skipManagedSection = codexManagedSection.MatchString(header)
			if skipManagedSection {
				managedSectionFound = true
				continue
			}
		}
		if skipManagedSection {
			continue
		}
		if !inTable {
			if match := codexTopLevelKey.FindStringSubmatch(line); match != nil {
				removedTopLevel[match[1]] = true
				continue
			}
		}
		if inTable {
			tableKept = append(tableKept, line)
		} else {
			topLevelKept = append(topLevelKept, line)
		}
	}

	// The parser saw our table but the scanner did not, so it is declared in a
	// form this merge would duplicate rather than replace.
	if providerTables, ok := parsed["model_providers"].(map[string]any); ok {
		if _, present := providerTables["oneagent"]; present && !managedSectionFound {
			return "", oerr.Newf("CONFIG_WRITE_FAILED", "Unsupported OneAgent TOML table syntax in %s", path)
		}
	}
	// Likewise for the two keys: present to the parser, invisible to the scanner.
	for _, key := range []string{"model_provider", "model"} {
		if _, present := parsed[key]; present && !removedTopLevel[key] {
			return "", oerr.Newf("CONFIG_WRITE_FAILED", "Unsupported TOML key syntax for %s in %s", key, path)
		}
	}

	sections := []string{
		strings.TrimSpace(strings.Join(topLevelKept, "\n")),
		strings.TrimRight(managed, "\n\r\t "),
		strings.TrimSpace(strings.Join(tableKept, "\n")),
	}
	present := []string{}
	for _, section := range sections {
		if section != "" {
			present = append(present, section)
		}
	}
	merged := strings.Join(present, "\n\n") + "\n"

	// Read back what will be written. A merge that produces invalid TOML would
	// otherwise leave the Agent with a file it cannot parse.
	var check map[string]any
	if _, err := toml.Decode(merged, &check); err != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot merge TOML configuration %s: %v", path, err)
	}
	return merged, nil
}

// splitLines splits on newlines without keeping them, matching str.splitlines
// for the cases a config file contains. A trailing newline does not produce a
// final empty element.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	normalised := strings.ReplaceAll(text, "\r\n", "\n")
	normalised = strings.ReplaceAll(normalised, "\r", "\n")
	lines := strings.Split(normalised, "\n")
	if last := len(lines) - 1; last >= 0 && lines[last] == "" {
		lines = lines[:last]
	}
	return lines
}
