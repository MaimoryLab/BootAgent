package convertproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Enabled       bool     `json:"enabled"`
	Listen        string   `json:"listen"`
	APIKey        string   `json:"api_key"`
	Models        []string `json:"models"`
	TargetBaseURL string   `json:"target_base_url"`
	TargetAPIKey  string   `json:"-"`
}

type Server struct {
	mu     sync.RWMutex
	cfg    Config
	http   *http.Server
	client *http.Client
}

func New(client *http.Client) *Server {
	if client == nil {
		client = http.DefaultClient
	}
	return &Server{client: client}
}
func (s *Server) SetConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.http != nil {
		_ = s.http.Shutdown(context.Background())
		s.http = nil
	}
	s.cfg = cfg
	if !cfg.Enabled {
		return nil
	}
	s.http = &http.Server{Addr: cfg.Listen, Handler: http.HandlerFunc(s.handle)}
	go s.http.ListenAndServe()
	return nil
}
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.http == nil {
		return nil
	}
	err := s.http.Shutdown(context.Background())
	s.http = nil
	return err
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
	target := strings.TrimRight(cfg.TargetBaseURL, "/") + "/chat/completions"
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cfg.TargetAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.TargetAPIKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()
	if format == "responses" && resp.StatusCode < 300 && streamRequested(body) && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		s.forwardResponsesStream(w, resp, modelFromRequest(body))
		return
	}
	data, _ := io.ReadAll(resp.Body)
	if format != "chat" && resp.StatusCode < 300 {
		data, _ = FromChat(format, data)
	}
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(data)
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
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Del("Content-Length")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	responseID := "resp_bootagent_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	messageID := responseID + "_msg"
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
	var text strings.Builder
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
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		if !messageAdded {
			writeEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": messageID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
			messageAdded = true
		}
		if !contentAdded {
			writeEvent("response.content_part.added", map[string]any{"type": "response.content_part.added", "item_id": messageID, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
			contentAdded = true
		}
		text.WriteString(delta)
		writeEvent("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": messageID, "output_index": 0, "content_index": 0, "delta": delta, "logprobs": []any{}})
	}
	if messageAdded {
		writeEvent("response.output_text.done", map[string]any{"type": "response.output_text.done", "item_id": messageID, "output_index": 0, "content_index": 0, "text": text.String()})
		writeEvent("response.content_part.done", map[string]any{"type": "response.content_part.done", "item_id": messageID, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": text.String(), "annotations": []any{}}})
		writeEvent("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"id": messageID, "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text.String(), "annotations": []any{}}}}})
	}
	response["status"] = "completed"
	if messageAdded {
		response["output"] = []any{
			map[string]any{
				"id":      messageID,
				"type":    "message",
				"status":  "completed",
				"role":    "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": text.String(), "annotations": []any{}}},
			},
		}
	}
	writeEvent("response.completed", map[string]any{"type": "response.completed", "response": response})
}
