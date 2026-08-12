package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/mcp"
)

type mcpDraftState struct {
	dirty  atomic.Bool
	locale atomic.Value
}

func (u *UseCases) SetMCPDraftState(dirty bool, locale string) {
	if u == nil {
		return
	}
	if locale == "" {
		locale = "zh"
	}
	u.mcpDraft.dirty.Store(dirty)
	u.mcpDraft.locale.Store(locale)
}

func (u *UseCases) MCPDraftState() (bool, string) {
	if u == nil {
		return false, "zh"
	}
	locale, _ := u.mcpDraft.locale.Load().(string)
	if locale == "" {
		locale = "zh"
	}
	return u.mcpDraft.dirty.Load(), locale
}

type MCPServerSummary struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Agents     []string `json:"agents"`
	Variants   int      `json:"variants"`
	Conflict   bool     `json:"conflict"`
	HasSecrets bool     `json:"has_secrets"`
}

type MCPServerDetail struct {
	ID       string        `json:"id"`
	Source   string        `json:"source_agent,omitempty"`
	Variants []mcp.Variant `json:"variants"`
}

type MCPChange struct {
	ID     string    `json:"id"`
	Spec   *mcp.Spec `json:"spec,omitempty"`
	Agents []string  `json:"agents"`
	Delete bool      `json:"delete,omitempty"`
}

type MCPApplyRequest struct {
	Changes []MCPChange `json:"changes"`
}
type MCPAgentApplyResult struct {
	Agent           string `json:"agent"`
	ConfigUpdated   bool   `json:"config_updated"`
	RegistryUpdated bool   `json:"registry_updated"`
	Error           string `json:"error,omitempty"`
}
type MCPApplyResult struct {
	Results []MCPAgentApplyResult `json:"results"`
}
type MCPScanResult struct {
	Servers        []MCPServerSummary `json:"servers"`
	EligibleAgents []string           `json:"eligible_agents"`
	Diagnostics    []string           `json:"diagnostics,omitempty"`
}

func mcpPath(home, osID string, agent catalog.Agent) string {
	rel := agent.MCPConfigPath
	if osID == "windows" && agent.MCPWindowsConfigPath != "" {
		rel = agent.MCPWindowsConfigPath
	}
	if rel == "" {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(rel))
}

func (u *UseCases) eligibleMCPAgents() (map[string]catalog.Agent, error) {
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return nil, err
	}
	lookup := u.status.Lookup
	if lookup == nil && u.runner != nil {
		lookup = u.runner.LookPath
	}
	result := make(map[string]catalog.Agent)
	for id, agent := range manifest.Agents {
		if agent.MCPAdapter == "" || agent.Command == "" || !contains(agent.Platforms, u.status.Platform.OS) || lookup == nil {
			continue
		}
		if _, ok := lookup(agent.Command); !ok {
			continue
		}
		path := mcpPath(u.status.Home, u.status.Platform.OS, agent)
		if path == "" {
			continue
		}
		if _, err := os.Stat(filepath.Dir(path)); err != nil {
			continue
		}
		result[id] = agent
	}
	return result, nil
}

func mcpAdapter(agent catalog.Agent) mcp.Adapter {
	switch agent.MCPAdapter {
	case "claude":
		return mcp.NewClaudeAdapter()
	case "codex":
		return mcp.NewCodexAdapter()
	case "opencode":
		return mcp.NewOpenCodeAdapter()
	case "kilo":
		return mcp.NewKiloAdapter()
	case "hermes":
		return mcp.NewHermesAdapter()
	default:
		return nil
	}
}

func (u *UseCases) mcpStore() mcp.Store { return mcp.NewStore(u.status.Home, u.filesystem) }

func (u *UseCases) ListMCP(ctx context.Context) ([]MCPServerSummary, error) {
	if err := contextError(ctx, "MCP request was cancelled"); err != nil {
		return nil, err
	}
	r, err := u.mcpStore().Load()
	if err != nil {
		return nil, err
	}
	return summarizeMCP(r), nil
}

func summarizeMCP(r mcp.Registry) []MCPServerSummary {
	ids := make([]string, 0, len(r.Servers))
	for id := range r.Servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]MCPServerSummary, 0, len(ids))
	for _, id := range ids {
		fact := r.Servers[id]
		agents := map[string]bool{}
		typ := ""
		secret := false
		for _, v := range fact.Variants {
			for _, a := range v.Agents {
				agents[a] = true
			}
			if typ == "" {
				typ = v.Spec.Type
			}
			secret = secret || len(v.Spec.Env) > 0 || len(v.Spec.Headers) > 0
		}
		al := make([]string, 0, len(agents))
		for a := range agents {
			al = append(al, a)
		}
		sort.Strings(al)
		out = append(out, MCPServerSummary{ID: id, Type: typ, Agents: al, Variants: len(fact.Variants), Conflict: len(fact.Variants) > 1, HasSecrets: secret})
	}
	return out
}

func removeMCPAgent(r *mcp.Registry, agentID string) {
	for id, fact := range r.Servers {
		kept := fact.Variants[:0]
		for _, variant := range fact.Variants {
			agents := variant.Agents[:0]
			for _, existing := range variant.Agents {
				if existing != agentID {
					agents = append(agents, existing)
				}
			}
			variant.Agents = agents
			kept = append(kept, variant)
		}
		if len(kept) > 0 {
			fact.Variants = kept
			r.Servers[id] = fact
		}
	}
}

type mcpAgentFact struct {
	id   string
	spec mcp.Spec
}

func factsForMCPAgent(r mcp.Registry, agentID string) []mcpAgentFact {
	var facts []mcpAgentFact
	for id, server := range r.Servers {
		for _, variant := range server.Variants {
			if contains(variant.Agents, agentID) {
				facts = append(facts, mcpAgentFact{id: id, spec: variant.Spec})
			}
		}
	}
	return facts
}

func restoreMCPAgentFacts(r *mcp.Registry, agentID string, facts []mcpAgentFact) {
	for _, old := range facts {
		server := r.Servers[old.id]
		for i := range server.Variants {
			if mcp.EqualNormalized(server.Variants[i].Spec, old.spec) {
				if !contains(server.Variants[i].Agents, agentID) {
					server.Variants[i].Agents = append(server.Variants[i].Agents, agentID)
				}
				r.Servers[old.id] = server
				goto restored
			}
		}
		server.Variants = append(server.Variants, mcp.Variant{Agents: []string{agentID}, Spec: old.spec})
		r.Servers[old.id] = server
	restored:
	}
}

func mcpTargetAgents(fact mcp.ServerFact, selected []string, eligible map[string]catalog.Agent) []string {
	set := map[string]bool{}
	for _, variant := range fact.Variants {
		for _, agentID := range variant.Agents {
			if _, ok := eligible[agentID]; ok {
				set[agentID] = true
			}
		}
	}
	for _, agentID := range selected {
		if _, ok := eligible[agentID]; ok {
			set[agentID] = true
		}
	}
	out := make([]string, 0, len(set))
	for agentID := range set {
		out = append(out, agentID)
	}
	sort.Strings(out)
	return out
}

func collapseEmptyMCPVariants(fact *mcp.ServerFact) {
	hasAgent := false
	for _, variant := range fact.Variants {
		hasAgent = hasAgent || len(variant.Agents) > 0
	}
	kept := fact.Variants[:0]
	for _, variant := range fact.Variants {
		if len(variant.Agents) > 0 || (!hasAgent && len(kept) == 0) {
			kept = append(kept, variant)
		}
	}
	fact.Variants = kept
}

func (u *UseCases) ScanMCP(ctx context.Context) (MCPScanResult, error) {
	if err := contextError(ctx, "MCP scan was cancelled"); err != nil {
		return MCPScanResult{}, err
	}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	store := u.mcpStore()
	previous, err := store.Load()
	if err != nil {
		return MCPScanResult{}, err
	}
	eligible, err := u.eligibleMCPAgents()
	if err != nil {
		return MCPScanResult{}, err
	}
	merged := mcp.Registry{SchemaVersion: mcp.RegistrySchemaVersion, Servers: map[string]mcp.ServerFact{}}
	for id, fact := range previous.Servers {
		merged.Servers[id] = fact
	}
	oldFacts := make(map[string][]mcpAgentFact, len(eligible))
	for agentID := range eligible {
		oldFacts[agentID] = factsForMCPAgent(previous, agentID)
		removeMCPAgent(&merged, agentID)
	}
	diagnostics := []string{}
	for agentID, agent := range eligible {
		adapter := mcpAdapter(agent)
		path := mcpPath(u.status.Home, u.status.Platform.OS, agent)
		if adapter == nil {
			continue
		}
		observed, readErr := adapter.Read(ctx, path)
		if readErr != nil {
			restoreMCPAgentFacts(&merged, agentID, oldFacts[agentID])
			diagnostics = append(diagnostics, fmt.Sprintf("%s: MCP configuration could not be read", agentID))
			continue
		}
		for id, server := range observed.Servers {
			fact := merged.Servers[id]
			found := -1
			for i, variant := range fact.Variants {
				if mcp.EqualNormalized(variant.Spec, server.Spec) {
					found = i
					break
				}
			}
			if found < 0 {
				fact.Variants = append(fact.Variants, mcp.Variant{Agents: []string{agentID}, Spec: server.Spec})
			} else if !contains(fact.Variants[found].Agents, agentID) {
				fact.Variants[found].Agents = append(fact.Variants[found].Agents, agentID)
			}
			merged.Servers[id] = fact
		}
	}
	eligibleIDs := make([]string, 0, len(eligible))
	for id := range eligible {
		eligibleIDs = append(eligibleIDs, id)
	}
	sort.Strings(eligibleIDs)
	if err := store.Save(ctx, merged); err != nil {
		return MCPScanResult{Servers: summarizeMCP(merged), EligibleAgents: eligibleIDs, Diagnostics: diagnostics}, err
	}
	return MCPScanResult{Servers: summarizeMCP(merged), EligibleAgents: eligibleIDs, Diagnostics: diagnostics}, nil
}

func (u *UseCases) GetMCP(ctx context.Context, id, source string) (MCPServerDetail, error) {
	if err := contextError(ctx, "MCP request was cancelled"); err != nil {
		return MCPServerDetail{}, err
	}
	if err := mcp.ValidateID(id); err != nil {
		return MCPServerDetail{}, err
	}
	r, err := u.mcpStore().Load()
	if err != nil {
		return MCPServerDetail{}, err
	}
	fact, ok := r.Servers[id]
	if !ok {
		return MCPServerDetail{}, errors.New("MCP server not found")
	}
	if source != "" {
		filtered := []mcp.Variant{}
		for _, v := range fact.Variants {
			if contains(v.Agents, source) {
				filtered = append(filtered, v)
			}
		}
		fact.Variants = filtered
	}
	return MCPServerDetail{ID: id, Source: source, Variants: fact.Variants}, nil
}

func (u *UseCases) ApplyMCP(ctx context.Context, req MCPApplyRequest) MCPApplyResult {
	result := MCPApplyResult{Results: []MCPAgentApplyResult{}}
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	store := u.mcpStore()
	registry, loadErr := store.Load()
	if loadErr != nil {
		result.Results = append(result.Results, MCPAgentApplyResult{Error: "Cannot read MCP Registry"})
		return result
	}
	eligible, err := u.eligibleMCPAgents()
	if err != nil {
		result.Results = append(result.Results, MCPAgentApplyResult{Error: err.Error()})
		return result
	}
	for _, change := range req.Changes {
		if err := mcp.ValidateID(change.ID); err != nil {
			result.Results = append(result.Results, MCPAgentApplyResult{Error: err.Error()})
			continue
		}
		if change.Spec != nil {
			if _, err := mcp.Normalize(*change.Spec); err != nil {
				result.Results = append(result.Results, MCPAgentApplyResult{Error: err.Error()})
				continue
			}
		}
		fact := registry.Servers[change.ID]
		selected := make(map[string]bool, len(change.Agents))
		for _, agentID := range change.Agents {
			selected[agentID] = true
		}
		deleteResultIndexes := []int{}
		deleteFailed := false
		for _, agentID := range mcpTargetAgents(fact, change.Agents, eligible) {
			agent, ok := eligible[agentID]
			item := MCPAgentApplyResult{Agent: agentID}
			if !ok {
				item.Error = "Agent is not eligible"
				result.Results = append(result.Results, item)
				continue
			}
			path := mcpPath(u.status.Home, u.status.Platform.OS, agent)
			data, readErr := os.ReadFile(path)
			if os.IsNotExist(readErr) {
				data = []byte("{}")
			} else if readErr != nil {
				item.Error = "Cannot read Agent MCP configuration"
				result.Results = append(result.Results, item)
				continue
			}
			adapter := mcpAdapter(agent)
			spec := change.Spec
			if change.Delete {
				spec = nil
			}
			if !selected[agentID] {
				spec = nil
			}
			out, secret, applyErr := adapter.Apply(ctx, path, data, map[string]*mcp.Spec{change.ID: spec})
			if applyErr == nil {
				_, applyErr = u.filesystem.AtomicWrite(ctx, path, out, secret)
				item.ConfigUpdated = applyErr == nil
			}
			if applyErr != nil {
				deleteFailed = deleteFailed || change.Delete
				item.Error = "Cannot apply MCP configuration"
				result.Results = append(result.Results, item)
				continue
			}
			if change.Delete {
				item.ConfigUpdated = true
				result.Results = append(result.Results, item)
				deleteResultIndexes = append(deleteResultIndexes, len(result.Results)-1)
				continue
			}
			fact = registry.Servers[change.ID]
			if spec != nil {
				collapseEmptyMCPVariants(&fact)
			}
			for i := range fact.Variants {
				kept := fact.Variants[i].Agents[:0]
				for _, existing := range fact.Variants[i].Agents {
					if existing != agentID {
						kept = append(kept, existing)
					}
				}
				fact.Variants[i].Agents = kept
			}
			if spec == nil {
				// Keep the variant as a retained MCP draft when all targets are cleared.
			} else {
				updated := false
				for i := range fact.Variants {
					if mcp.EqualNormalized(fact.Variants[i].Spec, *spec) {
						fact.Variants[i].Agents = append(fact.Variants[i].Agents, agentID)
						updated = true
						break
					}
				}
				if !updated {
					fact.Variants = append(fact.Variants, mcp.Variant{Agents: []string{agentID}, Spec: *spec})
				}
			}
			if len(fact.Variants) == 0 {
				delete(registry.Servers, change.ID)
			} else {
				registry.Servers[change.ID] = fact
			}
			if saveErr := store.Save(ctx, registry); saveErr != nil {
				item.Error = "Agent configuration updated but MCP Registry was not updated"
				result.Results = append(result.Results, item)
				continue
			}
			item.RegistryUpdated = true
			result.Results = append(result.Results, item)
		}
		if change.Delete && !deleteFailed {
			delete(registry.Servers, change.ID)
			if err := store.Save(ctx, registry); err != nil {
				if len(deleteResultIndexes) == 0 {
					result.Results = append(result.Results, MCPAgentApplyResult{Error: "Agent configurations updated but MCP Registry was not updated"})
				} else {
					for _, index := range deleteResultIndexes {
						result.Results[index].Error = "Agent configurations updated but MCP Registry was not updated"
					}
				}
			} else {
				if len(deleteResultIndexes) == 0 {
					result.Results = append(result.Results, MCPAgentApplyResult{RegistryUpdated: true})
				} else {
					for _, index := range deleteResultIndexes {
						result.Results[index].RegistryUpdated = true
					}
				}
			}
		}
	}
	return result
}

func (u *UseCases) ExportMCP(ctx context.Context, options mcp.ExportOptions) ([]byte, error) {
	r, err := u.mcpStore().Load()
	if err != nil {
		return nil, err
	}
	return mcp.Export(r, options)
}
func (u *UseCases) ImportMCP(ctx context.Context, data []byte, password string) (mcp.Registry, error) {
	if err := contextError(ctx, "MCP import was cancelled"); err != nil {
		return mcp.Registry{}, err
	}
	return mcp.Import(data, password)
}

func (u *UseCases) SaveImportedMCP(ctx context.Context, imported mcp.Registry) error {
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	current, err := u.mcpStore().Load()
	if err != nil {
		return err
	}
	for id, fact := range imported.Servers {
		current.Servers[id] = fact
	}
	return u.mcpStore().Save(ctx, current)
}
