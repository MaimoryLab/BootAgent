// Package oneerrors defines the stable error contract shared by the Go core,
// Wails services, and the standalone CLI.
package oneerrors

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	InvalidRequest      = "INVALID_REQUEST"
	InvalidOrigin       = "INVALID_ORIGIN" // Retained for compatibility; HTTP is being removed.
	PrerequisiteMissing = "PREREQUISITE_MISSING"
	AgentInstallFailed  = "AGENT_INSTALL_FAILED"
	ConfigWriteFailed   = "CONFIG_WRITE_FAILED"
	APIKeyRejected      = "API_KEY_REJECTED"
	ProviderUnreachable = "PROVIDER_UNREACHABLE"
	ModelsUnsupported   = "MODELS_UNSUPPORTED"
	ProtocolUnsupported = "PROTOCOL_UNSUPPORTED"
	Timeout             = "TIMEOUT"
	InternalError       = "INTERNAL_ERROR"
)

// ExitCodes is the stable process-level error contract. The map is copied by
// callers when they need to expose it to a CLI parser.
var ExitCodes = map[string]int{
	InvalidRequest:      2,
	InvalidOrigin:       2,
	PrerequisiteMissing: 3,
	AgentInstallFailed:  4,
	ConfigWriteFailed:   5,
	APIKeyRejected:      6,
	ProviderUnreachable: 6,
	ModelsUnsupported:   6,
	ProtocolUnsupported: 7,
	Timeout:             8,
	InternalError:       10,
}

// OneAgentError is safe to serialize across the Wails bridge. Message must
// already be free of credentials, file contents, and process output.
type OneAgentError struct {
	Code      string `json:"error_code"`
	Message   string `json:"message"`
	Status    int    `json:"status"`
	Retryable bool   `json:"retryable"`
	ExitCode  int    `json:"exit_code"`
	cause     error
}

func New(code, message string, options ...Option) *OneAgentError {
	err := &OneAgentError{
		Code:     code,
		Message:  message,
		Status:   400,
		ExitCode: ExitCodes[code],
	}
	if err.ExitCode == 0 {
		err.Code = InternalError
		err.ExitCode = ExitCodes[InternalError]
	}
	for _, option := range options {
		option(err)
	}
	return err
}

// Option customizes an error without exposing mutable transport details.
type Option func(*OneAgentError)

func WithStatus(status int) Option {
	return func(err *OneAgentError) { err.Status = status }
}

func WithRetryable(retryable bool) Option {
	return func(err *OneAgentError) { err.Retryable = retryable }
}

func WithCause(cause error) Option {
	return func(err *OneAgentError) { err.cause = cause }
}

func (e *OneAgentError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *OneAgentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// APIShape preserves the current HTTP response fields while the transport is
// being replaced by Wails bindings. It deliberately contains no cause or key.
func (e *OneAgentError) APIShape() map[string]any {
	if e == nil {
		return map[string]any{
			"ok":         false,
			"error_code": InternalError,
			"message":    "Unexpected OneAgent failure",
			"error":      "Unexpected OneAgent failure",
			"status":     500,
			"retryable":  true,
		}
	}
	return map[string]any{
		"ok":         false,
		"error":      e.Message,
		"message":    e.Message,
		"status":     e.Status,
		"error_code": e.Code,
		"retryable":  e.Retryable,
	}
}

// MarshalJSON keeps the bridge payload restricted to the documented fields.
func (e *OneAgentError) MarshalJSON() ([]byte, error) {
	type wire struct {
		Code      string `json:"error_code"`
		Message   string `json:"message"`
		Status    int    `json:"status"`
		Retryable bool   `json:"retryable"`
		ExitCode  int    `json:"exit_code"`
	}
	if e == nil {
		return json.Marshal(wire{Code: InternalError, Message: "Unexpected OneAgent failure", Status: 500, Retryable: true, ExitCode: ExitCodes[InternalError]})
	}
	return json.Marshal(wire{Code: e.Code, Message: e.Message, Status: e.Status, Retryable: e.Retryable, ExitCode: e.ExitCode})
}

// As converts an arbitrary error to the stable transport type. Unknown errors
// are intentionally generalized so internal details cannot reach the UI.
func As(err error) *OneAgentError {
	if err == nil {
		return nil
	}
	if oneErr, ok := errors.AsType[*OneAgentError](err); ok {
		return oneErr
	}
	return New(InternalError, "Unexpected OneAgent failure", WithStatus(500), WithRetryable(true), WithCause(err))
}

func Marshal(err error) []byte {
	payload, marshalErr := json.Marshal(As(err))
	if marshalErr != nil {
		// The shape above only contains primitive values; this is a final guard
		// for a Wails callback, which cannot return an error itself.
		return fmt.Appendf(nil, `{"error_code":%q,"message":%q,"status":500,"retryable":true,"exit_code":10}`, InternalError, "Unexpected OneAgent failure")
	}
	return payload
}
