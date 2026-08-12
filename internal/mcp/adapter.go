package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/tailscale/hujson"
	"gopkg.in/yaml.v3"
)

type Adapter interface {
	Read(ctx context.Context, path string) (Observed, error)
	Apply(ctx context.Context, path string, current []byte, changes map[string]*Spec) ([]byte, bool, error)
}

type Observed struct{ Servers map[string]ObservedServer }
type ObservedServer struct {
	Spec   Spec
	Native json.RawMessage
}

func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

type JSONAdapter struct {
	Section string
	Decode  func([]byte) (Spec, error)
	Encode  func(Spec) (any, error)
}

func (a JSONAdapter) Read(ctx context.Context, path string) (Observed, error) {
	select {
	case <-ctx.Done():
		return Observed{}, ctx.Err()
	default:
	}
	b, err := readFile(path)
	if err != nil {
		return Observed{}, fmt.Errorf("read MCP config: %w", err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return Observed{Servers: map[string]ObservedServer{}}, nil
	}
	v, err := hujson.Parse(b)
	if err != nil {
		return Observed{}, fmt.Errorf("invalid MCP config: %w", err)
	}
	return readJSONSection(v.Find("/"+a.Section), a.Decode)
}

func readJSONSection(v *hujson.Value, decode func([]byte) (Spec, error)) (Observed, error) {
	result := Observed{Servers: map[string]ObservedServer{}}
	if v == nil {
		return result, nil
	}
	v.Standardize()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(v.Pack(), &raw); err != nil {
		return Observed{}, fmt.Errorf("MCP section must be an object: %w", err)
	}
	for id, entry := range raw {
		if decode == nil {
			decode = decodeSpec
		}
		spec, err := decode(entry)
		if err != nil {
			return Observed{}, fmt.Errorf("MCP server %q: %w", id, err)
		}
		result.Servers[id] = ObservedServer{Spec: spec, Native: append(json.RawMessage(nil), entry...)}
	}
	return result, nil
}

func (a JSONAdapter) Apply(ctx context.Context, path string, current []byte, changes map[string]*Spec) ([]byte, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}
	if len(current) == 0 {
		current = []byte("{}")
	}
	v, err := hujson.Parse(current)
	if err != nil {
		return nil, false, fmt.Errorf("invalid MCP config: %w", err)
	}
	if v.Find("/"+a.Section) == nil {
		patch, _ := json.Marshal([]map[string]any{{"op": "add", "path": "/" + a.Section, "value": map[string]any{}}})
		if err := v.Patch(patch); err != nil {
			return nil, false, err
		}
	}
	for id, spec := range changes {
		ptr := "/" + a.Section + "/" + escapeJSONPointer(id)
		var op string
		var value any
		if spec == nil {
			op = "remove"
		} else {
			op = "add"
			if a.Encode != nil {
				value, err = a.Encode(*spec)
			} else {
				value, err = specValue(*spec)
			}
			if err != nil {
				return nil, false, err
			}
		}
		item := map[string]any{"op": op, "path": ptr}
		if op == "add" {
			item["value"] = value
		}
		patch, _ := json.Marshal([]any{item})
		if err := v.Patch(patch); err != nil {
			if spec == nil && strings.Contains(err.Error(), "does not exist") {
				continue
			}
			return nil, false, fmt.Errorf("patch MCP server %q: %w", id, err)
		}
	}
	v.Format()
	return v.Pack(), hasSecrets(changes), nil
}

func decodeSpec(raw []byte) (Spec, error) {
	var s Spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return Spec{}, err
	}
	return Normalize(s)
}

func specValue(s Spec) (map[string]any, error) {
	n, err := Normalize(s)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(n)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func hasSecrets(changes map[string]*Spec) bool {
	for _, s := range changes {
		if s != nil && (len(s.Env) > 0 || len(s.Headers) > 0) {
			return true
		}
	}
	return false
}

func escapeJSONPointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

type TOMLAdapter struct {
	Section string
	Decode  func([]byte) (Spec, error)
	Encode  func(Spec) (map[string]any, error)
}

func (a TOMLAdapter) Read(ctx context.Context, path string) (Observed, error) {
	b, err := readFile(path)
	if err != nil {
		return Observed{}, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return Observed{Servers: map[string]ObservedServer{}}, nil
	}
	var root map[string]any
	if err := toml.Unmarshal(b, &root); err != nil {
		return Observed{}, fmt.Errorf("invalid TOML MCP config: %w", err)
	}
	return readMapSection(root[a.Section], a.Decode)
}
func (a TOMLAdapter) Apply(ctx context.Context, path string, current []byte, changes map[string]*Spec) ([]byte, bool, error) {
	var root map[string]any
	if len(strings.TrimSpace(string(current))) > 0 {
		if err := toml.Unmarshal(current, &root); err != nil {
			return nil, false, err
		}
	}
	if root == nil {
		root = map[string]any{}
	}
	section, _ := root[a.Section].(map[string]any)
	if section == nil {
		section = map[string]any{}
		root[a.Section] = section
	}
	for id, spec := range changes {
		if spec == nil {
			delete(section, id)
			continue
		}
		value, err := encodeTOMLSpec(*spec, a.Encode)
		if err != nil {
			return nil, false, err
		}
		section[id] = value
	}
	b, err := toml.Marshal(root)
	return b, hasSecrets(changes), err
}

type YAMLAdapter struct{ Section string }

func (a YAMLAdapter) Read(ctx context.Context, path string) (Observed, error) {
	b, err := readFile(path)
	if err != nil {
		return Observed{}, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return Observed{Servers: map[string]ObservedServer{}}, nil
	}
	var root map[string]any
	if err := yaml.Unmarshal(b, &root); err != nil {
		return Observed{}, fmt.Errorf("invalid YAML MCP config: %w", err)
	}
	return readMapSection(root[a.Section], nil)
}
func (a YAMLAdapter) Apply(ctx context.Context, path string, current []byte, changes map[string]*Spec) ([]byte, bool, error) {
	var root map[string]any
	if len(strings.TrimSpace(string(current))) > 0 {
		if err := yaml.Unmarshal(current, &root); err != nil {
			return nil, false, err
		}
	}
	if root == nil {
		root = map[string]any{}
	}
	section, _ := root[a.Section].(map[string]any)
	if section == nil {
		section = map[string]any{}
		root[a.Section] = section
	}
	for id, spec := range changes {
		if spec == nil {
			delete(section, id)
			continue
		}
		value, err := specValue(*spec)
		if err != nil {
			return nil, false, err
		}
		section[id] = value
	}
	b, err := yaml.Marshal(root)
	return b, hasSecrets(changes), err
}

func readMapSection(value any, decode func([]byte) (Spec, error)) (Observed, error) {
	result := Observed{Servers: map[string]ObservedServer{}}
	m, ok := value.(map[string]any)
	if value == nil {
		return result, nil
	}
	if !ok {
		return Observed{}, errors.New("MCP section must be an object")
	}
	for id, entry := range m {
		b, err := json.Marshal(entry)
		if err != nil {
			return Observed{}, err
		}
		if decode == nil {
			decode = decodeSpec
		}
		s, err := decode(b)
		if err != nil {
			return Observed{}, fmt.Errorf("MCP server %q: %w", id, err)
		}
		result.Servers[id] = ObservedServer{Spec: s, Native: b}
	}
	return result, nil
}

func encodeTOMLSpec(spec Spec, encode func(Spec) (map[string]any, error)) (map[string]any, error) {
	if encode != nil {
		return encode(spec)
	}
	return specValue(spec)
}
