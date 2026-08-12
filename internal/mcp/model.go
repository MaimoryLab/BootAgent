package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"
)

const RedactedValue = "[redacted]"
const RegistrySchemaVersion = 1

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

type Spec struct {
	Type       string                     `json:"type,omitempty"`
	Command    string                     `json:"command,omitempty"`
	Args       []string                   `json:"args,omitempty"`
	Env        map[string]string          `json:"env,omitempty"`
	Cwd        string                     `json:"cwd,omitempty"`
	URL        string                     `json:"url,omitempty"`
	Headers    map[string]string          `json:"headers,omitempty"`
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
}

type Variant struct {
	Agents []string `json:"agents"`
	Spec   Spec     `json:"spec"`
}

type ServerFact struct {
	Variants []Variant `json:"variants"`
}
type Registry struct {
	SchemaVersion int                   `json:"schema_version"`
	Servers       map[string]ServerFact `json:"servers"`
}

func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid MCP server ID")
	}
	return nil
}

func (s Spec) Normalized() (Spec, error) {
	n := s
	n.Type, n.Command, n.Cwd, n.URL = strings.ToLower(strings.TrimSpace(s.Type)), strings.TrimSpace(s.Command), strings.TrimSpace(s.Cwd), strings.TrimSpace(s.URL)
	if n.Type == "" && n.Command != "" {
		n.Type = "stdio"
	}
	if n.Type != "stdio" && n.Type != "http" && n.Type != "sse" {
		return Spec{}, errors.New("MCP transport must be stdio, http, or sse")
	}
	n.Env = cloneStrings(s.Env)
	n.Headers = cloneStrings(s.Headers)
	n.Args = append([]string(nil), s.Args...)
	n.Extensions = cloneRaw(s.Extensions)
	if n.Type == "stdio" {
		n.URL, n.Headers = "", nil
	} else {
		n.Command, n.Cwd, n.Args, n.Env = "", "", nil, nil
	}
	if n.Type == "stdio" && n.Command == "" {
		return Spec{}, errors.New("stdio MCP server requires command")
	}
	if n.Type != "stdio" && n.URL == "" {
		return Spec{}, errors.New("remote MCP server requires URL")
	}
	return n, nil
}

func Normalize(s Spec) (Spec, error) { return s.Normalized() }

func EqualNormalized(a, b Spec) bool {
	na, ea := Normalize(a)
	nb, eb := Normalize(b)
	if ea != nil || eb != nil {
		return false
	}
	ba, ea := canonicalJSON(na)
	bb, eb := canonicalJSON(nb)
	return ea == nil && eb == nil && bytes.Equal(ba, bb)
}

func canonicalJSON(v any) ([]byte, error) {
	return json.Marshal(canonicalValue(v))
}
func canonicalValue(v any) any {
	switch x := v.(type) {
	case Spec:
		return canonicalValue(map[string]any{"type": x.Type, "command": x.Command, "args": x.Args, "env": x.Env, "cwd": x.Cwd, "url": x.URL, "headers": x.Headers, "extensions": x.Extensions})
	case map[string]string:
		m := make(map[string]any, len(x))
		for k, v := range x {
			m[k] = v
		}
		return canonicalValue(m)
	case map[string]json.RawMessage:
		m := make(map[string]any, len(x))
		for k, raw := range x {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				m[k] = canonicalValue(v)
			} else {
				m[k] = string(raw)
			}
		}
		return canonicalValue(m)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m := make(map[string]any, len(x))
		for _, k := range keys {
			m[k] = canonicalValue(x[k])
		}
		return m
	case []any:
		for i := range x {
			x[i] = canonicalValue(x[i])
		}
		return x
	default:
		return v
	}
}

func RedactSpec(s Spec) (Spec, []string) {
	n := s
	n.Env = cloneStrings(s.Env)
	n.Headers = cloneStrings(s.Headers)
	n.Args = append([]string(nil), s.Args...)
	n.Extensions = cloneRaw(s.Extensions)
	paths := []string{}
	for k := range n.Env {
		n.Env[k] = RedactedValue
		paths = append(paths, "env."+k)
	}
	for k := range n.Headers {
		n.Headers[k] = RedactedValue
		paths = append(paths, "headers."+k)
	}
	for k, raw := range n.Extensions {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			v, p := redactValue(v, "extensions."+k)
			if len(p) > 0 {
				encoded, _ := json.Marshal(v)
				n.Extensions[k] = encoded
				paths = append(paths, p...)
			}
		}
	}
	sort.Strings(paths)
	return n, paths
}

func redactValue(v any, path string) (any, []string) {
	switch x := v.(type) {
	case map[string]any:
		paths := []string{}
		for k, val := range x {
			p := path + "." + k
			if secretName(k) {
				x[k] = RedactedValue
				paths = append(paths, p)
			} else {
				var more []string
				x[k], more = redactValue(val, p)
				paths = append(paths, more...)
			}
		}
		return x, paths
	case []any:
		paths := []string{}
		for i, val := range x {
			var more []string
			x[i], more = redactValue(val, fmt.Sprintf("%s[%d]", path, i))
			paths = append(paths, more...)
		}
		return x, paths
	default:
		return v, nil
	}
}

func secretName(name string) bool {
	n := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "-", "_"), " ", "_"))
	return n == "authorization" || n == "token" || n == "api_key" || n == "apikey" || n == "client_secret" || n == "clientsecret" || strings.Contains(n, "secret") || strings.Contains(n, "password")
}
func cloneStrings(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
func cloneRaw(in map[string]json.RawMessage) map[string]json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}
