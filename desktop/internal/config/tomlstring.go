package config

import (
	"fmt"
	"strings"
)

// tomlString renders a TOML basic string the way the Python adapter does.
//
// That adapter builds the TOML by hand and quotes each value with json.dumps --
// no ensure_ascii=False, unlike the JSON adapters. So a non-ASCII model name
// becomes an escape sequence here while the same name stays UTF-8 in
// settings.json. The two are not interchangeable, and using one helper for both
// would produce a file that differs from Python's on exactly the inputs nobody
// tests with.
//
// TOML basic strings accept the same escapes JSON does, so the output is valid
// TOML as well as byte-identical.
func tomlString(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		default:
			switch {
			case char < 0x20:
				builder.WriteString(fmt.Sprintf(`\u%04x`, char))
			case char < 0x7f:
				builder.WriteRune(char)
			case char > 0xffff:
				// Python emits a surrogate pair for anything outside the BMP,
				// because json.dumps targets UTF-16 escapes.
				offset := char - 0x10000
				high := 0xd800 + (offset >> 10)
				low := 0xdc00 + (offset & 0x3ff)
				builder.WriteString(fmt.Sprintf(`\u%04x\u%04x`, high, low))
			default:
				builder.WriteString(fmt.Sprintf(`\u%04x`, char))
			}
		}
	}
	builder.WriteByte('"')
	return builder.String()
}
