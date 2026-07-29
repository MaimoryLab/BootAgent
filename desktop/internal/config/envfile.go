package config

import (
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/shellquote"
)

// assignment is one variable in an env file. A slice rather than a map because
// the file's line order is the insertion order in Python, and a Go map would
// emit them sorted -- a different file for the same inputs.
type assignment struct {
	Name  string
	Value string
}

// envAssignments renders the assignments in the platform's shell syntax.
func envAssignments(osID string, values []assignment) string {
	var builder strings.Builder
	for _, item := range values {
		if osID == "windows" {
			builder.WriteString("$env:" + item.Name + " = " + shellquote.PowerShell(item.Value) + "\n")
		} else {
			builder.WriteString("export " + item.Name + "=" + shellquote.Posix(item.Value) + "\n")
		}
	}
	return builder.String()
}

// WriteSharedEnv writes the legacy shared credential file.
//
// Kept because configs written by earlier versions still reference
// ONEAGENT_API_KEY; dropping it would break those Agents on upgrade. New configs
// read their own variable through WriteAgentEnv.
func (w *Writer) WriteSharedEnv(apiKey, baseURL string) (string, error) {
	path := EnvPath(w.Runtime)
	content := envAssignments(w.Runtime.OSID, []assignment{
		{"ONEAGENT_API_KEY", apiKey},
		{"ONEAGENT_API_BASE_URL", baseURL},
	})
	if _, err := w.FS.Write(path, content, true); err != nil {
		return "", err
	}
	return path, nil
}

// WriteAgentEnv writes the env file one Agent's credential reaches it through.
//
// Two shapes, declared per Agent as credential_delivery in the manifest:
//
// oneagent_env -- the config file the adapter wrote references ONEAGENT_* names
// (Codex's env_key, OpenCode's {env:...}), so those are what this file defines.
//
// native_env -- the Agent only reads variable names it defines itself. Claude
// Code is the case that proves this matters: it ignores the credential in its own
// settings.json and answers "Not logged in" until ANTHROPIC_AUTH_TOKEN is in the
// environment. Writing only ONEAGENT_* names for it produced an Agent that
// OneAgent reported as configured and that could not authenticate.
func (w *Writer) WriteAgentEnv(agentID string, agent catalog.Agent, settings Settings) (string, error) {
	path, err := AgentEnvPath(w.Runtime, agentID)
	if err != nil {
		return "", err
	}

	values := []assignment{
		{AgentEnvVar(agentID, "API_KEY"), settings.APIKey},
		{AgentEnvVar(agentID, "API_BASE_URL"), settings.BaseURL},
		// The shared names too, so a shell that sources only this file still
		// satisfies a config written before per-Agent variables existed.
		{"ONEAGENT_API_KEY", settings.APIKey},
		{"ONEAGENT_API_BASE_URL", settings.BaseURL},
	}

	// The Agent's own names, so sourcing this file is enough to start it. Read
	// from the manifest rather than a table here: which Agent needs which
	// variable is exactly what was wrong when it was inferred from an id.
	if native := agent.EnvVars; len(native) > 0 {
		for _, field := range []struct{ key, value string }{
			{"api_key", settings.APIKey},
			{"base_url", settings.BaseURL},
			{"model", settings.Model},
		} {
			if name := native[field.key]; name != "" && field.value != "" {
				values = append(values, assignment{name, field.value})
			}
		}
		if name := native["small_fast_model"]; name != "" && settings.Model != "" {
			smallFast := settings.SmallFastModel
			if smallFast == "" {
				smallFast = settings.Model
			}
			values = append(values, assignment{name, smallFast})
		}
	}

	content := envAssignments(w.Runtime.OSID, values)
	if _, err := w.FS.Write(path, content, true); err != nil {
		return "", err
	}
	return path, nil
}
