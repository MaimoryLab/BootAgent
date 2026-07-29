package jsonorder

import (
	"encoding/json"
	"testing"
)

func TestALargeIntegerIsNotDegradedToAFloat(t *testing.T) {
	// Python keeps integer precision without limit, and so must this: turning a
	// user's ID into 1.2345678901234567e+19 would corrupt it silently.
	const huge = "12345678901234567890123456789"
	if got := renderNumber(json.Number(huge)); got != huge {
		t.Fatalf("got %s, want the integer unchanged", got)
	}
}

func TestAnUnparseableNumberIsLeftAsWrittenRatherThanGuessed(t *testing.T) {
	// Not reachable through Parse, which would have rejected the document. This
	// covers the defensive branch: leaving the text alone keeps the file valid,
	// where substituting a guess would not.
	if got := renderNumber(json.Number("1e")); got != "1e" {
		t.Errorf("got %q, want the original text", got)
	}
}
