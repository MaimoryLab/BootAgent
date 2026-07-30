package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
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

func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, oneerrors.New(
			oneerrors.InvalidRequest,
			fmt.Sprintf("Cannot load Agent lock manifest: %v", err),
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
	if err := validate(manifest); err != nil {
		return Manifest{}, err
	}
	return cloneManifest(manifest), nil
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
	if pkg.Name == "" || pkg.Version == "" || pkg.Version == "latest" || pkg.License == "" {
		return invalidManifest(agentID, "package metadata is incomplete")
	}
	if !httpsURL(pkg.Source) || !httpsURL(pkg.LicenseURL) {
		return invalidManifest(agentID, "package source and license URL must use HTTPS")
	}
	if pkg.Manager == "npm" && (pkg.Integrity == nil || !strings.HasPrefix(*pkg.Integrity, "sha512-")) {
		return invalidManifest(agentID, "npm package integrity must use sha512")
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
	result.Agents = make(map[string]Agent, len(source.Agents))
	for id, agent := range source.Agents {
		copyAgent := agent
		copyAgent.EnvVars = cloneMap(agent.EnvVars)
		copyAgent.VersionArgs = append([]string(nil), agent.VersionArgs...)
		copyAgent.Platforms = append([]string(nil), agent.Platforms...)
		copyAgent.WindowsPrerequisites = append([]string(nil), agent.WindowsPrerequisites...)
		if agent.Package != nil {
			copyPackage := *agent.Package
			if agent.Package.Integrity != nil {
				integrity := *agent.Package.Integrity
				copyPackage.Integrity = &integrity
			}
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
	for key, value := range source {
		result[key] = value
	}
	return result
}

func AgentIDs(manifest Manifest) []string {
	ids := make([]string, 0, len(manifest.Agents))
	for id := range manifest.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
