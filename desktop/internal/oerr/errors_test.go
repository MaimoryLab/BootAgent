package oerr

import (
	"reflect"
	"testing"
)

func TestNewTakesItsExitCodeFromTheTable(t *testing.T) {
	err := New("CONFIG_WRITE_FAILED", "写入失败")
	if err.ExitCode != 5 {
		t.Fatalf("exit code = %d, want 5", err.ExitCode)
	}
	if err.Status != 400 {
		t.Fatalf("status = %d, want the 400 default", err.Status)
	}
	if err.Retryable {
		t.Fatal("a new error defaults to not retryable")
	}
}

func TestAnUnknownCodeFallsBackToTenRatherThanFailing(t *testing.T) {
	// Python reaches this through EXIT_CODES.get(code, 10). A caller that
	// invents a code still exits with something a shell can act on.
	err := New("NOT_A_REAL_CODE", "x")
	if err.ExitCode != UnknownExitCode {
		t.Fatalf("exit code = %d, want %d", err.ExitCode, UnknownExitCode)
	}
}

func TestTheThreeProviderCodesShareOneExitCode(t *testing.T) {
	// Everything the provider rejects is one class to a shell caller, so
	// splitting these would change the contract rather than refine it.
	for _, code := range []string{"API_KEY_REJECTED", "PROVIDER_UNREACHABLE", "MODELS_UNSUPPORTED"} {
		if got := ExitCodes[code]; got != 6 {
			t.Errorf("%s exits %d, want 6", code, got)
		}
	}
}

func TestPayloadHasExactlyTheSixKeysTheContractNames(t *testing.T) {
	payload := New("TIMEOUT", "超时", WithRetryable()).Payload()
	want := map[string]any{
		"ok":         false,
		"error":      "超时",
		"message":    "超时",
		"status":     400,
		"error_code": "TIMEOUT",
		"retryable":  true,
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = %#v, want %#v", payload, want)
	}
}

func TestPayloadCarriesNoExitCode(t *testing.T) {
	// The exit code belongs to the process, not the response body. Adding it
	// here would put a second, redundant contract in front of the frontend.
	if _, present := New("TIMEOUT", "x").Payload()["exit_code"]; present {
		t.Fatal("exit_code must not appear in the response payload")
	}
}

func TestTheMessageAppearsUnderBothErrorAndMessage(t *testing.T) {
	// Not redundancy to clean up: the frontend reads one key and older
	// callers read the other.
	payload := New("INVALID_REQUEST", "缺少参数").Payload()
	if payload["error"] != payload["message"] {
		t.Fatalf("error=%v message=%v, want both to carry the message", payload["error"], payload["message"])
	}
}

func TestErrorStringIsTheMessageAlone(t *testing.T) {
	// Anything appended here would end up in logs. The message is already
	// redacted; a wrapper carrying context might not be.
	err := New("API_KEY_REJECTED", "密钥被拒绝")
	if err.Error() != "密钥被拒绝" {
		t.Fatalf("Error() = %q, want the message alone", err.Error())
	}
}

func TestOptionsOverrideTheDefaults(t *testing.T) {
	err := New("PREREQUISITE_MISSING", "缺少 npm", WithRetryable(), WithStatus(503), WithExitCode(42))
	if !err.Retryable || err.Status != 503 || err.ExitCode != 42 {
		t.Fatalf("options did not apply: %+v", err)
	}
}

func TestAFormattedMessageTakesItsOptionsThroughSet(t *testing.T) {
	// Variadic format args and variadic options cannot both be last, so the
	// formatted constructor hands overrides to Set instead.
	err := Newf("PREREQUISITE_MISSING", "缺少 %s", "npm").Set(WithRetryable())
	if err.Message != "缺少 npm" {
		t.Fatalf("message = %q", err.Message)
	}
	if !err.Retryable {
		t.Error("Set did not apply the option")
	}
}

func TestErrorSatisfiesTheErrorInterface(t *testing.T) {
	var err error = New("INTERNAL_ERROR", "内部错误")
	if err.Error() == "" {
		t.Fatal("Error must be usable as a plain error")
	}
}
