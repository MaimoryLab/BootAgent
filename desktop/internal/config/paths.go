// Package config writes and reads the five Agent configuration formats.
//
// Nothing here branches on an Agent id. Which file to write, which adapter to
// use and how the credential travels all come from the manifest, because an
// Agent once reported "configured" while unable to authenticate: it was missing
// from a hardcoded set, and nothing in the code said what the set was for.
package config

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// Credential delivery values, declared per Agent in the manifest. They decide
// the start command, the restart hint and whether an env file is written.
const (
	// DeliveryOneAgentEnv: the config file references an ONEAGENT_* variable
	// that an env file defines.
	DeliveryOneAgentEnv = "oneagent_env"
	// DeliveryNativeEnv: the Agent reads its own variable names, so the env file
	// defines those instead.
	DeliveryNativeEnv = "native_env"
	// DeliveryConfigFile: the credential lives in the file the adapter writes.
	DeliveryConfigFile = "config_file"
)

// agentIDPattern is what an Agent id may look like. Enforced where the path is
// built so every caller inherits it: the id names a file holding a plaintext
// credential, and a traversal would place that key outside the private
// directory.
var agentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidateAgentID rejects an id that could not be a single path segment.
func ValidateAgentID(agentID string) (string, error) {
	if !agentIDPattern.MatchString(agentID) {
		return "", oerr.Newf("INVALID_REQUEST", "Invalid Agent ID: %q", agentID)
	}
	return agentID, nil
}

// ConfigPath is the file an Agent's adapter writes, or "" when the manifest
// declares none. The Windows-specific path wins when present, falling back to
// the shared one rather than failing -- most Agents use the same location.
func ConfigPath(rt *runtime.Runtime, agent catalog.Agent) string {
	relative := agent.ConfigPath
	if rt.OSID == "windows" && agent.WindowsConfigPath != "" {
		relative = agent.WindowsConfigPath
	}
	if relative == "" {
		return ""
	}
	return filepath.Join(rt.Home, filepath.FromSlash(relative))
}

// nonAlphanumeric collapses to the underscore separator used in variable names.
var nonAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9]+`)

// AgentEnvVar names the variable an Agent reads its credential from.
//
// Each Agent gets its own, so three Agents that all speak OpenAI-compatible can
// point at different providers in the same shell. A single shared
// ONEAGENT_API_KEY made that impossible: whichever env file was sourced last
// won.
func AgentEnvVar(agentID, suffix string) string {
	if suffix == "" {
		suffix = "API_KEY"
	}
	stem := strings.ToUpper(strings.Trim(nonAlphanumeric.ReplaceAllString(agentID, "_"), "_"))
	return "ONEAGENT_" + suffix + "_" + stem
}

// NeedsEnvFile reports whether this Agent's credential travels through an env
// file.
//
// Read from the manifest rather than a set of ids written here: that set had
// left Claude Code out while it was the one Agent that could not authenticate
// without one.
func NeedsEnvFile(agent catalog.Agent) bool {
	return agent.CredentialDelivery == DeliveryOneAgentEnv || agent.CredentialDelivery == DeliveryNativeEnv
}

// EnvPath is the shared credential file, kept for Agents configured before
// per-Agent files existed.
func EnvPath(rt *runtime.Runtime) string {
	name := "env"
	if rt.OSID == "windows" {
		name = "env.ps1"
	}
	return filepath.Join(rt.Home, ".oneagent", name)
}

// AgentEnvPath is one Agent's credential file.
func AgentEnvPath(rt *runtime.Runtime, agentID string) (string, error) {
	name, err := ValidateAgentID(agentID)
	if err != nil {
		return "", err
	}
	suffix := "env"
	if rt.OSID == "windows" {
		suffix = "env.ps1"
	}
	return filepath.Join(rt.Home, ".oneagent", "agents", name+"."+suffix), nil
}
