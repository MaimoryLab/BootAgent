package jsonorder

import (
	"encoding/json"
	"strconv"
	"strings"
)

// renderNumber preserves the legacy configuration number representation. Plain
// integers and decimals retain their text; exponent forms use the established
// fixed/scientific notation rules.
func renderNumber(number json.Number) string {
	text := number.String()
	if !strings.ContainsAny(text, "eE") {
		return text
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return text
	}
	return renderFloat(value)
}

func renderFloat(value float64) string {
	if value != value {
		return "NaN"
	}
	if value > maxFloat {
		return "Infinity"
	}
	if value < -maxFloat {
		return "-Infinity"
	}
	if exponent := decimalExponent(value); exponent < -4 || exponent >= 16 {
		return strconv.FormatFloat(value, 'e', -1, 64)
	}
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(text, ".") {
		text += ".0"
	}
	return text
}

const maxFloat = 1.7976931348623157e308

func decimalExponent(value float64) int {
	if value == 0 {
		return 0
	}
	text := strconv.FormatFloat(value, 'e', -1, 64)
	_, after, ok := strings.Cut(text, "e")
	if !ok {
		return 0
	}
	exponent, err := strconv.Atoi(after)
	if err != nil {
		return 0
	}
	return exponent
}
