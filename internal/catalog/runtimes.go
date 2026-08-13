package catalog

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	bootagent "github.com/MaimoryLab/BootAgent"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

// RuntimeManifest is the pinned download contract for package managers that
// BootAgent bootstraps itself. Unlike Agent packages resolved by npm or uv, these
// archives need an immutable URL and checksum before BootAgent extracts them.
type RuntimeManifest struct {
	SchemaVersion int                `json:"schema_version"`
	Runtimes      map[string]Runtime `json:"runtimes"`
	RuntimeOrder  []string           `json:"-"`
}

type Runtime struct {
	Name           string                     `json:"name"`
	Version        string                     `json:"version"`
	Commands       []string                   `json:"commands"`
	ProbeCommand   string                     `json:"probe_command"`
	VersionCommand string                     `json:"version_command"`
	License        string                     `json:"license"`
	LicenseURL     string                     `json:"license_url"`
	Source         string                     `json:"source"`
	Note           string                     `json:"note"`
	Artifacts      map[string]RuntimeArtifact `json:"artifacts"`
}

// RuntimeArtifact is one platform's archive. StripRoot reports whether the
// archive wraps its payload in a single top-level directory that must be
// removed; BinDir is the directory inside the installed tree that holds the
// executables, relative to the stripped root.
type RuntimeArtifact struct {
	URL       string `json:"url"`
	MirrorURL string `json:"mirror_url"`
	SHA256    string `json:"sha256"`
	Archive   string `json:"archive"`
	StripRoot bool   `json:"strip_root"`
	BinDir    string `json:"bin_dir"`
}

var (
	runtimeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	sha256Pattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)

	runtimeOnce     sync.Once
	runtimeManifest RuntimeManifest
	runtimeErr      error
)

// RuntimeArtifactKey is the platform key used in runtimes.lock.json. It is
// built from the same normalized values platform.Info reports so the lock file
// and the running host cannot disagree about naming.
func RuntimeArtifactKey(osID, arch string) string {
	if arch == "" {
		arch = "x64"
	}
	return osID + "-" + arch
}

// LoadEmbeddedRuntimes parses the embedded runtime lock once and returns a
// defensive copy. A malformed lock file is a build defect, so the error is
// returned rather than silently degraded.
func LoadEmbeddedRuntimes() (RuntimeManifest, error) {
	runtimeOnce.Do(func() {
		data, err := bootagent.EmbeddedRuntimeLock()
		if err != nil {
			runtimeErr = oneerrors.New(oneerrors.InvalidRequest, "Cannot load embedded runtime lock manifest", oneerrors.WithCause(err))
			return
		}
		runtimeManifest, runtimeErr = ParseRuntimes(data)
	})
	if runtimeErr != nil {
		return RuntimeManifest{}, runtimeErr
	}
	return cloneRuntimeManifest(runtimeManifest), nil
}

func ParseRuntimes(data []byte) (RuntimeManifest, error) {
	var manifest RuntimeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return RuntimeManifest{}, invalidRuntime(fmt.Sprintf("Cannot load runtime lock manifest: %v", err))
	}
	if manifest.SchemaVersion != SchemaVersion {
		return RuntimeManifest{}, invalidRuntime(fmt.Sprintf("Unsupported runtime lock schema_version %d", manifest.SchemaVersion))
	}
	if len(manifest.Runtimes) == 0 {
		return RuntimeManifest{}, invalidRuntime("Runtime lock manifest contains no runtimes")
	}
	for id, runtime := range manifest.Runtimes {
		if err := validateRuntime(id, runtime); err != nil {
			return RuntimeManifest{}, err
		}
	}
	manifest.RuntimeOrder = sortedKeys(manifest.Runtimes)
	return cloneRuntimeManifest(manifest), nil
}

func validateRuntime(id string, runtime Runtime) error {
	if !runtimeIDPattern.MatchString(id) {
		return invalidRuntime(fmt.Sprintf("Runtime id %q must be lowercase alphanumeric", id))
	}
	if strings.TrimSpace(runtime.Name) == "" || strings.TrimSpace(runtime.Version) == "" {
		return invalidRuntime(fmt.Sprintf("Runtime %s requires name and version", id))
	}
	if len(runtime.Commands) == 0 {
		return invalidRuntime(fmt.Sprintf("Runtime %s requires at least one command", id))
	}
	if runtime.ProbeCommand == "" {
		return invalidRuntime(fmt.Sprintf("Runtime %s requires probe_command", id))
	}
	if len(runtime.Artifacts) == 0 {
		return invalidRuntime(fmt.Sprintf("Runtime %s requires at least one artifact", id))
	}
	for key, artifact := range runtime.Artifacts {
		if err := validateArtifact(id, key, artifact); err != nil {
			return err
		}
	}
	return nil
}

func validateArtifact(runtimeID, key string, artifact RuntimeArtifact) error {
	for label, value := range map[string]string{"url": artifact.URL, "mirror_url": artifact.MirrorURL} {
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return invalidRuntime(fmt.Sprintf("Runtime %s artifact %s %s must be an https URL", runtimeID, key, label))
		}
	}
	if artifact.URL == "" {
		return invalidRuntime(fmt.Sprintf("Runtime %s artifact %s requires url", runtimeID, key))
	}
	if !sha256Pattern.MatchString(artifact.SHA256) {
		return invalidRuntime(fmt.Sprintf("Runtime %s artifact %s requires a lowercase hex sha256", runtimeID, key))
	}
	if artifact.Archive != "tar.gz" && artifact.Archive != "zip" {
		return invalidRuntime(fmt.Sprintf("Runtime %s artifact %s has unsupported archive %q", runtimeID, key, artifact.Archive))
	}
	return nil
}

func invalidRuntime(message string) error {
	return oneerrors.New(oneerrors.InvalidRequest, message)
}

func sortedKeys[V any](source map[string]V) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneRuntimeManifest(source RuntimeManifest) RuntimeManifest {
	result := RuntimeManifest{
		SchemaVersion: source.SchemaVersion,
		Runtimes:      make(map[string]Runtime, len(source.Runtimes)),
		RuntimeOrder:  append([]string(nil), source.RuntimeOrder...),
	}
	for id, runtime := range source.Runtimes {
		copied := runtime
		copied.Commands = append([]string(nil), runtime.Commands...)
		copied.Artifacts = make(map[string]RuntimeArtifact, len(runtime.Artifacts))
		maps.Copy(copied.Artifacts, runtime.Artifacts)
		result.Runtimes[id] = copied
	}
	return result
}
