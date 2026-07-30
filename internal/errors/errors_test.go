package oneerrors

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestStableErrorShapeAndExitCode(t *testing.T) {
	err := New(ConfigWriteFailed, "configuration write failed", WithStatus(422), WithRetryable(true))
	if err.ExitCode != 5 || err.Status != 422 || !err.Retryable {
		t.Fatalf("unexpected error: %#v", err)
	}
	var wire map[string]any
	if marshalErr := json.Unmarshal(Marshal(err), &wire); marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if wire["error_code"] != ConfigWriteFailed || wire["exit_code"] != float64(5) {
		t.Fatalf("unexpected wire shape: %#v", wire)
	}
	if _, exists := wire["cause"]; exists {
		t.Fatal("internal cause must not cross the bridge")
	}
}

func TestAsGeneralizesUnknownErrors(t *testing.T) {
	sentinel := errors.New("secret process output")
	err := As(sentinel)
	if err.Code != InternalError || err.ExitCode != 10 || !errors.Is(err, sentinel) {
		t.Fatalf("unexpected generalized error: %#v", err)
	}
	if err.Message == sentinel.Error() {
		t.Fatal("unknown error details leaked into the stable message")
	}
}

func TestUnknownCodeFallsBackToInternal(t *testing.T) {
	err := New("NOT_A_CODE", "ignored")
	if err.Code != InternalError || err.ExitCode != ExitCodes[InternalError] {
		t.Fatalf("unexpected fallback: %#v", err)
	}
}
