package shellquote

import (
	"errors"
	"strings"
)

// ErrUnbalancedQuote is what shlex reports as ValueError("No closing quotation").
//
// Only a hand edit produces it -- Posix never emits an unbalanced quote -- but the
// credential files are user-editable by design, so the error is part of the
// contract rather than an impossible case. Python raised it uncaught, which
// reached the CLI as a traceback; both sides now report no key held.
var ErrUnbalancedQuote = errors.New("no closing quotation")

// SplitPosix reverses Posix, following shlex.split's POSIX rules.
//
// Needed because the credential is read back out of a file that was written with
// shlex.quote, and trimming the surrounding quotes is not the inverse: a key
// containing a quote was written as 'a'"'"'b', which trimming turns into
// a'"'"'b rather than a'b.
//
// Comments are not recognised, matching shlex.split's default of comments=False:
// a key containing '#' is a key, not the start of a comment.
func SplitPosix(input string) ([]string, error) {
	fields := []string{}
	var current strings.Builder
	started := false

	const (
		outside = iota
		inSingle
		inDouble
	)
	state := outside

	runes := []rune(input)
	for index := 0; index < len(runes); index++ {
		char := runes[index]

		switch state {
		case outside:
			switch {
			case char == ' ' || char == '\t' || char == '\n' || char == '\r':
				if started {
					fields = append(fields, current.String())
					current.Reset()
					started = false
				}
			case char == '\'':
				state, started = inSingle, true
			case char == '"':
				state, started = inDouble, true
			case char == '\\':
				started = true
				// A trailing backslash is an error, not a dropped character: shlex
				// treats the escape as an open state and reports the same "no
				// closing quotation" it does for an open quote. I had this as a
				// silent drop, and the comparison against the real splitter is
				// what corrected it.
				if index+1 >= len(runes) {
					return nil, ErrUnbalancedQuote
				}
				index++
				current.WriteRune(runes[index])
			default:
				started = true
				current.WriteRune(char)
			}

		case inSingle:
			// Nothing is special inside single quotes, not even a backslash.
			if char == '\'' {
				state = outside
			} else {
				current.WriteRune(char)
			}

		case inDouble:
			switch {
			case char == '"':
				state = outside
			case char == '\\' && index+1 < len(runes):
				next := runes[index+1]
				// Inside double quotes a backslash only escapes a quote or another
				// backslash. Before anything else it stays literal, which is why
				// this is not the same branch as the unquoted case.
				if next == '"' || next == '\\' {
					index++
					current.WriteRune(next)
				} else {
					current.WriteRune(char)
				}
			default:
				current.WriteRune(char)
			}
		}
	}

	if state != outside {
		return nil, ErrUnbalancedQuote
	}
	if started {
		fields = append(fields, current.String())
	}
	return fields, nil
}

// FirstPosixField is the first field of a POSIX-quoted line, or "" when the line
// holds none or cannot be parsed.
//
// The whole line is parsed even though only the first field is wanted, because
// shlex.split rejects an unbalanced quote anywhere in the input -- stopping early
// would accept a line Python refuses.
func FirstPosixField(input string) string {
	fields, err := SplitPosix(input)
	if err != nil || len(fields) == 0 {
		return ""
	}
	return fields[0]
}
