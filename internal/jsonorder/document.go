// Package jsonorder edits JSON objects without reordering them.
//
// encoding/json sorts map keys when it writes them. Config adapters use this
// package so rewriting a user's file changes only the fields OneAgent manages.
package jsonorder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Object is a JSON object that remembers the order its keys arrived in.
// Existing keys are updated in place; new keys are appended.
type Object struct {
	keys   []string
	values map[string]any
}

// NewObject builds an empty object.
func NewObject() *Object {
	return &Object{values: map[string]any{}}
}

// Len reports the number of keys in the object.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys returns keys in their current order.
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

// Set updates an existing key in place, or appends a new key.
func (o *Object) Set(key string, value any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, present := o.values[key]; !present {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Delete removes a key and preserves the order of the remaining keys.
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
			return
		}
	}
}

// Child returns the nested object at key, creating it when absent. It refuses
// to replace a non-object value because doing so would discard user data.
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

// GetString returns a string value, or an empty string for another type.
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

// MarshalJSON lets an Object nested in an ordinary struct retain its contents.
func (o *Object) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	var buffer bytes.Buffer
	if err := writeValue(&buffer, o, 0); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// UnmarshalJSON reads an object and records key order.
func (o *Object) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
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
	_, err := decoder.Token()
	return err
}

func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); ok {
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
	return token, nil
}

// Parse reads a JSON object, preserving key order.
func Parse(raw []byte) (*Object, error) {
	object := NewObject()
	if err := json.Unmarshal(raw, object); err != nil {
		return nil, err
	}
	return object, nil
}

// Marshal renders an object with two-space indentation and one trailing
// newline, matching json.dumps(..., ensure_ascii=False, indent=2) plus the
// newline used by OneAgent's config files.
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

func writeString(out io.Writer, value string) error {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range value {
		switch character {
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
			if character < 0x20 {
				builder.WriteString(fmt.Sprintf(`\u%04x`, character))
				continue
			}
			builder.WriteRune(character)
		}
	}
	builder.WriteByte('"')
	_, err := io.WriteString(out, builder.String())
	return err
}

// SortedKeys returns a sorted copy for diagnostics without changing output
// order.
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
