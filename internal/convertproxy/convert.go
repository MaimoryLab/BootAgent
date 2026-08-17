package convertproxy

import (
	"encoding/json"
	"fmt"
)

// ToChat converts the two supported client request formats to Chat Completions.
func ToChat(format string, body []byte) ([]byte, error) {
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	switch format {
	case "anthropic":
		return json.Marshal(anthropicRequest(in))
	case "responses":
		return json.Marshal(responsesRequest(in))
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func anthropicRequest(in map[string]any) map[string]any {
	out := map[string]any{"model": in["model"]}
	if v, ok := in["max_tokens"]; ok {
		out["max_tokens"] = v
	}
	if v, ok := in["temperature"]; ok {
		out["temperature"] = v
	}
	if v, ok := in["top_p"]; ok {
		out["top_p"] = v
	}
	if v, ok := in["tools"]; ok {
		out["tools"] = anthropicTools(v)
	}
	messages := []any{}
	if system, ok := in["system"].(string); ok && system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	for _, raw := range array(in["messages"]) {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := stringValue(m["role"])
		content := m["content"]
		messages = append(messages, map[string]any{"role": role, "content": anthropicContent(content)})
	}
	out["messages"] = messages
	if stream, ok := in["stream"]; ok {
		out["stream"] = stream
	}
	return out
}

func anthropicContent(v any) any {
	if text, ok := v.(string); ok {
		return text
	}
	parts := []any{}
	for _, raw := range array(v) {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch stringValue(b["type"]) {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": b["text"]})
		case "tool_use":
			parts = append(parts, map[string]any{"type": "function", "function": map[string]any{"name": b["name"], "arguments": jsonString(b["input"])}, "id": b["id"]})
		case "image":
			if source, ok := b["source"].(map[string]any); ok {
				if data := stringValue(source["data"]); data != "" {
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + stringValue(source["media_type"]) + ";base64," + data}})
				}
			}
		}
	}
	return parts
}

func responsesRequest(in map[string]any) map[string]any {
	out := map[string]any{"model": in["model"]}
	if v, ok := in["temperature"]; ok {
		out["temperature"] = v
	}
	if v, ok := in["top_p"]; ok {
		out["top_p"] = v
	}
	if v, ok := in["tools"]; ok {
		out["tools"] = responsesTools(v)
	}
	messages := []any{}
	if instructions, ok := in["instructions"].(string); ok && instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}
	if input, ok := in["input"].(string); ok && input != "" {
		messages = append(messages, map[string]any{"role": "user", "content": input})
	}
	for _, raw := range array(in["input"]) {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch stringValue(b["type"]) {
		case "message":
			messages = append(messages, map[string]any{"role": stringValue(b["role"]), "content": responsesContent(b["content"])})
		case "function_call_output":
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": b["call_id"], "content": b["output"]})
		}
	}
	out["messages"] = messages
	if stream, ok := in["stream"]; ok {
		out["stream"] = stream
	}
	return out
}

func responsesContent(v any) any {
	if text, ok := v.(string); ok {
		return text
	}
	parts := []any{}
	for _, raw := range array(v) {
		if b, ok := raw.(map[string]any); ok && stringValue(b["type"]) == "input_text" {
			parts = append(parts, map[string]any{"type": "text", "text": b["text"]})
		}
	}
	return parts
}

func anthropicTools(v any) any {
	out := []any{}
	for _, raw := range array(v) {
		if b, ok := raw.(map[string]any); ok {
			out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": b["name"], "description": b["description"], "parameters": b["input_schema"]}})
		}
	}
	return out
}
func responsesTools(v any) any {
	out := []any{}
	for _, raw := range array(v) {
		if b, ok := raw.(map[string]any); ok {
			if stringValue(b["type"]) == "function" {
				fn := map[string]any{"name": b["name"], "description": b["description"], "parameters": b["parameters"]}
				if strict, exists := b["strict"]; exists {
					fn["strict"] = strict
				}
				out = append(out, map[string]any{"type": "function", "function": fn})
				continue
			}
			out = append(out, b)
		}
	}
	return out
}
func array(v any) []any        { a, _ := v.([]any); return a }
func stringValue(v any) string { s, _ := v.(string); return s }
func jsonString(v any) string  { b, _ := json.Marshal(v); return string(b) }

func FromChat(format string, body []byte) ([]byte, error) {
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	switch format {
	case "anthropic":
		return json.Marshal(map[string]any{"id": in["id"], "type": "message", "role": "assistant", "model": in["model"], "content": chatContent(in["choices"]), "stop_reason": firstChoice(in, "finish_reason"), "usage": anthropicUsage(in["usage"])})
	case "responses":
		return json.Marshal(map[string]any{"id": in["id"], "object": "response", "model": in["model"], "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": firstText(in)}}}}})
	default:
		return body, nil
	}
}
func firstChoice(in map[string]any, key string) any {
	for _, x := range array(in["choices"]) {
		if b, ok := x.(map[string]any); ok {
			return b[key]
		}
	}
	return nil
}
func chatContent(v any) []any {
	for _, x := range array(v) {
		if b, ok := x.(map[string]any); ok {
			if m, ok := b["message"].(map[string]any); ok {
				return []any{map[string]any{"type": "text", "text": m["content"]}}
			}
		}
	}
	return []any{}
}
func firstText(in map[string]any) any {
	for _, x := range array(in["choices"]) {
		if b, ok := x.(map[string]any); ok {
			if m, ok := b["message"].(map[string]any); ok {
				return m["content"]
			}
		}
	}
	return ""
}
func anthropicUsage(v any) map[string]any {
	if b, ok := v.(map[string]any); ok {
		return map[string]any{"input_tokens": b["prompt_tokens"], "output_tokens": b["completion_tokens"]}
	}
	return map[string]any{}
}
