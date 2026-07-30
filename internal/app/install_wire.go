package app

import "github.com/MaimoryLab/OneAgent/internal/jsonorder"

// IsCheckOnly reports whether this result came from the agent-presence check
// path. It is intentionally a method rather than a serialized field: the
// transport adapters need the distinction to preserve Python's field
// presence, while clients should only see the public result shape.
func (r AgentInstallResult) IsCheckOnly() bool {
	return r.checkOnly
}

// MarshalJSON preserves the Python install response's field presence and
// insertion order. The distinction matters to clients: a normal automatic
// result reports installed=false and version=null, while guide and failed
// results do not pretend those fields exist.
func (r AgentInstallResult) MarshalJSON() ([]byte, error) {
	document := jsonorder.NewObject()
	document.Set("agent", r.Agent)
	document.Set("status", r.Status)

	automatic := r.Status == "configured" || r.Status == "skipped" || r.Status == "installed"
	if automatic {
		if !r.checkOnly {
			document.Set("config", r.Config)
		}
		document.Set("installed", r.Installed)
		if r.Version == "" {
			document.Set("version", nil)
		} else {
			document.Set("version", r.Version)
		}
		if r.LockedVersion == "" {
			document.Set("lockedVersion", nil)
		} else {
			document.Set("lockedVersion", r.LockedVersion)
		}
		if r.Registry != "" {
			document.Set("registry", r.Registry)
		}
	}
	if r.Status == "failed" {
		document.Set("code", r.Code)
		document.Set("error_code", r.ErrorCode)
		document.Set("message", r.Message)
	} else if r.Status == "guide-only" {
		document.Set("message", r.Message)
	}
	document.Set("retryable", r.Retryable)
	return document.MarshalJSON()
}
