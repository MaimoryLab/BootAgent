package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/securefs"
)

// bindingSchema is the only Agent binding version this build reads. A newer one
// is refused rather than partially understood.
const bindingSchema = 1

// Store reads and writes both the profile store and the Agent bindings.
type Store struct {
	Runtime *runtime.Runtime
	FS      *securefs.FS
	// Now supplies the timestamps, injected so a test can assert what was
	// written rather than only that something was.
	Now func() time.Time
}

// NewStore builds a Store over a runtime.
func NewStore(rt *runtime.Runtime) *Store {
	return &Store{Runtime: rt, FS: securefs.New(rt), Now: time.Now}
}

// timestamp reproduces datetime.now(timezone.utc).isoformat() with +00:00
// replaced by Z, which is what the stored files already contain.
//
// The microsecond field is not simply formatted with .000000: Python omits the
// fractional part entirely when it is zero, so a timestamp landing exactly on a
// second would differ in shape. Rare, but it is a stored field, and "rare" is how
// the other divergences in this migration presented.
func (s *Store) timestamp() string {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	value := now().UTC()
	if value.Nanosecond() == 0 {
		return value.Format("2006-01-02T15:04:05Z")
	}
	return value.Format("2006-01-02T15:04:05.000000Z")
}

// BindingPath is one Agent's binding file.
func BindingPath(rt *runtime.Runtime, agentID string) (string, error) {
	name, err := config.ValidateAgentID(agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(AgentsDir(rt), name+".json"), nil
}

// ReadBinding reports what an Agent is currently pointed at, or why that cannot
// be read. A missing file is neither: it means nothing has configured this Agent.
func (s *Store) ReadBinding(agentID string) (*jsonorder.Object, string, error) {
	path, err := BindingPath(s.Runtime, agentID)
	if err != nil {
		return nil, "", err
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, "", nil
		}
		return nil, readErr.Error(), nil
	}
	value, parseErr := jsonorder.Parse(raw)
	if parseErr != nil {
		return nil, parseErr.Error(), nil
	}
	if schema, _ := value.Get("schema_version"); !isSchema(schema, bindingSchema) {
		return nil, "Unsupported Agent binding schema for " + agentID, nil
	}
	return value, "", nil
}

// WriteBinding records an Agent's own provider and model. Never its key.
//
// Bindings answer "what is this Agent pointed at" without reading the Agent's own
// config file, whose shape differs per adapter. The credential stays in the
// sibling env file so this file stays safe to read and report.
func (s *Store) WriteBinding(agentID, provider, baseURL, model, profileRef string) (*jsonorder.Object, error) {
	name, err := config.ValidateAgentID(agentID)
	if err != nil {
		return nil, err
	}
	path, err := BindingPath(s.Runtime, agentID)
	if err != nil {
		return nil, err
	}
	existing, _, err := s.ReadBinding(agentID)
	if err != nil {
		return nil, err
	}
	now := s.timestamp()

	binding := jsonorder.NewObject()
	binding.Set("schema_version", bindingSchema)
	binding.Set("agent_id", name)
	binding.Set("provider", provider)
	binding.Set("base_url", baseURL)
	binding.Set("model", model)
	// An explicit ref wins, otherwise the existing one is kept: re-pointing an
	// Agent must not silently detach it from the profile it came from.
	binding.Set("profile_ref", firstNonEmpty(profileRef, stringField(existing, "profile_ref")))
	binding.Set("created_at", firstNonEmpty(stringField(existing, "created_at"), now))
	binding.Set("updated_at", now)

	if err := s.FS.EnsureDir(AgentsDir(s.Runtime)); err != nil {
		return nil, err
	}
	encoded, err := jsonorder.Marshal(binding)
	if err != nil {
		return nil, oerr.Newf("INTERNAL_ERROR", "Cannot encode the Agent binding: %v", err)
	}
	if _, err := s.FS.Write(path, string(encoded), false); err != nil {
		return nil, err
	}
	return binding, nil
}

// ListBindings returns every readable binding, keyed by Agent id.
//
// An unreadable or unknown-schema file is skipped rather than failing the call:
// one corrupt binding must not blank the whole overview, which is the same
// guarantee the config readers give.
func (s *Store) ListBindings() (map[string]*jsonorder.Object, error) {
	bindings := map[string]*jsonorder.Object{}
	entries, err := os.ReadDir(AgentsDir(s.Runtime))
	if err != nil {
		return bindings, nil
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		agentID := strings.TrimSuffix(name, ".json")
		if _, err := config.ValidateAgentID(agentID); err != nil {
			continue
		}
		value, reason, err := s.ReadBinding(agentID)
		if err != nil || reason != "" || value == nil {
			continue
		}
		bindings[agentID] = value
	}
	return bindings, nil
}

// isSchema compares a decoded schema_version against a wanted value. jsonorder
// decodes with UseNumber, so the value is a json.Number and a plain == against an
// int never matches. Compared as an integer rather than as text, so a file
// carrying 1.0 or 01 is not read as a different version than 1.
func isSchema(value any, want int) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	parsed, err := number.Int64()
	return err == nil && parsed == int64(want)
}

// stringField reads a string from an object that may be nil.
func stringField(object *jsonorder.Object, key string) string {
	if object == nil {
		return ""
	}
	return object.GetString(key)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
