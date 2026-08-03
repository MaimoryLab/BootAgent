// Package oneagent is the module root. It exists to embed the catalog files
// and the built frontend bundle shared by the CLI and desktop shell.
package oneagent

import "embed"

// AgentLockManifest contains the repository's hand-edited catalog files.
// Keeping the embed at the module root lets internal packages use them without
// maintaining a second hand-edited copy inside a package.
//
//go:embed agents.lock.json providers.lock.json runtimes.lock.json
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

// EmbeddedProviderLock returns a fresh copy of providers.lock.json for the
// catalog parser.
func EmbeddedProviderLock() ([]byte, error) {
	return AgentLockManifest.ReadFile("providers.lock.json")
}

// EmbeddedRuntimeLock returns a fresh copy of runtimes.lock.json, the pinned
// Node.js and uv download contract used to bootstrap package managers.
func EmbeddedRuntimeLock() ([]byte, error) {
	return AgentLockManifest.ReadFile("runtimes.lock.json")
}
