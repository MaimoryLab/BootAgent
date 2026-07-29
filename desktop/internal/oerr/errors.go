// Package oerr carries the error contract across the Python-to-Go migration.
//
// The codes and their exit codes are an external contract: scripts and CI read
// the process exit status, so a code that changes value breaks callers outside
// this repository. While both implementations exist, oneagent/errors.py stays
// the single source of truth and codes_parity_test.go holds this file to it.
package oerr

import "fmt"

// ExitCodes mirrors EXIT_CODES in oneagent/errors.py. Three codes deliberately
// share 6 -- everything the provider rejects is one class to a shell caller --
// and 1 and 9 are unused.
var ExitCodes = map[string]int{
	"INVALID_REQUEST":      2,
	"INVALID_ORIGIN":       2,
	"PREREQUISITE_MISSING": 3,
	"AGENT_INSTALL_FAILED": 4,
	"CONFIG_WRITE_FAILED":  5,
	"API_KEY_REJECTED":     6,
	"PROVIDER_UNREACHABLE": 6,
	"MODELS_UNSUPPORTED":   6,
	"PROTOCOL_UNSUPPORTED": 7,
	"TIMEOUT":              8,
	"INTERNAL_ERROR":       10,
}

// UnknownExitCode is what an unrecognised code resolves to. Python reaches it
// via EXIT_CODES.get(code, 10) -- a literal default rather than a lookup of
// INTERNAL_ERROR, so a typo in that name cannot silently change the fallback.
const UnknownExitCode = 10

// Error is the one error type the core raises. Callers read Code rather than
// matching on Message, which is user-facing Chinese and may be rephrased.
type Error struct {
	Code      string
	Message   string
	Retryable bool
	Status    int
	ExitCode  int
}

// New builds an Error with the same defaults as the Python constructor: not
// retryable, HTTP 400, and the exit code the table gives for this code.
func New(code, message string, opts ...Option) *Error {
	err := &Error{
		Code:     code,
		Message:  message,
		Status:   400,
		ExitCode: exitCodeFor(code),
	}
	for _, opt := range opts {
		opt(err)
	}
	return err
}

// Option adjusts a field that New defaults. Retryable and Status vary per call
// site often enough to deserve this; ExitCode is overridden only where a
// caller must diverge from the table.
type Option func(*Error)

// Retryable marks the failure as worth another attempt, which the frontend
// uses to decide whether to offer a retry rather than only an explanation.
func WithRetryable() Option {
	return func(e *Error) { e.Retryable = true }
}

// Status overrides the HTTP status. Retained through the Wails migration
// because the frontend error contract still carries it.
func WithStatus(status int) Option {
	return func(e *Error) { e.Status = status }
}

// WithExitCode overrides the code the table would give. Named for the option
// rather than the field so it does not read as a second meaning of Error.ExitCode.
func WithExitCode(code int) Option {
	return func(e *Error) { e.ExitCode = code }
}

// Newf builds an Error whose message is formatted. Options are applied
// afterwards through Set, because variadic format args and variadic options
// cannot both be last:
//
//	oerr.Newf("PREREQUISITE_MISSING", "缺少 %s", "npm").Set(oerr.WithRetryable())
func Newf(code, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}

// Set applies options to an existing Error and returns it, so a formatted
// message and an override can be written in one expression.
func (e *Error) Set(opts ...Option) *Error {
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Error returns only the redacted Message. Nothing derived from a credential
// reaches this string, so logging an error cannot leak a key.
func (e *Error) Error() string { return e.Message }

// Payload is the response body, matching to_dict() in errors.py exactly: six
// keys, with the message under both "error" and "message" because the frontend
// reads one and older callers read the other. exit_code is deliberately absent
// -- it belongs to the process, not the response.
func (e *Error) Payload() map[string]any {
	return map[string]any{
		"ok":         false,
		"error":      e.Message,
		"message":    e.Message,
		"status":     e.Status,
		"error_code": e.Code,
		"retryable":  e.Retryable,
	}
}

func exitCodeFor(code string) int {
	if value, ok := ExitCodes[code]; ok {
		return value
	}
	return UnknownExitCode
}
