package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

type AgentBinding struct {
	SchemaVersion int
	AgentID       string
	Provider      string
	BaseURL       string
	Model         string
	ProfileRef    string
	CreatedAt     string
	UpdatedAt     string
}

type BindingWriteRequest struct {
	Provider   string
	BaseURL    string
	Model      string
	ProfileRef string
}

type storedBinding struct {
	SchemaVersion int    `json:"schema_version"`
	AgentID       string `json:"agent_id"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"base_url"`
	Model         string `json:"model"`
	ProfileRef    string `json:"profile_ref"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func (s Store) AgentsPath() string {
	return filepath.Join(s.Root(), "agents")
}

func (s Store) AgentBindingPath(agentID string) (string, error) {
	if err := ValidateID(agentID); err != nil {
		return "", oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Invalid Agent ID: %s", agentID))
	}
	return filepath.Join(s.AgentsPath(), agentID+".json"), nil
}

func (s Store) ReadAgentBinding(agentID string) (*AgentBinding, error) {
	path, err := s.AgentBindingPath(agentID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read Agent binding for %s: %w", agentID, err)
	}
	var stored storedBinding
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("Agent binding for %s is corrupt", agentID)
	}
	if stored.SchemaVersion != 1 {
		return nil, fmt.Errorf("Unsupported Agent binding schema for %s", agentID)
	}
	if stored.AgentID != agentID {
		return nil, fmt.Errorf("Agent binding for %s is corrupt", agentID)
	}
	return &AgentBinding{
		SchemaVersion: stored.SchemaVersion,
		AgentID:       stored.AgentID,
		Provider:      stored.Provider,
		BaseURL:       stored.BaseURL,
		Model:         stored.Model,
		ProfileRef:    stored.ProfileRef,
		CreatedAt:     stored.CreatedAt,
		UpdatedAt:     stored.UpdatedAt,
	}, nil
}

func (s Store) ListAgentBindings() map[string]AgentBinding {
	entries, err := os.ReadDir(s.AgentsPath())
	if err != nil {
		return map[string]AgentBinding{}
	}
	result := make(map[string]AgentBinding)
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if ValidateID(id) != nil {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		binding, err := s.ReadAgentBinding(id)
		if err == nil && binding != nil {
			result[id] = *binding
		}
	}
	return result
}

func (s Store) WriteAgentBinding(ctx context.Context, agentID string, request BindingWriteRequest) (AgentBinding, error) {
	if err := requestContext(ctx); err != nil {
		return AgentBinding{}, err
	}
	path, err := s.AgentBindingPath(agentID)
	if err != nil {
		return AgentBinding{}, err
	}
	if strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.BaseURL) == "" || strings.TrimSpace(request.Model) == "" {
		return AgentBinding{}, oneerrors.New(oneerrors.InvalidRequest, "Agent binding requires provider, base URL, and model")
	}
	existing, _ := s.ReadAgentBinding(agentID)
	now := s.clock().UTC().Format(time.RFC3339)
	profileRef := request.ProfileRef
	created := now
	if existing != nil {
		if profileRef == "" {
			profileRef = existing.ProfileRef
		}
		if existing.CreatedAt != "" {
			created = existing.CreatedAt
		}
	}
	stored := storedBinding{
		SchemaVersion: 1,
		AgentID:       agentID,
		Provider:      request.Provider,
		BaseURL:       request.BaseURL,
		Model:         request.Model,
		ProfileRef:    profileRef,
		CreatedAt:     created,
		UpdatedAt:     now,
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return AgentBinding{}, oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot encode Agent binding")
	}
	data = append(data, '\n')
	if _, err := s.filesystem().AtomicWrite(ctx, path, data, false); err != nil {
		return AgentBinding{}, err
	}
	return AgentBinding{
		SchemaVersion: stored.SchemaVersion,
		AgentID:       stored.AgentID,
		Provider:      stored.Provider,
		BaseURL:       stored.BaseURL,
		Model:         stored.Model,
		ProfileRef:    stored.ProfileRef,
		CreatedAt:     stored.CreatedAt,
		UpdatedAt:     stored.UpdatedAt,
	}, nil
}
