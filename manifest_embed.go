package oneagent

import "embed"

// AgentLockManifest is the single embedded copy of the repository's Agent
// lock file. Keeping the embed at the module root lets internal packages use
// the root manifest without maintaining a second hand-edited catalog file.
//
//go:embed agents.lock.json
var AgentLockManifest embed.FS

// FrontendAssets is the asset tree consumed by the Wails shell. The checked-in
// .keep file makes the stage-0 shell buildable before Vite has produced dist;
// the build task replaces that directory with the real bundle.
//
//go:embed all:frontend/dist
var FrontendAssets embed.FS

// EmbeddedAgentLock returns a fresh copy of agents.lock.json for parsers and
// callers that need to retain or modify the returned bytes.
func EmbeddedAgentLock() ([]byte, error) {
	return AgentLockManifest.ReadFile("agents.lock.json")
}
