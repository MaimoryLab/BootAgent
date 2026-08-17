package convertproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
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
