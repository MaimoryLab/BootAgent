package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/config"
	"github.com/pelletier/go-toml/v2"
)

type CodexAdapter struct{ TOMLAdapter }

func NewCodexAdapter() Adapter {
	return CodexAdapter{TOMLAdapter{Section: "mcp_servers", Decode: decodeCodex, Encode: encodeCodex, ApplyFunc: applyCodex}}
}

func applyCodex(ctx context.Context, _ string, current []byte, changes map[string]*Spec) ([]byte, bool, error) {
	text := string(current)
	for id, spec := range changes {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		var err error
		text, err = patchCodexServer(text, id, spec)
		if err != nil {
			return nil, false, err
		}
	}
	return []byte(text), hasSecrets(changes), nil
}

func patchCodexServer(text, id string, spec *Spec) (string, error) {
	managed := ""
	if spec != nil {
		lines, err := codexManagedLines(id, *spec)
		if err != nil {
			return "", err
		}
		managedLines := append([]string(nil), lines...)
		sectionHeader := "[mcp_servers." + id + "]"
		inputLines := strings.Split(text, "\n")
		for i, line := range inputLines {
			if strings.TrimSpace(line) != sectionHeader {
				continue
			}
			for _, bodyLine := range inputLines[i+1:] {
				trimmed := strings.TrimSpace(bodyLine)
				if strings.HasPrefix(trimmed, "[") {
					break
				}
				key := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
				if key != "command" && key != "args" && key != "env" && key != "cwd" && key != "url" && key != "http_headers" && key != "headers" {
					managedLines = append(managedLines, bodyLine)
				}
			}
			break
		}
		managed = strings.Join(managedLines, "\n")
	}
	return config.MergeCodexMCPTOML(text, managed, "Codex MCP configuration", id)
}

func codexManagedLines(id string, spec Spec) ([]string, error) {
	value, err := encodeCodex(spec)
	if err != nil {
		return nil, err
	}
	b, err := toml.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Codex MCP: %w", err)
	}
	body := strings.TrimRight(string(b), "\n")
	body = strings.ReplaceAll(body, "[env]", "[mcp_servers."+id+".env]")
	body = strings.ReplaceAll(body, "[http_headers]", "[mcp_servers."+id+".http_headers]")
	return append([]string{"[mcp_servers." + id + "]"}, strings.Split(body, "\n")...), nil
}

func decodeCodex(raw []byte) (Spec, error) {
	var native struct {
		Type        string            `json:"type"`
		Command     string            `json:"command"`
		Args        []string          `json:"args"`
		Env         map[string]string `json:"env"`
		Cwd         string            `json:"cwd"`
		URL         string            `json:"url"`
		HTTPHeaders map[string]string `json:"http_headers"`
		Headers     map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &native); err != nil {
		return Spec{}, err
	}
	headers := native.HTTPHeaders
	if headers == nil {
		headers = native.Headers
	}
	return Normalize(Spec{Type: native.Type, Command: native.Command, Args: native.Args, Env: native.Env, Cwd: native.Cwd, URL: native.URL, Headers: headers})
}

func encodeCodex(spec Spec) (map[string]any, error) {
	value, err := specValue(spec)
	if err != nil {
		return nil, err
	}
	if headers, ok := value["headers"]; ok {
		value["http_headers"] = headers
		delete(value, "headers")
	}
	return value, nil
}
