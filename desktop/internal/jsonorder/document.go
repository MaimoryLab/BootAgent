// Package jsonorder edits JSON objects without reordering them.
//
// Go's encoding/json serialises a map with its keys sorted, and unmarshalling
// into a map discards the order the file had. Either alone would rewrite a
// user's config with their own keys rearranged -- a diff full of noise, and a
// difference from what the Python core produces for the same input.
//
// Two other defaults here would also diverge silently: the standard encoder
// escapes <, > and & to <, > and &, and MarshalIndent omits the
// trailing newline the config files carry. Both are handled below.
package jsonorder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Object is a JSON object that remembers the order its keys arrived in. Setting
// an existing key updates it in place; a new key appends. That is what Python
// dicts do, and it is why a rewritten config keeps the user's layout.
type Object struct {
	keys   []string
	values map[string]any
}

// NewObject builds an empty object.
func NewObject() *Object {
	return &Object{values: map[string]any{}}
}

// Len reports how many keys the object holds.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys returns the keys in their current order.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), o.keys...)
}

// Get returns a value and whether the key is present.
func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	value, present := o.values[key]
	return value, present
}

// Set updates an existing key in place, or appends a new one.
func (o *Object) Set(key string, value any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, present := o.values[key]; !present {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Delete removes a key, preserving the order of the rest.
func (o *Object) Delete(key string) {
	if o == nil {
		return
	}
	if _, present := o.values[key]; !present {
		return
	}
	delete(o.values, key)
	for index, existing := range o.keys {
		if existing == key {
			o.keys = append(o.keys[:index], o.keys[index+1:]...)
			break
		}
	}
}

// Child returns the nested object at key, creating it when absent.
//
// It reports an error when the key holds something that is not an object,
// because overwriting a user's value with a table would lose data the file was
// carrying. Python raises CONFIG_WRITE_FAILED in the same situation.
func (o *Object) Child(key string) (*Object, error) {
	existing, present := o.Get(key)
	if !present || existing == nil {
		child := NewObject()
		o.Set(key, child)
		return child, nil
	}
	child, ok := existing.(*Object)
	if !ok {
		return nil, fmt.Errorf("%q is %s, not an object", key, describe(existing))
	}
	return child, nil
}

// GetString returns a string value, or "" when the key is absent or holds
// another type. Config files are user-editable, so a wrongly typed field must
// read as absent rather than crash or stringify.
func (o *Object) GetString(key string) string {
	value, present := o.Get(key)
	if !present {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

// GetObject returns a nested object without creating one.
func (o *Object) GetObject(key string) (*Object, bool) {
	value, present := o.Get(key)
	if !present {
		return nil, false
	}
	child, ok := value.(*Object)
	return child, ok
}

// UnmarshalJSON reads an object, recording key order.
func (o *Object) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// Numbers are kept as their original text so a round trip does not turn 1
	// into 1e+00 or lose precision on a large integer.
	decoder.UseNumber()

	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("expected a JSON object")
	}
	o.keys = nil
	o.values = map[string]any{}
	return o.decodeInto(decoder)
}

func (o *Object) decodeInto(decoder *json.Decoder) error {
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("expected an object key")
		}
		value, err := decodeValue(decoder)
		if err != nil {
			return err
		}
		o.Set(key, value)
	}
	// Consume the closing brace.
	_, err := decoder.Token()
	return err
}

func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		child := NewObject()
		if err := child.decodeInto(decoder); err != nil {
			return nil, err
		}
		return child, nil
	case '[':
		items := []any{}
		for decoder.More() {
			item, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unexpected %v", delimiter)
	}
}

// Parse reads a JSON object, preserving key order.
func Parse(raw []byte) (*Object, error) {
	object := NewObject()
	if err := json.Unmarshal(raw, object); err != nil {
		return nil, err
	}
	return object, nil
}

// Marshal renders the object the way json.dumps(ensure_ascii=False, indent=2)
// does, including the trailing newline the config files carry.
//
// Written by hand rather than through Encoder because the differences from Go's
// defaults are exactly the ones that would go unnoticed: HTML escaping, sorted
// keys, and the missing final newline.
func Marshal(object *Object) ([]byte, error) {
	var buffer bytes.Buffer
	if err := writeValue(&buffer, object, 0); err != nil {
		return nil, err
	}
	buffer.WriteByte('\n')
	return buffer.Bytes(), nil
}

const indentStep = "  "

func writeValue(out io.Writer, value any, depth int) error {
	switch typed := value.(type) {
	case *Object:
		return writeObject(out, typed, depth)
	case []any:
		return writeArray(out, typed, depth)
	case nil:
		_, err := io.WriteString(out, "null")
		return err
	case string:
		return writeString(out, typed)
	case bool:
		if typed {
			_, err := io.WriteString(out, "true")
			return err
		}
		_, err := io.WriteString(out, "false")
		return err
	case json.Number:
		_, err := io.WriteString(out, renderNumber(typed))
		return err
	default:
		// Values the core itself sets (int, float64) rather than values read
		// back from a file.
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		_, err = out.Write(encoded)
		return err
	}
}

func writeObject(out io.Writer, object *Object, depth int) error {
	if object == nil || len(object.keys) == 0 {
		_, err := io.WriteString(out, "{}")
		return err
	}
	if _, err := io.WriteString(out, "{\n"); err != nil {
		return err
	}
	inner := strings.Repeat(indentStep, depth+1)
	for index, key := range object.keys {
		if _, err := io.WriteString(out, inner); err != nil {
			return err
		}
		if err := writeString(out, key); err != nil {
			return err
		}
		if _, err := io.WriteString(out, ": "); err != nil {
			return err
		}
		if err := writeValue(out, object.values[key], depth+1); err != nil {
			return err
		}
		if index < len(object.keys)-1 {
			if _, err := io.WriteString(out, ","); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, "\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(out, strings.Repeat(indentStep, depth)+"}")
	return err
}

func writeArray(out io.Writer, items []any, depth int) error {
	if len(items) == 0 {
		_, err := io.WriteString(out, "[]")
		return err
	}
	if _, err := io.WriteString(out, "[\n"); err != nil {
		return err
	}
	inner := strings.Repeat(indentStep, depth+1)
	for index, item := range items {
		if _, err := io.WriteString(out, inner); err != nil {
			return err
		}
		if err := writeValue(out, item, depth+1); err != nil {
			return err
		}
		if index < len(items)-1 {
			if _, err := io.WriteString(out, ","); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, "\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(out, strings.Repeat(indentStep, depth)+"]")
	return err
}

// writeString renders a JSON string the way Python does with
// ensure_ascii=False: non-ASCII stays as UTF-8, and <, > and & are *not*
// escaped, unlike Go's default encoder.
func writeString(out io.Writer, value string) error {
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
			if char < 0x20 {
				// Python emits lower-case hex here, and so must this.
				builder.WriteString(fmt.Sprintf(`\u%04x`, char))
				continue
			}
			builder.WriteRune(char)
		}
	}
	builder.WriteByte('"')
	_, err := io.WriteString(out, builder.String())
	return err
}

// SortedKeys is for tests and diagnostics that want a stable listing without
// caring about the file's order.
func SortedKeys(object *Object) []string {
	keys := object.Keys()
	sort.Strings(keys)
	return keys
}

func describe(value any) string {
	switch value.(type) {
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case json.Number:
		return "a number"
	case []any:
		return "an array"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", value)
	}
}
