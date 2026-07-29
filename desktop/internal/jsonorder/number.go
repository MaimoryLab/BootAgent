package jsonorder

import (
	"encoding/json"
	"strconv"
	"strings"
)

// renderNumber reproduces what a number looks like after a round trip through
// Python's json module.
//
// Keeping the original text would be the obvious choice, and it is wrong:
// json.loads turns any number carrying an exponent into a float, and json.dumps
// then writes repr() of that float. So "1e10" in a user's file comes back as
// "10000000000.0", and "1e-5" as "1e-05". Integers and plain decimals do
// round-trip unchanged, which is why this only diverts the exponent forms.
//
// The values that reach here come from a user's existing config -- timeouts,
// token limits, temperatures. Rewriting one of those is exactly the kind of
// silent edit the "preserve fields we do not manage" rule exists to prevent, so
// the formatting has to match rather than merely parse.
func renderNumber(number json.Number) string {
	text := number.String()
	if !hasExponent(text) {
		// Integers and plain decimals survive Python's round trip byte for byte,
		// including values too large for float64.
		return text
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		// Not a number Go can parse. Leaving it as written is better than
		// guessing; the file still round-trips even if this one value would
		// differ from Python.
		return text
	}
	return pythonFloat(value)
}

func hasExponent(text string) bool {
	return strings.ContainsAny(text, "eE")
}

// pythonFloat formats a float the way repr() does: the shortest representation
// that round-trips, in exponent form when the decimal exponent is below -4 or at
// least 16, and always with a decimal point or exponent so it cannot be mistaken
// for an integer.
func pythonFloat(value float64) string {
	if value != value { // NaN
		return "NaN"
	}
	if value > maxFloat {
		return "Infinity"
	}
	if value < -maxFloat {
		return "-Infinity"
	}

	if exponent := decimalExponent(value); exponent < -4 || exponent >= 16 {
		// Go writes "1e-05" and "1e+16" here, matching Python's two-digit
		// exponent with an explicit sign.
		return strconv.FormatFloat(value, 'e', -1, 64)
	}
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(text, ".") {
		// Python never renders a float without one, so "300" becomes "300.0".
		text += ".0"
	}
	return text
}

const maxFloat = 1.7976931348623157e308

// decimalExponent reports the power of ten in the shortest exponent form, which
// is what decides between fixed and scientific notation.
func decimalExponent(value float64) int {
	if value == 0 {
		return 0
	}
	text := strconv.FormatFloat(value, 'e', -1, 64)
	index := strings.IndexAny(text, "eE")
	if index < 0 {
		return 0
	}
	exponent, err := strconv.Atoi(text[index+1:])
	if err != nil {
		return 0
	}
	return exponent
}
