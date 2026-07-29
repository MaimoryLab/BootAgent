package config

import (
	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
)

// Adapter names, matching the config_adapter values in the manifest.
const (
	AdapterCodex      = "codex"
	AdapterClaudeCode = "claude-code"
	AdapterOpenCode   = "opencode"
	AdapterKiloCLI    = "kilo-cli"
	AdapterAider      = "aider"
)

// schemaURLs are the $schema values each OpenAI-compatible format expects. Two
// Agents share one adapter but not one schema, which is why this is keyed
// separately rather than derived from the adapter name.
var schemaURLs = map[string]string{
	AdapterOpenCode: "https://opencode.ai/config.json",
	AdapterKiloCLI:  "https://app.kilo.ai/config.json",
}

// Write dispatches on the manifest's config_adapter and returns the path
// written.
//
// Keyed on the adapter, never on the Agent id. Two Agents sharing a config
// format share the code path, and adding an Agent with an existing format needs
// no change here -- which is the property that broke when behaviour was inferred
// from a hardcoded set of ids.
func (w *Writer) Write(agent catalog.Agent, settings Settings) (string, error) {
	switch agent.ConfigAdapter {
	case AdapterCodex:
		return w.WriteCodex(agent, settings)
	case AdapterClaudeCode:
		return w.WriteClaude(agent, settings)
	case AdapterOpenCode, AdapterKiloCLI:
		return w.WriteOpenAICompatible(agent, settings, schemaURLs[agent.ConfigAdapter])
	case AdapterAider:
		return w.WriteAider(agent, settings)
	default:
		// An Agent whose adapter has no implementation must not be reported as
		// configured. The message names the Agent because that is what the user
		// selected.
		return "", oerr.Newf("INVALID_REQUEST", "Unsupported auto-config Agent: %s", settings.AgentID)
	}
}

// SupportedAdapters lists the adapters that can be written. Used by a test to
// prove every auto Agent in the manifest has one.
func SupportedAdapters() []string {
	return []string{AdapterCodex, AdapterClaudeCode, AdapterOpenCode, AdapterKiloCLI, AdapterAider}
}
