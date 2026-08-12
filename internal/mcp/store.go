package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

type Store struct {
	home string
	fs   securefs.Store
}

func NewStore(home string, filesystem securefs.Store) Store { return Store{home: home, fs: filesystem} }

func (s Store) Path() string { return filepath.Join(s.home, ".oneagent", "mcp.json") }

func (s Store) Load() (Registry, error) {
	empty := Registry{SchemaVersion: RegistrySchemaVersion, Servers: map[string]ServerFact{}}
	b, err := os.ReadFile(s.Path())
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return Registry{}, fmt.Errorf("cannot read MCP registry: %w", err)
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return Registry{}, errors.New("MCP registry is invalid")
	}
	if err := validateRegistry(r); err != nil {
		return Registry{}, err
	}
	return r, nil
}

func (s Store) Save(ctx context.Context, r Registry) error {
	if err := validateRegistry(r); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode MCP registry: %w", err)
	}
	_, err = s.fs.AtomicWrite(ctx, s.Path(), append(b, '\n'), true)
	return err
}

func validateRegistry(r Registry) error {
	if r.SchemaVersion != RegistrySchemaVersion {
		return fmt.Errorf("unsupported MCP registry schema version %d", r.SchemaVersion)
	}
	if r.Servers == nil {
		return errors.New("MCP registry servers are missing")
	}
	for id, fact := range r.Servers {
		if err := ValidateID(id); err != nil {
			return fmt.Errorf("invalid MCP server ID %q: %w", id, err)
		}
		for i, v := range fact.Variants {
			if len(v.Agents) == 0 {
				return fmt.Errorf("MCP server %q variant %d has no Agents", id, i)
			}
			if _, err := Normalize(v.Spec); err != nil {
				return fmt.Errorf("MCP server %q variant %d: %w", id, i, err)
			}
		}
	}
	return nil
}
