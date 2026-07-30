package binding

import "github.com/MaimoryLab/OneAgent/internal/app"

// MarshalJSON delegates the transport DTO's wire shape to the use-case result
// so CLI and Wails callers receive identical field presence and ordering.
func (r AgentInstallResult) MarshalJSON() ([]byte, error) {
	return (app.AgentInstallResult{
		Agent:         r.Agent,
		Status:        r.Status,
		Installed:     r.Installed,
		Version:       r.Version,
		LockedVersion: r.LockedVersion,
		Registry:      r.Registry,
		Config:        r.Config,
		Code:          r.Code,
		ErrorCode:     r.ErrorCode,
		Message:       r.Message,
		Retryable:     r.Retryable,
	}).MarshalJSON()
}
