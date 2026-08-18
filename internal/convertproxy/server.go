// Package convertproxy serves local protocol conversion endpoints.
package convertproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Enabled               bool     `json:"enabled"`
	Listen                string   `json:"listen"`
	APIKey                string   `json:"api_key"`
	Models                []string `json:"models"`
	TargetBaseURL         string   `json:"target_base_url"`
	TargetModel           string   `json:"target_model"`
	TargetReasoningEffort string   `json:"target_reasoning_effort"`
	TargetAPIKey          string   `json:"-"`
}

type Server struct {
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
	cfg         Config
	http        *http.Server
	listener    net.Listener
	client      *http.Client
}

type streamedToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

func New(client *http.Client) *Server {
	if client == nil {
		client = http.DefaultClient
	}
	return &Server{client: client}
}
func (s *Server) SetConfig(cfg Config) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	s.mu.Lock()
	old := s.http
	oldListener := s.listener
	s.http = nil
	s.listener = nil
	s.mu.Unlock()
	if old != nil {
		if err := shutdownHTTPServer(old); err != nil {
			return err
		}
	}
	if oldListener != nil {
		_ = oldListener.Close()
	}
	if !cfg.Enabled {
		s.mu.Lock()
		s.cfg = cfg
		s.mu.Unlock()
		return nil
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		cfg.Enabled = false
		s.mu.Lock()
		s.cfg = cfg
		s.mu.Unlock()
		return err
	}
	server := &http.Server{Addr: cfg.Listen, Handler: http.HandlerFunc(s.handle)}
	s.mu.Lock()
	s.cfg = cfg
	s.http = server
	s.listener = listener
	s.mu.Unlock()
	go func() { _ = server.Serve(listener) }()
	return nil
}
func (s *Server) Close() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	s.mu.Lock()
	server := s.http
	listener := s.listener
	s.http = nil
	s.listener = nil
	s.cfg.Enabled = false
	s.mu.Unlock()
	if server == nil {
		if listener != nil {
			_ = listener.Close()
		}
		return nil
	}
	err := shutdownHTTPServer(server)
	if listener != nil {
		_ = listener.Close()
	}
	return err
}

func shutdownHTTPServer(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	authorized := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == cfg.APIKey || r.Header.Get("x-api-key") == cfg.APIKey
	if cfg.APIKey != "" && !authorized {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet && (r.URL.Path == "/models" || strings.HasSuffix(r.URL.Path, "/v1/models")) {
		models := make([]map[string]any, 0, len(cfg.Models))
		for _, id := range cfg.Models {
			if strings.TrimSpace(id) == "" {
				continue
			}
			models = append(models, map[string]any{"id": id, "object": "model", "created": 0, "owned_by": "bootagent"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
		return
	}
	format := ""
	switch {
	case strings.HasSuffix(r.URL.Path, "/messages"):
		format = "anthropic"
	case strings.HasSuffix(r.URL.Path, "/responses"):
		format = "responses"
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		format = "chat"
	default:
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if format == "chat" {
		s.forward(w, r, body, cfg)
		return
	}
	converted, err := ToChat(format, body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.forwardConverted(w, r, converted, cfg, format)
}
func (s *Server) forward(w http.ResponseWriter, r *http.Request, body []byte, cfg Config) {
	s.forwardConverted(w, r, body, cfg, "chat")
}
func (s *Server) forwardConverted(w http.ResponseWriter, r *http.Request, body []byte, cfg Config, format string) {
	if cfg.TargetModel != "" {
		if format == "chat" {
			body = withTargetModel(body, cfg.TargetModel)
		} else {
			body = withTargetConfig(body, cfg.TargetModel, cfg.TargetReasoningEffort)
		}
	}
	target := strings.TrimRight(cfg.TargetBaseURL, "/") + "/chat/completions"
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cfg.TargetAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.TargetAPIKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if format == "responses" && resp.StatusCode < 300 && streamRequested(body) && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		s.forwardResponsesStream(w, resp, modelFromRequest(body))
		return
	}
	if format == "anthropic" && resp.StatusCode < 300 && streamRequested(body) && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		s.forwardAnthropicStream(w, resp, modelFromRequest(body))
		return
	}
	data, _ := io.ReadAll(resp.Body)
	if format != "chat" && resp.StatusCode < 300 {
		data, _ = FromChat(format, data)
	}
	maps.Copy(w.Header(), resp.Header)
	// The body is read and written again (and may be transformed or transparently
	// decompressed), so upstream framing/encoding metadata is no longer valid.
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Encoding")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(data)
}

func (s *Server) forwardAnthropicStream(w http.ResponseWriter, resp *http.Response, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	writeEvent := func(event string, value any) {
		data, _ := json.Marshal(value)
		_, _ = io.WriteString(w, "event: "+event+"\ndata: "+string(data)+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	messageID := "msg_bootagent_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	writeEvent("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": messageID, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
	blockIndex := 0
	textStarted, thinkingStarted := false, false
	var tool *streamedToolCall
	var thinking strings.Builder
	var text strings.Builder
	toolIndex := -1
	activeKind := ""
	writeBlockDone := func(index int) {
		writeEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content        string `json:"content"`
					Reasoning      string `json:"reasoning_content"`
					ReasoningAlias string `json:"reasoning"`
					ToolCalls      []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		reasoning := delta.Reasoning
		if reasoning == "" {
			reasoning = delta.ReasoningAlias
		}
		if reasoning != "" {
			if !thinkingStarted {
				if activeKind != "" {
					writeBlockDone(blockIndex)
					blockIndex++
				}
				writeEvent("content_block_start", map[string]any{"type": "content_block_start", "index": blockIndex, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
				thinkingStarted = true
				activeKind = "thinking"
			}
			thinking.WriteString(reasoning)
			writeEvent("content_block_delta", map[string]any{"type": "content_block_delta", "index": blockIndex, "delta": map[string]any{"type": "thinking_delta", "thinking": reasoning}})
		}
		for _, call := range delta.ToolCalls {
			if tool == nil || toolIndex != call.Index {
				if activeKind != "" {
					writeBlockDone(blockIndex)
					blockIndex++
				}
				tool = &streamedToolCall{id: call.ID, name: call.Function.Name}
				toolIndex = call.Index
				writeEvent("content_block_start", map[string]any{"type": "content_block_start", "index": blockIndex, "content_block": map[string]any{"type": "tool_use", "id": tool.id, "name": tool.name, "input": map[string]any{}}})
				activeKind = "tool"
			}
			if call.ID != "" {
				tool.id = call.ID
			}
			if call.Function.Name != "" {
				tool.name = call.Function.Name
			}
			tool.arguments.WriteString(call.Function.Arguments)
			if call.Function.Arguments != "" {
				writeEvent("content_block_delta", map[string]any{"type": "content_block_delta", "index": blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": call.Function.Arguments}})
			}
		}
		if delta.Content != "" {
			if activeKind != "text" {
				if activeKind != "" {
					writeBlockDone(blockIndex)
					blockIndex++
				}
				activeKind = "text"
			}
			if !textStarted {
				writeEvent("content_block_start", map[string]any{"type": "content_block_start", "index": blockIndex, "content_block": map[string]any{"type": "text", "text": ""}})
				textStarted = true
			}
			text.WriteString(delta.Content)
			writeEvent("content_block_delta", map[string]any{"type": "content_block_delta", "index": blockIndex, "delta": map[string]any{"type": "text_delta", "text": delta.Content}})
		}
		if chunk.Choices[0].FinishReason != "" {
			break
		}
	}
	if activeKind != "" {
		writeBlockDone(blockIndex)
	}
	stopReason := "end_turn"
	if tool != nil {
		stopReason = "tool_use"
	}
	writeEvent("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 0}})
	writeEvent("message_stop", map[string]any{"type": "message_stop"})
}

func withTargetModel(body []byte, model string) []byte {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return body
	}
	request["model"] = model
	updated, err := json.Marshal(request)
	if err != nil {
		return body
	}
	return updated
}

func withTargetConfig(body []byte, model, reasoningEffort string) []byte {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return body
	}
	request["model"] = model
	if reasoningEffort != "" {
		request["reasoning_effort"] = reasoningEffort
	} else {
		delete(request, "reasoning_effort")
	}
	if reasoningModel(model) {
		if maxTokens, ok := request["max_tokens"]; ok {
			request["max_completion_tokens"] = maxTokens
			delete(request, "max_tokens")
		}
	} else if maxTokens, ok := request["max_completion_tokens"]; ok {
		request["max_tokens"] = maxTokens
		delete(request, "max_completion_tokens")
	}
	updated, err := json.Marshal(request)
	if err != nil {
		return body
	}
	return updated
}

func streamRequested(body []byte) bool {
	var request struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &request) == nil && request.Stream
}

func modelFromRequest(body []byte) string {
	var request struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &request)
	return request.Model
}

func (s *Server) forwardResponsesStream(w http.ResponseWriter, resp *http.Response, model string) {
	maps.Copy(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Del("Content-Length")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	responseID := "resp_bootagent_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	messageID := responseID + "_msg"
	reasoningID := responseID + "_reasoning"
	writeEvent := func(event string, value any) {
		data, _ := json.Marshal(value)
		_, _ = io.WriteString(w, "event: "+event+"\ndata: "+string(data)+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	response := map[string]any{"id": responseID, "object": "response", "status": "in_progress", "model": model, "output": []any{}}
	writeEvent("response.created", map[string]any{"type": "response.created", "response": response})
	messageAdded := false
	contentAdded := false
	reasoningAdded := false
	var reasoningText strings.Builder
	var text strings.Builder
	tools := map[int]*streamedToolCall{}
	maxToolIndex := -1
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content        string `json:"content"`
					Reasoning      string `json:"reasoning_content"`
					ReasoningAlias string `json:"reasoning"`
					ToolCalls      []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		reasoning := chunk.Choices[0].Delta.Reasoning
		if reasoning == "" {
			reasoning = chunk.Choices[0].Delta.ReasoningAlias
		}
		if reasoning != "" {
			if !reasoningAdded {
				writeEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": reasoningID, "type": "reasoning", "status": "in_progress", "summary": []any{}}})
				writeEvent("response.reasoning_summary_part.added", map[string]any{"type": "response.reasoning_summary_part.added", "item_id": reasoningID, "output_index": 0, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": ""}})
				reasoningAdded = true
			}
			reasoningText.WriteString(reasoning)
			writeEvent("response.reasoning_summary_text.delta", map[string]any{"type": "response.reasoning_summary_text.delta", "item_id": reasoningID, "output_index": 0, "summary_index": 0, "delta": reasoning})
		}
		delta := chunk.Choices[0].Delta.Content
		for _, tool := range chunk.Choices[0].Delta.ToolCalls {
			if tool.Index > maxToolIndex {
				maxToolIndex = tool.Index
			}
			state := tools[tool.Index]
			if state == nil {
				state = &streamedToolCall{}
				tools[tool.Index] = state
			}
			if tool.ID != "" {
				state.id = tool.ID
			}
			if tool.Function.Name != "" {
				state.name = tool.Function.Name
			}
			state.arguments.WriteString(tool.Function.Arguments)
		}
		if delta == "" {
			continue
		}
		textIndex := 0
		if reasoningAdded {
			textIndex = 1
		}
		if !messageAdded {
			writeEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": textIndex, "item": map[string]any{"id": messageID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
			messageAdded = true
		}
		if !contentAdded {
			writeEvent("response.content_part.added", map[string]any{"type": "response.content_part.added", "item_id": messageID, "output_index": textIndex, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
			contentAdded = true
		}
		text.WriteString(delta)
		writeEvent("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": messageID, "output_index": textIndex, "content_index": 0, "delta": delta, "logprobs": []any{}})
	}
	if reasoningAdded {
		writeEvent("response.reasoning_summary_text.done", map[string]any{"type": "response.reasoning_summary_text.done", "item_id": reasoningID, "output_index": 0, "summary_index": 0, "text": reasoningText.String()})
		writeEvent("response.reasoning_summary_part.done", map[string]any{"type": "response.reasoning_summary_part.done", "item_id": reasoningID, "output_index": 0, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": reasoningText.String()}})
		writeEvent("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"id": reasoningID, "type": "reasoning", "status": "completed", "summary": []any{map[string]any{"type": "summary_text", "text": reasoningText.String()}}}})
	}
	if messageAdded {
		textIndex := 0
		if reasoningAdded {
			textIndex = 1
		}
		writeEvent("response.output_text.done", map[string]any{"type": "response.output_text.done", "item_id": messageID, "output_index": textIndex, "content_index": 0, "text": text.String()})
		writeEvent("response.content_part.done", map[string]any{"type": "response.content_part.done", "item_id": messageID, "output_index": textIndex, "content_index": 0, "part": map[string]any{"type": "output_text", "text": text.String(), "annotations": []any{}}})
		writeEvent("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": textIndex, "item": map[string]any{"id": messageID, "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text.String(), "annotations": []any{}}}}})
	}
	textIndex := 0
	if reasoningAdded {
		textIndex = 1
	}
	toolOutput := textIndex + 1
	for index := 0; index <= maxToolIndex; index++ {
		state, ok := tools[index]
		if !ok {
			break
		}
		if state.id == "" || state.name == "" {
			continue
		}
		item := map[string]any{"id": state.id, "type": "function_call", "status": "completed", "call_id": state.id, "name": state.name, "arguments": state.arguments.String()}
		writeEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": toolOutput, "item": map[string]any{"id": state.id, "type": "function_call", "status": "in_progress", "call_id": state.id, "name": state.name, "arguments": ""}})
		writeEvent("response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "item_id": state.id, "output_index": toolOutput, "delta": state.arguments.String()})
		writeEvent("response.function_call_arguments.done", map[string]any{"type": "response.function_call_arguments.done", "item_id": state.id, "output_index": toolOutput, "arguments": state.arguments.String()})
		writeEvent("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": toolOutput, "item": item})
		toolOutput++
	}
	response["status"] = "completed"
	if reasoningAdded || messageAdded || maxToolIndex >= 0 {
		output := []any{}
		if reasoningAdded {
			output = append(output, map[string]any{"id": reasoningID, "type": "reasoning", "status": "completed", "summary": []any{map[string]any{"type": "summary_text", "text": reasoningText.String()}}})
		}
		if messageAdded {
			output = append(output,
				map[string]any{
					"id":      messageID,
					"type":    "message",
					"status":  "completed",
					"role":    "assistant",
					"content": []any{map[string]any{"type": "output_text", "text": text.String(), "annotations": []any{}}},
				},
			)
		}
		for index := 0; index <= maxToolIndex; index++ {
			state, ok := tools[index]
			if !ok {
				break
			}
			if state.id != "" && state.name != "" {
				output = append(output, map[string]any{"id": state.id, "type": "function_call", "status": "completed", "call_id": state.id, "name": state.name, "arguments": state.arguments.String()})
			}
		}
		response["output"] = output
	}
	writeEvent("response.completed", map[string]any{"type": "response.completed", "response": response})
}
