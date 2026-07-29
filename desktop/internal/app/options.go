// Package app is the use case layer: the orchestration both the CLI and the
// desktop shell call, with no transport of its own.
//
// Nothing here knows about HTTP, IPC or argv. That is what lets the CLI stay a
// plain Go binary that does not link the GUI runtime -- a headless or automated
// environment would otherwise need a display to install an Agent.
package app

import (
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/profile"
	"github.com/MaimoryLab/OneAgent/desktop/internal/provider"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// InstallOptions is one install request. Named fields rather than positional
// arguments because several are booleans that read identically at a call site.
type InstallOptions struct {
	Agents []string
	// ProfileAgents is what the stored profile should list, which can be wider
	// than the Agents being installed now: the wizard remembers the whole
	// selection while installing only what is missing. Empty means Agents.
	ProfileAgents []string
	Provider      string
	APIBaseURL    string
	APIKey        string
	Model         string
	// SmallFastModel is Claude Code's cheaper background model. Empty follows
	// Model, and only the claude-code adapter reads it.
	SmallFastModel string
	// Configure false checks and installs without writing any config, which is
	// how "I already have an account" is handled.
	Configure bool
	// InstallAgent lets OneAgent install the package. Without it a missing Agent
	// is reported with the official command rather than installed behind the
	// user's back.
	InstallAgent bool
	// CheckAgentOnly stops after establishing what is present.
	CheckAgentOnly bool
	// SkipTest opts out of every network round trip, discovery included.
	SkipTest bool
	// LockedVersion reinstalls when the present version differs from the pin.
	LockedVersion bool
	// Latest installs the floating tag. Only ever from an explicit user request:
	// the manifest forbids `latest` and the integrity check cannot apply to it.
	Latest  bool
	Timeout time.Duration
	// Registry is a mirror id or an HTTPS URL. Empty means the official source.
	Registry string
}

// AgentResult is one Agent's outcome. Status is the string the frontend switches
// on, so the values are fixed: configured, skipped, installed, guide-only, failed.
type AgentResult struct {
	Agent  string `json:"agent"`
	Status string `json:"status"`
	Config string `json:"config,omitempty"`
	// Installed reports whether this run installed the package, as distinct from
	// finding it already present.
	Installed     bool   `json:"installed,omitempty"`
	Version       string `json:"version,omitempty"`
	LockedVersion string `json:"lockedVersion,omitempty"`
	Registry      string `json:"registry,omitempty"`
	// Code, ErrorCode and Message are set only on a failure.
	Code      int    `json:"code,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable"`
}

// InstallResult is what one install request produced.
type InstallResult struct {
	OK bool `json:"ok"`
	// Code is the exit code the CLI returns: the first failure's, so a script
	// sees the reason rather than a generic one.
	Code    int           `json:"code"`
	Results []AgentResult `json:"results"`
	// Log is redacted before it leaves this package.
	Log  string `json:"log"`
	Next string `json:"next"`
	// Probe is the verdict worth showing when several were taken, and Probes
	// holds all of them keyed by protocol.
	Probe  *provider.Verdict           `json:"probe"`
	Probes map[string]provider.Verdict `json:"probes"`
}

// Service carries the collaborators an operation needs. Assembled once so a test
// can replace any of them, and so nothing reaches for a global.
//
// No Installer seam: install.LockedAgent already takes the runtime, whose runner
// and lookup are the injection points, so wrapping it in an interface would add a
// layer without adding a thing a test can do.
type Service struct {
	Runtime *runtime.Runtime
	Writer  *config.Writer
	Store   *profile.Store
	Probes  *provider.Client
}

// NewService assembles the layer over one runtime.
func NewService(rt *runtime.Runtime, timeout time.Duration) *Service {
	return &Service{
		Runtime: rt,
		Writer:  config.NewWriter(rt),
		Store:   profile.NewStore(rt),
		Probes:  provider.NewClient(timeout),
	}
}
