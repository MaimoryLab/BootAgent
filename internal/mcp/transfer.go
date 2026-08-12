package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
)

type SecretMode string

const (
	SecretOmit      SecretMode = "omit"
	SecretEncrypted SecretMode = "encrypted"
	SecretPlaintext SecretMode = "plaintext"
)

type ExportOptions struct {
	Mode             SecretMode
	Password         string
	ConfirmPlaintext bool
	ServerIDs        []string
}

type transferEnvelope struct {
	SchemaVersion int        `json:"schema_version"`
	Mode          SecretMode `json:"secret_mode"`
	Registry      *Registry  `json:"registry,omitempty"`
	Payload       []byte     `json:"payload,omitempty"`
}

func Export(r Registry, options ExportOptions) ([]byte, error) {
	if len(options.ServerIDs) > 0 {
		selected := make(map[string]bool, len(options.ServerIDs))
		for _, id := range options.ServerIDs {
			selected[id] = true
		}
		servers := make(map[string]ServerFact, len(selected))
		for id, fact := range r.Servers {
			if selected[id] {
				servers[id] = fact
			}
		}
		r.Servers = servers
	}
	for id, fact := range r.Servers {
		for i := range fact.Variants {
			fact.Variants[i].Agents = nil
		}
		r.Servers[id] = fact
	}
	if err := validateRegistry(r); err != nil {
		return nil, err
	}
	if options.Mode == "" {
		options.Mode = SecretOmit
	}
	e := transferEnvelope{SchemaVersion: RegistrySchemaVersion, Mode: options.Mode}
	switch options.Mode {
	case SecretOmit:
		redacted := redactRegistry(r)
		e.Registry = &redacted
	case SecretPlaintext:
		if !options.ConfirmPlaintext {
			return nil, errors.New("plaintext MCP export requires confirmation")
		}
		e.Registry = &r
	case SecretEncrypted:
		if options.Password == "" {
			return nil, errors.New("encrypted MCP export requires a password")
		}
		plain, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		payload, err := EncryptPayload(options.Password, plain)
		if err != nil {
			return nil, err
		}
		e.Payload = payload
	default:
		return nil, fmt.Errorf("unsupported MCP secret mode %q", options.Mode)
	}
	return json.MarshalIndent(e, "", "  ")
}

func Import(data []byte, password string) (Registry, error) {
	if len(data) > maxPayloadBytes {
		return Registry{}, errors.New("MCP transfer exceeds size limit")
	}
	var e transferEnvelope
	if err := json.Unmarshal(data, &e); err != nil {
		return Registry{}, errors.New("MCP transfer is invalid")
	}
	if e.SchemaVersion != RegistrySchemaVersion {
		return Registry{}, fmt.Errorf("unsupported MCP transfer schema version %d", e.SchemaVersion)
	}
	var r Registry
	switch e.Mode {
	case SecretOmit, SecretPlaintext:
		if e.Registry == nil {
			return Registry{}, errors.New("MCP transfer registry is missing")
		}
		r = *e.Registry
	case SecretEncrypted:
		if len(e.Payload) == 0 {
			return Registry{}, errors.New("MCP encrypted payload is missing")
		}
		plain, err := DecryptPayload(password, e.Payload)
		if err != nil {
			return Registry{}, err
		}
		if err := json.Unmarshal(plain, &r); err != nil {
			return Registry{}, errors.New("MCP encrypted registry is invalid")
		}
	default:
		return Registry{}, fmt.Errorf("unsupported MCP secret mode %q", e.Mode)
	}
	if err := validateRegistry(r); err != nil {
		return Registry{}, err
	}
	return r, nil
}

func redactRegistry(r Registry) Registry {
	out := Registry{SchemaVersion: RegistrySchemaVersion, Servers: make(map[string]ServerFact, len(r.Servers))}
	for id, fact := range r.Servers {
		variants := make([]Variant, len(fact.Variants))
		for i, v := range fact.Variants {
			variants[i] = v
			variants[i].Agents = append([]string(nil), v.Agents...)
			variants[i].Spec, _ = RedactSpec(v.Spec)
		}
		out.Servers[id] = ServerFact{Variants: variants}
	}
	return out
}
