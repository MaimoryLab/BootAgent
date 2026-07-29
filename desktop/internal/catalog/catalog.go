// Package catalog reads the manifest that decides what OneAgent does.
//
// agents.lock.json is the single source of truth for every Agent's command,
// config path, adapter, credential delivery and pinned version. Nothing here
// or above may branch on an Agent id: an Agent once reported "configured" while
// unable to authenticate because it was missing from a hardcoded set, and the
// fix was to derive behaviour from the manifest instead.
package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"

	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
)

// The manifest is embedded rather than located at runtime. The Python build
// needed resource_root() to cope with three layouts -- checkout, PyInstaller
// bundle and wheel -- and a stale staging directory could shadow the real file.
// A single binary removes that problem rather than solving it.
//
// The embedded file is a copy, because go:embed cannot reach outside its own
// package directory and refuses symlinks. The repository root keeps the only
// editable manifest; this one is generated, and embed_parity_test.go fails if
// the two differ by a single byte. The name says "embed" so it is not mistaken
// for a second source of truth.
//
//go:generate go run ./cmd/sync-manifest
//go:embed agents.lock.embed.json
var manifestJSON []byte

// SchemaVersion is the only manifest version this build understands. A newer
// manifest is refused rather than partially read.
const SchemaVersion = 1

// Protocol names the inference protocol an Agent speaks once configured. A
// model ID that answers one is not guaranteed to answer the others, so this
// drives both the connection test and the config write.
const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
	ProtocolResponses = "responses"
)

// adapterProtocols maps a config adapter to the protocol it speaks. Keyed on
// the adapter rather than the Agent id so two Agents sharing a config format
// share the entry.
var adapterProtocols = map[string]string{
	"codex":       ProtocolResponses,
	"claude-code": ProtocolAnthropic,
	"opencode":    ProtocolOpenAI,
	"kilo-cli":    ProtocolOpenAI,
	"aider":       ProtocolOpenAI,
}

// AgentProtocol reports the protocol an adapter speaks. An unknown adapter gets
// OpenAI-compatible, which is the documented default -- an adapter added
// without an entry must not silently probe a protocol it does not speak.
func AgentProtocol(adapter string) string {
	if protocol, known := adapterProtocols[adapter]; known {
		return protocol
	}
	return ProtocolOpenAI
}

// Package is an Agent's pinned upstream package.
type Package struct {
	Manager    string `json:"manager"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Integrity  string `json:"integrity"`
	Source     string `json:"source"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
}

// Agent is one manifest entry. Every key the manifest declares has a field here,
// which is not a convention but a gate: TestParityEveryManifestKeyIsReadByTheGoStruct
// reflects over these tags and fails when the manifest declares something nothing
// reads. Byte-identical embedding gives no protection against that -- the same
// bytes can still be read differently -- and two keys were in fact silently
// dropped before the test existed.
type Agent struct {
	Name    string   `json:"name"`
	Group   string   `json:"group"`
	Command string   `json:"command"`
	Rank    *int     `json:"rank"`
	Website string   `json:"website"`
	Docs    string   `json:"docs"`
	Notes   string   `json:"notes"`
	License string   `json:"license"`
	Package *Package `json:"package"`

	ConfigMode    string `json:"config_mode"`
	ConfigPath    string `json:"config_path"`
	ConfigAdapter string `json:"config_adapter"`
	// WindowsConfigPath overrides ConfigPath on Windows. Only Aider declares one
	// today, but reading it from the manifest is what keeps the exception out of
	// the code.
	WindowsConfigPath string `json:"windows_config_path"`
	// VersionArgs asks the Agent for its version. Declared per Agent because not
	// every CLI answers --version.
	VersionArgs []string `json:"version_args"`
	// Guide is the instruction text shown for a guide-only Agent.
	Guide any `json:"guide"`
	// CredentialDelivery decides how the key reaches the Agent, and with it the
	// start command, the restart hint and whether an env file is written. It is
	// declared per Agent precisely so none of that is inferred from an id.
	CredentialDelivery string            `json:"credential_delivery"`
	EnvVars            map[string]string `json:"env_vars"`

	Platforms            []string `json:"platforms"`
	WindowsNote          string   `json:"windows_note"`
	WindowsPrerequisites []string `json:"windows_prerequisites"`
}

// GuideOnly reports whether OneAgent only shows instructions for this Agent.
// A guide-only Agent is never installed and never has config written.
func (a Agent) GuideOnly() bool { return a.ConfigMode != "auto" }

// RankOrDefault is how prominently to show the Agent. Absent means 99, which
// sorts last: prominence is independent of whether OneAgent can install it,
// because an overview is judged by whether the tools people use are on it.
func (a Agent) RankOrDefault() int {
	if a.Rank == nil {
		return 99
	}
	return *a.Rank
}

// Manifest is the parsed lock file.
type Manifest struct {
	SchemaVersion   int              `json:"schema_version"`
	OneAgentVersion string           `json:"oneagent_version"`
	GeneratedAt     string           `json:"generated_at"`
	Agents          map[string]Agent `json:"agents"`
	// declared is the order the manifest lists the Agents in, which a Go map does
	// not keep. Python iterates the parsed dict, so this is the order that reaches
	// the status payload's supportedAgentIds -- and it is not the rank order the
	// catalog is sorted by, so the two cannot share one accessor.
	declared []string
}

// DeclaredIDs lists the Agents in the order the manifest declares them.
func (m *Manifest) DeclaredIDs() []string {
	return append([]string{}, m.declared...)
}

// Parse reads a manifest from bytes and refuses anything it cannot fully
// understand. Both failures report INVALID_REQUEST to match the Python core.
func Parse(raw []byte) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, oerr.Newf("INVALID_REQUEST", "Cannot load Agent lock manifest: %v", err)
	}
	if manifest.SchemaVersion != SchemaVersion || len(manifest.Agents) == 0 {
		return nil, oerr.New("INVALID_REQUEST", "Unsupported Agent lock manifest schema")
	}
	declared, err := declaredAgentOrder(raw)
	if err != nil {
		return nil, err
	}
	manifest.declared = declared
	return &manifest, nil
}

// declaredAgentOrder reads the agent keys in the order the file lists them.
//
// jsonorder already decodes order-preserving JSON for the config adapters, so this
// reuses it rather than hand-walking tokens: the first version of this function did
// walk them, and it was 90 lines of skip-the-nested-value logic to answer one
// question the existing decoder already answers.
func declaredAgentOrder(raw []byte) ([]string, error) {
	document, err := jsonorder.Parse(raw)
	if err != nil {
		return nil, oerr.Newf("INVALID_REQUEST", "Cannot read the Agent lock manifest: %v", err)
	}
	agents, present := document.GetObject("agents")
	if !present {
		return nil, oerr.New("INVALID_REQUEST", "Agent lock manifest declares no agents object")
	}
	return agents.Keys(), nil
}

// Load returns the embedded manifest. Parsed once: it cannot change at runtime,
// and a parse failure here is a build defect rather than a user error.
//
// sync.OnceValues rather than a nil check, because every Wails binding call runs
// on its own goroutine and several of them read the catalog. A plain check-then-
// assign is a data race that -race reports, and it would first be seen at the
// moment the orchestration layer is wired into the shell.
func Load() (*Manifest, error) { return loadEmbedded() }

// loadEmbedded is a variable so the parse happens once; Load stays a function so
// no caller can replace it.
var loadEmbedded = sync.OnceValues(func() (*Manifest, error) {
	return Parse(manifestJSON)
})

// MustLoad returns the embedded manifest and panics if it cannot be parsed.
// Reserved for start-up paths where a broken embed means a broken build.
func MustLoad() *Manifest {
	manifest, err := Load()
	if err != nil {
		panic(fmt.Sprintf("embedded agents.lock.json is unusable: %v", err))
	}
	return manifest
}

// Agent returns one entry.
func (m *Manifest) Agent(id string) (Agent, bool) {
	agent, present := m.Agents[id]
	return agent, present
}

// AutoAgents lists the ids OneAgent installs and configures, in catalog order.
func (m *Manifest) AutoAgents() []string {
	ids := []string{}
	for id, agent := range m.Agents {
		if !agent.GuideOnly() {
			ids = append(ids, id)
		}
	}
	m.sortIDs(ids)
	return ids
}

// IDs lists every Agent in catalog order.
func (m *Manifest) IDs() []string {
	ids := make([]string, 0, len(m.Agents))
	for id := range m.Agents {
		ids = append(ids, id)
	}
	m.sortIDs(ids)
	return ids
}

// sortIDs orders by rank then id, the same order PublicCatalog uses, so a
// client never has to re-derive it and two clients cannot disagree.
func (m *Manifest) sortIDs(ids []string) {
	sort.Slice(ids, func(left, right int) bool {
		leftRank, rightRank := m.Agents[ids[left]].RankOrDefault(), m.Agents[ids[right]].RankOrDefault()
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return ids[left] < ids[right]
	})
}
