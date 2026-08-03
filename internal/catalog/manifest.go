package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"sort"
	"strings"

	oneagent "github.com/MaimoryLab/OneAgent"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

const SchemaVersion = 1

var agentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func LoadEmbedded() (Manifest, error) {
	data, err := oneagent.EmbeddedAgentLock()
	if err != nil {
		return Manifest{}, oneerrors.New(
			oneerrors.InvalidRequest,
			"Cannot load embedded Agent lock manifest",
			oneerrors.WithCause(err),
		)
	}
	return Parse(data)
}

func Parse(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, oneerrors.New(
			oneerrors.InvalidRequest,
			fmt.Sprintf("Cannot load Agent lock manifest: %v", err),
			oneerrors.WithCause(err),
		)
	}
	order, err := decodeAgentOrder(data)
	if err != nil {
		return Manifest{}, oneerrors.New(
			oneerrors.InvalidRequest,
			fmt.Sprintf("Cannot load Agent lock manifest: %v", err),
			oneerrors.WithCause(err),
		)
	}
	manifest.AgentOrder = order
	if err := validate(manifest); err != nil {
		return Manifest{}, err
	}
	return cloneManifest(manifest), nil
}

func decodeAgentOrder(data []byte) ([]string, error) {
	var document struct {
		Agents json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Agents) == 0 || bytes.Equal(bytes.TrimSpace(document.Agents), []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(document.Agents))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("agents must be an object")
	}
	order := make([]string, 0)
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		id, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("Agent ID must be a string")
		}
		if !seen[id] {
			order = append(order, id)
			seen[id] = true
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return order, nil
}

func validate(manifest Manifest) error {
	if manifest.SchemaVersion != SchemaVersion || manifest.Agents == nil {
		return oneerrors.New(oneerrors.InvalidRequest, "Unsupported Agent lock manifest schema")
	}
	if len(manifest.Agents) == 0 {
		return oneerrors.New(oneerrors.InvalidRequest, "Agent lock manifest has no Agents")
	}
	for id, agent := range manifest.Agents {
		if !agentIDPattern.MatchString(id) {
			return invalidManifest(id, "invalid Agent ID")
		}
		if strings.TrimSpace(agent.Name) == "" {
			return invalidManifest(id, "name is required")
		}
		if agent.ConfigMode != "auto" && agent.ConfigMode != "guide" {
			return invalidManifest(id, "config_mode must be auto or guide")
		}
		if agent.Rank <= 0 {
			return invalidManifest(id, "rank must be positive")
		}
		if len(agent.Platforms) == 0 {
			return invalidManifest(id, "platforms must not be empty")
		}
		for _, platformID := range agent.Platforms {
			if platformID != "macos" && platformID != "linux" && platformID != "windows" {
				return invalidManifest(id, "platforms contains an unsupported value")
			}
		}
		if agent.ConfigMode == "guide" {
			if agent.Package != nil || agent.ConfigAdapter != "" || strings.TrimSpace(agent.Guide) == "" {
				return invalidManifest(id, "guide Agent has an installation contract")
			}
			continue
		}
		if agent.Command == "" || agent.ConfigPath == "" || agent.ConfigAdapter == "" || agent.Package == nil {
			return invalidManifest(id, "auto Agent is missing an installation field")
		}
		if err := validatePackage(id, *agent.Package); err != nil {
			return err
		}
	}
	return nil
}

func validatePackage(agentID string, pkg Package) error {
	if pkg.Manager != "npm" && pkg.Manager != "uv" {
		return invalidManifest(agentID, "package manager is not allowlisted")
	}
	if pkg.Name == "" || pkg.License == "" {
		return invalidManifest(agentID, "package metadata is incomplete")
	}
	if !httpsURL(pkg.Source) || !httpsURL(pkg.LicenseURL) {
		return invalidManifest(agentID, "package source and license URL must use HTTPS")
	}
	return nil
}

func httpsURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func invalidManifest(agentID, message string) error {
	return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Invalid Agent lock manifest entry %s: %s", agentID, message))
}

func cloneManifest(source Manifest) Manifest {
	result := source
	result.AgentOrder = append([]string(nil), source.AgentOrder...)
	result.Agents = make(map[string]Agent, len(source.Agents))
	for id, agent := range source.Agents {
		copyAgent := agent
		copyAgent.EnvVars = cloneMap(agent.EnvVars)
		copyAgent.VersionArgs = append([]string(nil), agent.VersionArgs...)
		copyAgent.Platforms = append([]string(nil), agent.Platforms...)
		copyAgent.WindowsPrerequisites = append([]string(nil), agent.WindowsPrerequisites...)
		if agent.Package != nil {
			copyPackage := *agent.Package
			copyAgent.Package = &copyPackage
		}
		result.Agents[id] = copyAgent
	}
	return result
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	maps.Copy(result, source)
	return result
}

func AgentIDs(manifest Manifest) []string {
	ids := make([]string, 0, len(manifest.Agents))
	seen := make(map[string]bool, len(manifest.Agents))
	for _, id := range manifest.AgentOrder {
		if _, ok := manifest.Agents[id]; ok && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	missing := make([]string, 0, len(manifest.Agents)-len(ids))
	for id := range manifest.Agents {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	ids = append(ids, missing...)
	return ids
}
