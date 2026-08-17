package convertproxy

import (
	"encoding/json"
	"fmt"
	"strings"
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
	model := stringValue(in["model"])
	if v, ok := in["max_output_tokens"]; ok {
		if reasoningModel(model) {
			out["max_completion_tokens"] = v
		} else {
			out["max_tokens"] = v
		}
	}
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
		out["tools"] = responsesTools(v)
	}
	out["messages"] = responsesMessages(in)
	if reasoning, ok := in["reasoning"].(map[string]any); ok {
		if effort := stringValue(reasoning["effort"]); effort != "" {
			out["reasoning_effort"] = effort
		}
	}
	if stream, ok := in["stream"]; ok {
		out["stream"] = stream
	}
	return out
}

func responsesMessages(in map[string]any) []any {
	messages := []any{}
	if instructions := instructionText(in["instructions"]); instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}
	pendingReasoning := ""
	pendingCalls := []any{}
	lastAssistant := -1
	flushReasoning := func() {
		if pendingReasoning == "" || lastAssistant < 0 {
			return
		}
		if message, ok := messages[lastAssistant].(map[string]any); ok {
			message["reasoning_content"] = joinReasoning(stringValue(message["reasoning_content"]), pendingReasoning)
		}
		pendingReasoning = ""
	}
	flushCalls := func() {
		if len(pendingCalls) == 0 {
			return
		}
		message := map[string]any{"role": "assistant", "content": nil, "tool_calls": pendingCalls}
		if pendingReasoning != "" {
			message["reasoning_content"] = pendingReasoning
			pendingReasoning = ""
		}
		messages = append(messages, message)
		lastAssistant = len(messages) - 1
		pendingCalls = nil
	}
	appendItem := func(item map[string]any) {
		typeName := stringValue(item["type"])
		switch typeName {
		case "reasoning":
			pendingReasoning = joinReasoning(pendingReasoning, reasoningItemText(item))
		case "function_call":
			callID := item["call_id"]
			if callID == nil {
				callID = item["id"]
			}
			pendingCalls = append(pendingCalls, map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": item["name"], "arguments": item["arguments"]}})
		case "message":
			role := responsesRole(stringValue(item["role"]))
			if role != "assistant" {
				flushReasoning()
				flushCalls()
			}
			message := map[string]any{"role": role, "content": responsesContent(item["content"])}
			if role == "assistant" && pendingReasoning != "" {
				message["reasoning_content"] = pendingReasoning
				pendingReasoning = ""
			}
			messages = append(messages, message)
			if role == "assistant" {
				lastAssistant = len(messages) - 1
			}
		case "input_text", "input_image", "input_file", "input_audio":
			flushReasoning()
			flushCalls()
			messages = append(messages, map[string]any{"role": "user", "content": responsesContent([]any{item})})
		case "function_call_output", "custom_tool_call_output", "tool_search_output":
			flushReasoning()
			flushCalls()
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": item["call_id"], "content": toolOutput(item["output"])})
		}
	}
	switch input := in["input"].(type) {
	case string:
		if input != "" {
			messages = append(messages, map[string]any{"role": "user", "content": input})
		}
	case []any:
		for _, raw := range input {
			if item, ok := raw.(map[string]any); ok {
				appendItem(item)
			}
		}
	case map[string]any:
		appendItem(input)
	}
	flushCalls()
	flushReasoning()
	return messages
}

func instructionText(v any) string {
	if text, ok := v.(string); ok {
		return text
	}
	parts := []string{}
	for _, raw := range array(v) {
		if item, ok := raw.(map[string]any); ok && stringValue(item["text"]) != "" {
			parts = append(parts, stringValue(item["text"]))
		}
	}
	return strings.Join(parts, "\n\n")
}

func responsesRole(role string) string {
	if role == "developer" || role == "system" {
		return "system"
	}
	if role == "assistant" || role == "tool" {
		return role
	}
	return "user"
}

func reasoningItemText(item map[string]any) string {
	if text := stringValue(item["text"]); text != "" {
		return text
	}
	parts := []string{}
	for _, raw := range array(item["summary"]) {
		if part, ok := raw.(map[string]any); ok && stringValue(part["text"]) != "" {
			parts = append(parts, stringValue(part["text"]))
		}
	}
	return strings.Join(parts, "\n\n")
}

func joinReasoning(existing, next string) string {
	if strings.TrimSpace(next) == "" {
		return existing
	}
	if existing == "" {
		return next
	}
	return existing + "\n\n" + next
}

func toolOutput(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return jsonString(value)
}

func responsesContent(v any) any {
	if text, ok := v.(string); ok {
		return text
	}
	parts := []any{}
	for _, raw := range array(v) {
		if b, ok := raw.(map[string]any); ok {
			switch stringValue(b["type"]) {
			case "input_text", "output_text", "text":
				parts = append(parts, map[string]any{"type": "text", "text": b["text"]})
			case "input_image":
				imageURL := b["image_url"]
				if _, ok := imageURL.(map[string]any); !ok {
					imageURL = map[string]any{"url": imageURL}
				}
				parts = append(parts, map[string]any{"type": "image_url", "image_url": imageURL})
			case "refusal":
				parts = append(parts, map[string]any{"type": "text", "text": b["refusal"]})
			}
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
			appendResponseTool(&out, b, "")
		}
	}
	return out
}

func appendResponseTool(out *[]any, tool map[string]any, namespace string) {
	typeName := stringValue(tool["type"])
	switch typeName {
	case "function":
		name := stringValue(tool["name"])
		if name == "" {
			return
		}
		if namespace != "" {
			name = namespace + "_" + name
		}
		parameters := tool["parameters"]
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		fn := map[string]any{"name": name, "description": tool["description"], "parameters": parameters}
		if strict, exists := tool["strict"]; exists {
			fn["strict"] = strict
		}
		*out = append(*out, map[string]any{"type": "function", "function": fn})
	case "custom":
		name := stringValue(tool["name"])
		if name == "" {
			return
		}
		*out = append(*out, map[string]any{"type": "function", "function": map[string]any{
			"name": name, "description": tool["description"], "parameters": map[string]any{"type": "object", "properties": map[string]any{"input": map[string]any{"type": "string"}}, "required": []string{"input"}},
		}})
	case "namespace":
		children, _ := tool["tools"].([]any)
		if children == nil {
			children, _ = tool["children"].([]any)
		}
		for _, child := range children {
			if childMap, ok := child.(map[string]any); ok {
				appendResponseTool(out, childMap, stringValue(tool["name"]))
			}
		}
	case "tool_search":
		*out = append(*out, map[string]any{"type": "function", "function": map[string]any{
			"name": "tool_search", "description": "Search available tools", "parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}},
		}})
	}
}
func array(v any) []any        { a, _ := v.([]any); return a }
func stringValue(v any) string { s, _ := v.(string); return s }
func jsonString(v any) string  { b, _ := json.Marshal(v); return string(b) }
func reasoningModel(model string) bool {
	model = strings.ToLower(model)
	return strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") || strings.HasPrefix(model, "gpt-5")
}

func FromChat(format string, body []byte) ([]byte, error) {
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	switch format {
	case "anthropic":
		return json.Marshal(map[string]any{"id": in["id"], "type": "message", "role": "assistant", "model": in["model"], "content": chatContent(in["choices"]), "stop_reason": firstChoice(in, "finish_reason"), "usage": anthropicUsage(in["usage"])})
	case "responses":
		return json.Marshal(chatResponseToResponses(in))
	default:
		return body, nil
	}
}

func chatResponseToResponses(in map[string]any) map[string]any {
	choice := firstChoiceMap(in)
	message, _ := choice["message"].(map[string]any)
	output := []any{}
	if reasoning := chatReasoning(message); reasoning != "" {
		output = append(output, map[string]any{"id": stringValue(in["id"]) + "_reasoning", "type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": reasoning}}})
	}
	if text := chatMessageText(message); text != "" {
		output = append(output, map[string]any{"id": stringValue(in["id"]) + "_msg", "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}})
	}
	for _, raw := range array(message["tool_calls"]) {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		output = append(output, map[string]any{"id": call["id"], "type": "function_call", "status": "completed", "call_id": call["id"], "name": fn["name"], "arguments": fn["arguments"]})
	}
	status := "completed"
	if stringValue(choice["finish_reason"]) == "length" {
		status = "incomplete"
	}
	response := map[string]any{"id": in["id"], "object": "response", "status": status, "model": in["model"], "output": output}
	if status == "incomplete" {
		response["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	}
	if usage, ok := in["usage"].(map[string]any); ok {
		response["usage"] = map[string]any{"input_tokens": usage["prompt_tokens"], "output_tokens": usage["completion_tokens"], "total_tokens": usage["total_tokens"]}
	}
	return response
}

func firstChoiceMap(in map[string]any) map[string]any {
	for _, raw := range array(in["choices"]) {
		if choice, ok := raw.(map[string]any); ok {
			return choice
		}
	}
	return map[string]any{}
}

func chatReasoning(message map[string]any) string {
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if value := stringValue(message[key]); value != "" {
			return value
		}
	}
	text := stringValue(message["content"])
	if strings.HasPrefix(strings.TrimSpace(text), "<think>") {
		text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "<think>"))
		if end := strings.Index(text, "</think>"); end >= 0 {
			return strings.TrimSpace(text[:end])
		}
	}
	return ""
}

func chatMessageText(message map[string]any) string {
	text := stringValue(message["content"])
	if strings.HasPrefix(strings.TrimSpace(text), "<think>") {
		trimmed := strings.TrimSpace(text)
		if end := strings.Index(trimmed, "</think>"); end >= 0 {
			return strings.TrimSpace(trimmed[end+len("</think>"):])
		}
	}
	return text
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
