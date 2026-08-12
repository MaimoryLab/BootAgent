package mcp

import (
	"encoding/json"
	"fmt"
)

type OpenCodeAdapter struct{ JSONAdapter }

func NewOpenCodeAdapter() Adapter {
	return OpenCodeAdapter{JSONAdapter{Section: "mcp", Decode: decodeOpenCode, Encode: encodeOpenCode}}
}

func decodeOpenCode(raw []byte) (Spec, error) {
	var native struct {
		Type    string            `json:"type"`
		Command json.RawMessage   `json:"command"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Env     map[string]string `json:"environment"`
	}
	if err := json.Unmarshal(raw, &native); err != nil {
		return Spec{}, err
	}
	if native.Type == "local" {
		var command []string
		if err := json.Unmarshal(native.Command, &command); err != nil {
			return Spec{}, fmt.Errorf("OpenCode local command must be an array: %w", err)
		}
		if len(command) == 0 {
			return Spec{}, fmt.Errorf("OpenCode local command is empty")
		}
		return Normalize(Spec{Type: "stdio", Command: command[0], Args: command[1:], Env: native.Env})
	}
	if native.Type == "remote" {
		return Normalize(Spec{Type: "http", URL: native.URL, Headers: native.Headers})
	}
	return decodeSpec(raw)
}

func encodeOpenCode(spec Spec) (any, error) {
	n, err := Normalize(spec)
	if err != nil {
		return nil, err
	}
	if n.Type == "stdio" {
		return map[string]any{"type": "local", "command": append([]string{n.Command}, n.Args...), "environment": n.Env, "enabled": true}, nil
	}
	return map[string]any{"type": "remote", "url": n.URL, "headers": n.Headers, "enabled": true}, nil
}
