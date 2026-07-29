package profile

import (
	"sort"

	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/desktop/internal/provider"
)

// SaveRequest is a profile template the user asked to keep.
type SaveRequest struct {
	ID    string
	Label string
	// Provider is a managed provider id or "custom".
	Provider string
	// APIBaseURL is the custom endpoint, empty for a managed provider. Stored as
	// given rather than resolved, so a managed provider whose endpoint changes
	// upstream is not pinned to the old one.
	APIBaseURL string
	Model      string
	AgentIDs   []string
	// APIKey is written to the secrets file only. It never reaches the stored
	// profile, and the only thing any payload says about it is the hasKey boolean.
	APIKey string
}

// Save stores a profile template and, when a key came with it, the key.
//
// The endpoint is resolved before anything is written so an unusable provider or
// base URL is refused rather than stored: a profile that cannot be activated is
// worse than a rejected request, because it looks saved.
func (s *Store) Save(request SaveRequest) (*jsonorder.Object, error) {
	profileID, err := ValidateID(request.ID)
	if err != nil {
		return nil, err
	}
	baseURL, err := provider.Base(request.Provider, request.APIBaseURL)
	if err != nil {
		return nil, err
	}
	existing, _, err := s.ReadStored(profileID)
	if err != nil {
		return nil, err
	}
	now := s.timestamp()

	stored := jsonorder.NewObject()
	stored.Set("schema_version", storeSchema)
	stored.Set("id", profileID)
	stored.Set("label", firstNonEmpty(request.Label, stringField(existing, "label"), profileID))
	stored.Set("provider", request.Provider)
	stored.Set("base_url", nullWhenEmpty(request.APIBaseURL))
	stored.Set("model", request.Model)
	stored.Set("config_mode", "provider")
	stored.Set("agent_ids", uniqueSorted(request.AgentIDs))
	stored.Set("created_at", firstNonEmpty(stringField(existing, "created_at"), now))
	// Saving a template does not activate it, so the timestamp is carried over
	// rather than set: a saved-but-never-used profile must not claim it was used.
	stored.Set("activated_at", carriedActivation(existing))

	if _, err := s.writeStored(profileID, stored); err != nil {
		return nil, err
	}
	if request.APIKey != "" {
		if err := s.WriteSecret(profileID, request.APIKey, baseURL); err != nil {
			return nil, err
		}
	}
	return stored, nil
}

// ActivateRequest records what an install pointed the machine at.
type ActivateRequest struct {
	AgentIDs []string
	// Configure false means the user is keeping an existing account rather than
	// having OneAgent write a provider, which is a different config_mode and
	// stores no endpoint or model.
	Configure bool
	Provider  string
	BaseURL   string
	Model     string
	APIKey    string
}

// Activate records the active profile and returns the pointer path.
//
// The agent list is merged with the current profile's when the provider and model
// are unchanged: installing one more Agent against the same provider is adding to
// the set, not replacing it. A different provider or model starts a new set,
// because the old Agents are no longer pointed where this profile says.
func (s *Store) Activate(request ActivateRequest) (string, error) {
	current, _, err := s.Load()
	if err != nil {
		return "", err
	}
	profileID := "default"
	if current != nil {
		if id := current.GetString("id"); id != "" {
			profileID = id
		}
	}

	storedProvider := "existing-account"
	var storedBase, storedModel any = nil, nil
	configMode := "existing-account"
	if request.Configure {
		storedProvider = request.Provider
		storedBase = request.BaseURL
		storedModel = request.Model
		configMode = "provider"
	}

	agents := map[string]bool{}
	for _, id := range request.AgentIDs {
		agents[id] = true
	}
	if current != nil &&
		current.GetString("provider") == storedProvider &&
		sameOptional(current, "model", storedModel) {
		for _, id := range agentIDsOf(current) {
			if text, ok := id.(string); ok {
				agents[text] = true
			}
		}
	}
	merged := []string{}
	for id := range agents {
		merged = append(merged, id)
	}
	sort.Strings(merged)

	now := s.timestamp()
	stored := jsonorder.NewObject()
	stored.Set("schema_version", storeSchema)
	stored.Set("id", profileID)
	stored.Set("label", firstNonEmpty(stringField(current, "label"), profileID))
	stored.Set("provider", storedProvider)
	stored.Set("base_url", storedBase)
	stored.Set("model", storedModel)
	stored.Set("config_mode", configMode)
	stored.Set("agent_ids", asAnySlice(merged))
	stored.Set("created_at", firstNonEmpty(stringField(current, "created_at"), now))
	stored.Set("activated_at", now)

	if _, err := s.writeStored(profileID, stored); err != nil {
		return "", err
	}
	if request.Configure {
		if err := s.WriteSecret(profileID, request.APIKey, request.BaseURL); err != nil {
			return "", err
		}
	}
	return s.writePointer(profileID)
}

// PublicSummary is the client-facing shape of a stored profile: camelCase, and no
// key material beyond whether one is held.
func PublicSummary(item *jsonorder.Object) *jsonorder.Object {
	summary := jsonorder.NewObject()
	id := item.GetString("id")
	summary.Set("id", id)
	summary.Set("label", firstNonEmpty(item.GetString("label"), id))
	summary.Set("provider", passThrough(item, "provider"))
	summary.Set("baseUrl", passThrough(item, "base_url"))
	summary.Set("model", passThrough(item, "model"))
	summary.Set("agentIds", agentIDsOf(item))
	summary.Set("activatedAt", passThrough(item, "activated_at"))
	hasKey, _ := item.Get("has_key")
	truth, _ := hasKey.(bool)
	summary.Set("hasKey", truth)
	return summary
}

// nullWhenEmpty keeps the distinction between "no custom endpoint" and "the empty
// endpoint", which the frontend reads as different states.
func nullWhenEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// carriedActivation returns the existing activation timestamp, or null.
func carriedActivation(existing *jsonorder.Object) any {
	if existing == nil {
		return nil
	}
	value, present := existing.Get("activated_at")
	if !present {
		return nil
	}
	return value
}

// sameOptional compares a stored field against a value that may be null.
func sameOptional(object *jsonorder.Object, key string, want any) bool {
	value, _ := object.Get(key)
	if want == nil {
		return value == nil
	}
	text, ok := want.(string)
	if !ok {
		return false
	}
	stored, ok := value.(string)
	return ok && stored == text
}

// uniqueSorted deduplicates and orders the agent list, so the same request always
// stores the same bytes.
func uniqueSorted(values []string) []any {
	seen := map[string]bool{}
	unique := []string{}
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return asAnySlice(unique)
}

func asAnySlice(values []string) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return items
}
