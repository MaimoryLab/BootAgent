package mcp

import "encoding/json"

type CodexAdapter struct{ TOMLAdapter }

func NewCodexAdapter() Adapter {
	return CodexAdapter{TOMLAdapter{Section: "mcp_servers", Decode: decodeCodex, Encode: encodeCodex}}
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
