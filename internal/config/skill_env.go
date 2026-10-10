package config

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Skill environment variables carry a Provider key to the scripts of
// generated multimodal Skills (ADR-011). BootAgent owns only names of the
// form BOOTAGENT_<PROVIDER>_API_KEY; every other variable in the same table is
// the user's and is carried through untouched.

var (
	skillEnvNamePattern = regexp.MustCompile(`^BOOTAGENT_[A-Z0-9_]+_API_KEY$`)
	skillEnvSanitize    = regexp.MustCompile(`[^A-Z0-9]`)
	bareTOMLKey         = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

	codexSkillEnvSection = regexp.MustCompile(`^\[(?:shell_environment_policy|"shell_environment_policy")\.(?:set|"set")\]$`)
)

// SkillEnvName is the variable a Provider's key is delivered in: the ID upper
// cased, with every character outside [A-Z0-9] replaced by an underscore.
func SkillEnvName(providerID string) string {
	return "BOOTAGENT_" + skillEnvSanitize.ReplaceAllString(strings.ToUpper(strings.TrimSpace(providerID)), "_") + "_API_KEY"
}

// IsSkillEnvName reports whether a variable is one BootAgent manages.
func IsSkillEnvName(name string) bool {
	return skillEnvNamePattern.MatchString(name)
}

// SkillEnvFilePath is the fallback file for Agents with no configurable
// environment. The generated script reads it only when the variable is unset.
func SkillEnvFilePath(home, providerID string) string {
	return filepath.Join(home, ".bootagent", "skill-env", strings.TrimSpace(providerID)+".env")
}

// WriteClaudeSkillEnv makes the BOOTAGENT_* variables in the env block of
// ~/.claude/settings.json exactly `values`: missing ones are added, changed
// ones rewritten, and ones not in `values` removed. Nothing is written when
// nothing changes, so an Agent without Skills never gets a file created.
func (w Writer) WriteClaudeSkillEnv(ctx context.Context, path string, values map[string]string) error {
	if err := checkSkillEnvValues(values); err != nil {
		return err
	}
	if len(values) == 0 && !mentionsSkillEnv(path) {
		// Nothing to add and nothing of ours to remove. Not parsing the file
		// keeps an unrelated problem in it from failing every Skill change.
		return nil
	}
	document, err := loadJSON(path)
	if err != nil {
		return err
	}
	existing, present := document.Get("env")
	if (!present || existing == nil) && len(values) == 0 {
		return nil
	}
	env, err := document.Child("env")
	if err != nil {
		return configError("Existing Claude Code env configuration must contain an object: %s", path)
	}
	changed := false
	for _, name := range env.Keys() {
		if _, keep := values[name]; IsSkillEnvName(name) && !keep {
			env.Delete(name)
			changed = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if env.GetString(name) != values[name] {
			env.Set(name, values[name])
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return w.writeJSON(ctx, path, document, true)
}

// WriteCodexSkillEnv makes the BOOTAGENT_* entries of Codex's
// [shell_environment_policy.set] exactly `values`, keeping the user's other
// entries there and the rest of config.toml as it is. Codex applies `set`
// after its default KEY/SECRET/TOKEN exclusion, so these survive it.
//
// config.toml carries a key from then on, so it is written as a secret,
// backups included (ADR-011).
func (w Writer) WriteCodexSkillEnv(ctx context.Context, path string, values map[string]string) error {
	if err := checkSkillEnvValues(values); err != nil {
		return err
	}
	if len(values) == 0 && !mentionsSkillEnv(path) {
		return nil
	}
	existing, err := readText(path)
	if err != nil {
		return configError("Cannot read existing TOML configuration %s: %v", path, err)
	}
	merged, err := mergeCodexSkillEnv(existing, values, path)
	if err != nil {
		return err
	}
	if merged == existing {
		return nil
	}
	return w.write(ctx, path, []byte(merged), true)
}

func mergeCodexSkillEnv(existing string, values map[string]string, path string) (string, error) {
	set := map[string]string{}
	if strings.TrimSpace(existing) != "" {
		var parsed map[string]any
		if err := toml.Unmarshal([]byte(existing), &parsed); err != nil {
			return "", configError("Existing TOML configuration is invalid: %s: %v", path, err)
		}
		if policy, ok := parsed["shell_environment_policy"].(map[string]any); ok {
			if raw, ok := policy["set"]; ok {
				table, ok := raw.(map[string]any)
				if !ok {
					return "", configError("shell_environment_policy.set in %s must be a table", path)
				}
				for name, value := range table {
					text, ok := value.(string)
					if !ok {
						return "", configError("shell_environment_policy.set.%s in %s must be a string", name, path)
					}
					if !IsSkillEnvName(name) {
						set[name] = text
					}
				}
			}
		}
	}
	maps.Copy(set, values)
	managed := ""
	if len(set) > 0 {
		lines := []string{"[shell_environment_policy.set]"}
		for _, name := range slices.Sorted(maps.Keys(set)) {
			lines = append(lines, tomlKey(name)+" = "+quoteTOML(set[name]))
		}
		managed = strings.Join(lines, "\n") + "\n"
	}
	// The whole set table is rewritten, so a spelling of it the line merge
	// cannot see -- an inline table or dotted keys -- is refused rather than
	// duplicated.
	merged, err := mergeManagedTOML(existing, managed, path, managedTOMLShape{
		topLevelKeys:   regexp.MustCompile(`^$`),
		managedSection: codexSkillEnvSection,
		ownedTables:    [][2]string{{"shell_environment_policy", "set"}},
		appendManaged:  true,
	})
	if err != nil {
		return "", err
	}
	if managed == "" && strings.TrimSpace(merged) == "" {
		return "", nil
	}
	var check map[string]any
	if err := toml.Unmarshal([]byte(merged), &check); err != nil {
		return "", configError("Cannot merge TOML configuration %s: %v", path, err)
	}
	return merged, nil
}

// WriteSkillEnvFile writes the private fallback file for one Provider.
func (w Writer) WriteSkillEnvFile(ctx context.Context, path, name, value string) error {
	if err := checkSkillEnvValues(map[string]string{name: value}); err != nil {
		return err
	}
	content := name + "=" + dotenvQuote(value) + "\n"
	if current, err := os.ReadFile(path); err == nil && string(current) == content {
		return nil
	}
	return w.write(ctx, path, []byte(content), true)
}

// RemoveSkillEnvFile deletes a fallback file. It is not backed up: the backup
// would be one more copy of a key nothing references any more.
func RemoveSkillEnvFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return configError("Cannot remove %s: %v", path, err)
	}
	return nil
}

// CodexConfigHoldsSkillKey reports whether a config.toml text carries a
// BOOTAGENT_* key, which makes the file a secret for every writer that
// touches it, MCP included.
func CodexConfigHoldsSkillKey(text string) bool {
	if !strings.Contains(text, "BOOTAGENT_") {
		return false
	}
	var parsed map[string]any
	if toml.Unmarshal([]byte(text), &parsed) != nil {
		// Unparseable but mentions the prefix: err on the secret side.
		return true
	}
	policy, _ := parsed["shell_environment_policy"].(map[string]any)
	set, _ := policy["set"].(map[string]any)
	for name := range set {
		if IsSkillEnvName(name) {
			return true
		}
	}
	return false
}

// mentionsSkillEnv is a cheap pre-check: a file that does not contain the
// prefix holds no variable of ours. A file that cannot be read is treated as
// mentioning it, so the real read reports the problem.
func mentionsSkillEnv(path string) bool {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false
	}
	return err != nil || strings.Contains(string(data), "BOOTAGENT_")
}

func checkSkillEnvValues(values map[string]string) error {
	for name, value := range values {
		if !IsSkillEnvName(name) {
			return configError("%q is not a BootAgent Skill variable", name)
		}
		if value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return configError("The value for %s is empty or contains a line break", name)
		}
	}
	return nil
}

func tomlKey(name string) string {
	if bareTOMLKey.MatchString(name) {
		return name
	}
	return quoteTOML(name)
}
