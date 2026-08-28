package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// RuntimeDownloadTimeout is a backstop for one runtime archive download, not the
// working limit. DownloadStallTimeout is what ends a dead transfer; this only
// has to exceed any legitimate download. At 10 minutes it was cutting off Node's
// ~50 MB archive on genuinely slow links, and as an http.Client.Timeout it
// bounded the whole request including the body, so a transfer that was still
// making progress died anyway.
const RuntimeDownloadTimeout = 60 * time.Minute

// DownloadStallTimeout is how long a download may receive nothing before it is
// abandoned. Shorter than the command stall window because a stalled socket is
// unambiguous: unlike npm, an HTTP body has no reason to go quiet for a minute
// and then recover.
const DownloadStallTimeout = 120 * time.Second

// dialTimeout and responseHeaderTimeout bound the phases that should be fast
// even on a slow link. Only the body transfer is unbounded, and stall detection
// covers that.
const (
	dialTimeout           = 30 * time.Second
	responseHeaderTimeout = 60 * time.Second
)

// defaultDownloadClient deliberately sets no Client.Timeout. That field bounds
// the entire request including reading the body, which is exactly the wall-clock
// limit this change removes; the phase timeouts above plus stall detection
// replace it.
func defaultDownloadClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: dialTimeout}).DialContext
	transport.TLSHandshakeTimeout = dialTimeout
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return &http.Client{Transport: transport}
}

// Doer is the narrow HTTP boundary so runtime downloads are testable without a
// network. It matches internal/provider's client boundary on purpose.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// RuntimeState is the public projection of one managed runtime. It carries no
// filesystem paths beyond the managed root so the UI cannot leak a private
// directory layout it should not depend on.
type RuntimeState struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Command        string `json:"command"`
	Installed      bool   `json:"installed"`
	Version        string `json:"version"`
	LockedVersion  string `json:"lockedVersion"`
	Managed        bool   `json:"managed"`
	Supported      bool   `json:"supported"`
	Note           string `json:"note"`
	License        string `json:"license"`
	LicenseURL     string `json:"licenseUrl"`
	Source         string `json:"source"`
	InstallPath    string `json:"installPath"`
	RequiredByHint string `json:"requiredByHint,omitempty"`
}

// RuntimeRoot is the parent of every managed runtime tree.
func RuntimeRoot(home string) string {
	return filepath.Join(home, ".bootagent", "runtimes")
}

// runtimeDir is the versioned install directory. Keeping the version in the
// path means a lock bump installs alongside the old tree instead of mutating a
// directory a running Agent may be executing from.
func runtimeDir(home, runtimeID, version string) string {
	return filepath.Join(RuntimeRoot(home), runtimeID, "v"+version)
}

// ManagedBinDir returns the executable directory of an installed managed
// runtime, or "" when this runtime is not installed under BootAgent's root.
func ManagedBinDir(home string, runtimeID string, runtime catalog.Runtime, artifact catalog.RuntimeArtifact) string {
	directory := runtimeDir(home, runtimeID, runtime.Version)
	binDir := artifact.BinDir
	if binDir == "" || binDir == "." {
		binDir = ""
	}
	candidate := filepath.Join(directory, binDir)
	if _, err := os.Stat(filepath.Join(candidate, ".bootagent-runtime-ok")); err != nil {
		return ""
	}
	return candidate
}

// RuntimeStates reports every runtime in the lock file for the current
// platform, including whether the command already resolves on this machine.
func RuntimeStates(ctx context.Context, runtime Runtime, manifest catalog.RuntimeManifest) []RuntimeState {
	key := catalog.RuntimeArtifactKey(runtime.Platform.OS, runtime.Platform.Arch)
	states := make([]RuntimeState, 0, len(manifest.Runtimes))
	for _, id := range manifest.RuntimeOrder {
		entry := manifest.Runtimes[id]
		artifact, supported := entry.Artifacts[key]
		state := RuntimeState{
			ID:            id,
			Name:          entry.Name,
			Command:       entry.ProbeCommand,
			LockedVersion: entry.Version,
			Supported:     supported,
			Note:          entry.Note,
			License:       entry.License,
			LicenseURL:    entry.LicenseURL,
			Source:        entry.Source,
		}
		if supported {
			state.InstallPath = runtimeDir(runtime.Home, id, entry.Version)
			state.Managed = ManagedBinDir(runtime.Home, id, entry, artifact) != ""
		}
		if _, ok := lookRuntime(runtime, entry.ProbeCommand); ok {
			state.Installed = true
			versionCommand := entry.VersionCommand
			if versionCommand == "" {
				versionCommand = entry.ProbeCommand
			}
			if versionExecutable, ok := lookRuntime(runtime, versionCommand); ok {
				state.Version = runtimeVersion(ctx, runtime, versionExecutable)
			}
		}
		states = append(states, state)
	}
	return states
}

func lookRuntime(runtime Runtime, command string) (string, bool) {
	if runtime.Runner == nil {
		return "", false
	}
	executable, ok := runtime.Runner.LookPath(command)
	if !ok || executable == "" {
		return "", false
	}
	return executable, true
}

func runtimeVersion(ctx context.Context, runtime Runtime, executable string) string {
	result, err := runtime.command(ctx, []string{executable, "--version"}, nil, VersionCommandTimeout)
	if err != nil {
		return ""
	}
	return VersionFromOutput(result.Stdout + "\n" + result.Stderr)
}

// RuntimeForCommand maps a package manager command to the runtime that
// provides it, so a missing "npm" resolves to Node rather than to a runtime
// named after the command.
func RuntimeForCommand(manifest catalog.RuntimeManifest, command string) (string, catalog.Runtime, bool) {
	for _, id := range manifest.RuntimeOrder {
		entry := manifest.Runtimes[id]
		if slices.Contains(entry.Commands, command) {
			return id, entry, true
		}
	}
	return "", catalog.Runtime{}, false
}

// RuntimeOptions carries per-install choices. It is a struct rather than extra
// parameters so adding a choice does not churn every call site, and so a caller
// reads as PreferMirror rather than as a bare true.
type RuntimeOptions struct {
	// PreferMirror tries the locked mirror before the official source. The
	// checksum gate is identical either way, so this only chooses a host.
	PreferMirror bool
	// StallTimeout overrides DownloadStallTimeout for one request. Zero takes the
	// default and negative disables the check, matching OSRunner.StallTimeout.
	// Tests set it so they need not wait out the real window.
	StallTimeout time.Duration
}

func (o RuntimeOptions) stallTimeout() time.Duration {
	if o.StallTimeout != 0 {
		return o.StallTimeout
	}
	return DownloadStallTimeout
}

// EnsureRuntime installs a locked runtime when its command is not already
// resolvable, and returns the runtime with the executable directory prepended
// to its PATH. An already-satisfied runtime is a no-op: the command resolving
// on the host is enough, whether it came from BootAgent or the user's own
// installation.
func EnsureRuntime(ctx context.Context, runtime Runtime, client Doer, runtimeID string, entry catalog.Runtime, options RuntimeOptions) (Runtime, bool, error) {
	if err := checkContext(ctx); err != nil {
		return runtime, false, err
	}
	key := catalog.RuntimeArtifactKey(runtime.Platform.OS, runtime.Platform.Arch)
	artifact, supported := entry.Artifacts[key]
	if !supported {
		return runtime, false, prerequisiteError(fmt.Sprintf("%s has no locked download for %s", entry.Name, key))
	}
	// A managed tree from an earlier run only needs to be put back on PATH.
	if binDir := ManagedBinDir(runtime.Home, runtimeID, entry, artifact); binDir != "" {
		return withPath(runtime, binDir), false, nil
	}
	if _, ok := lookRuntime(runtime, entry.ProbeCommand); ok {
		return runtime, false, nil
	}
	binDir, err := installRuntime(ctx, runtime, client, runtimeID, entry, artifact, options)
	if err != nil {
		return runtime, false, err
	}
	return withPath(runtime, binDir), true, nil
}

// WithManagedPath returns a copy of the runtime whose environment and command
// lookup both resolve binDir first. Callers that run npm or uv use it so a
// runtime installed by BootAgent is found the same way a system one would be.
func WithManagedPath(runtime Runtime, binDir string) Runtime {
	return withPath(runtime, binDir)
}

// withPath returns a copy of the runtime whose environment resolves the managed
// directory first. Env is cloned because Runtime is passed by value but its map
// is shared.
func withPath(runtime Runtime, binDir string) Runtime {
	if binDir == "" {
		return runtime
	}
	environment := cloneEnv(runtime.Env)
	key := "PATH"
	if runtime.Platform.OS == "windows" {
		for name := range environment {
			if strings.EqualFold(name, "PATH") {
				key = name
				break
			}
		}
	}
	current := environment[key]
	if current == "" || !hasPathEntry(current, binDir) {
		if current == "" {
			environment[key] = binDir
		} else {
			environment[key] = binDir + string(os.PathListSeparator) + current
		}
	}
	runtime.Env = environment
	// Ask the runner to adopt the environment rather than type-asserting to
	// OSRunner: the desktop wraps its runner in a logging decorator, and an
	// assertion would silently skip the injection and report a managed npm as
	// missing.
	if setter, ok := runtime.Runner.(process.EnvSetter); ok {
		runtime.Runner = setter.WithEnvironment(environment)
	}
	return runtime
}

func hasPathEntry(search, directory string) bool {
	return slices.Contains(filepath.SplitList(search), directory)
}

func installRuntime(ctx context.Context, runtime Runtime, client Doer, runtimeID string, entry catalog.Runtime, artifact catalog.RuntimeArtifact, options RuntimeOptions) (string, error) {
	target := runtimeDir(runtime.Home, runtimeID, entry.Version)
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", runtimeError(fmt.Sprintf("Cannot create the runtime directory for %s", entry.Name), err)
	}
	// Runtime bootstrap is an internal download, not a child command. The UI
	// renders its progress events as a card; emitting a made-up command here
	// makes the log look as if a second CLI process was started.
	archive, err := downloadArtifact(ctx, client, entry, artifact, parent, options, runtime.OnOutput, runtimeID)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)
	if err := checkContext(ctx); err != nil {
		return "", err
	}

	// Extract beside the final directory so a partial or corrupt tree is never
	// visible under the versioned path, then swap it in with one rename.
	staging, err := os.MkdirTemp(parent, ".staging-")
	if err != nil {
		return "", runtimeError(fmt.Sprintf("Cannot stage the %s installation", entry.Name), err)
	}
	defer os.RemoveAll(staging)
	if err := extract(archive, staging, artifact); err != nil {
		return "", err
	}
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	root := staging
	if artifact.StripRoot {
		if root, err = singleChild(staging); err != nil {
			return "", runtimeError(fmt.Sprintf("The %s archive layout is unexpected", entry.Name), err)
		}
	}
	binDir := root
	if artifact.BinDir != "" && artifact.BinDir != "." {
		binDir = filepath.Join(root, artifact.BinDir)
	}
	if _, err := os.Stat(binDir); err != nil {
		return "", runtimeError(fmt.Sprintf("The %s archive has no %s directory", entry.Name, artifact.BinDir), err)
	}
	// The marker is written before the rename so it lands atomically with the
	// tree. Its presence is what makes a directory count as a managed install.
	if err := os.WriteFile(filepath.Join(binDir, ".bootagent-runtime-ok"), []byte(entry.Version+"\n"), 0o600); err != nil {
		return "", runtimeError(fmt.Sprintf("Cannot finalize the %s installation", entry.Name), err)
	}
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	if err := os.RemoveAll(target); err != nil {
		return "", runtimeError(fmt.Sprintf("Cannot replace the existing %s directory", entry.Name), err)
	}
	if err := os.Rename(root, target); err != nil {
		return "", runtimeError(fmt.Sprintf("Cannot publish the %s installation", entry.Name), err)
	}
	installed := target
	if artifact.BinDir != "" && artifact.BinDir != "." {
		installed = filepath.Join(target, artifact.BinDir)
	}
	return installed, nil
}

func downloadArtifact(ctx context.Context, client Doer, entry catalog.Runtime, artifact catalog.RuntimeArtifact, directory string, options RuntimeOptions, listener process.OutputListener, target string) (string, error) {
	if client == nil {
		client = defaultDownloadClient()
	}
	downloadCtx, cancel := context.WithTimeout(ctx, RuntimeDownloadTimeout)
	defer cancel()

	var lastErr error
	for _, source := range downloadSources(artifact, options.PreferMirror) {
		path, err := fetchTo(downloadCtx, client, source, artifact.SHA256, directory, listener, target, options.stallTimeout())
		if err == nil {
			return path, nil
		}
		if err := downloadCtx.Err(); err != nil {
			return "", err
		}
		lastErr = err
	}
	return "", runtimeError(fmt.Sprintf("Cannot download %s %s", entry.Name, entry.Version), lastErr)
}

// downloadSources orders the hosts to try. Whichever comes first, the other
// remains a fallback and both must satisfy the same locked checksum, so a
// hostile or stale mirror cannot substitute a different archive — it can only
// fail and hand the download back to the official source.
func downloadSources(artifact catalog.RuntimeArtifact, preferMirror bool) []string {
	if artifact.MirrorURL == "" {
		return []string{artifact.URL}
	}
	if preferMirror {
		return []string{artifact.MirrorURL, artifact.URL}
	}
	return []string{artifact.URL, artifact.MirrorURL}
}

func fetchTo(ctx context.Context, client Doer, source, expected, directory string, listener process.OutputListener, target string, stallTimeout time.Duration) (string, error) {
	if listener != nil {
		listener(process.Output{Kind: "source", Target: target, Source: sourceHost(source)})
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	file, err := os.CreateTemp(directory, ".download-")
	if err != nil {
		return "", err
	}
	path := file.Name()
	digest := sha256.New()
	// A retry against the fallback host starts the bar over rather than
	// resuming: the mirror's byte count says nothing about the official source's.
	_, copyErr := process.CopyWithStallTimeout(ctx, io.MultiWriter(file, digest), response.Body, response.ContentLength, target, listener, stallTimeout)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(path)
		if copyErr != nil {
			return "", copyErr
		}
		return "", closeErr
	}
	if err := ctx.Err(); err != nil {
		os.Remove(path)
		return "", err
	}
	if actual := hex.EncodeToString(digest.Sum(nil)); actual != expected {
		os.Remove(path)
		return "", fmt.Errorf("checksum mismatch: lock expects %s, download reports %s", expected, actual)
	}
	if listener != nil {
		listener(process.Output{Kind: "phase", Target: target, Phase: "verified"})
	}
	return path, nil
}

func sourceHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "unknown source"
	}
	return parsed.Host
}

func extract(archive, destination string, artifact catalog.RuntimeArtifact) error {
	switch artifact.Archive {
	case "tar.gz":
		return extractTarGz(archive, destination)
	case "zip":
		return extractZip(archive, destination)
	default:
		return prerequisiteError("Unsupported runtime archive format: " + artifact.Archive)
	}
}

func singleChild(directory string) (string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	directories := make([]string, 0, 1)
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry.Name())
		}
	}
	if len(directories) != 1 {
		return "", fmt.Errorf("expected exactly one top-level directory, found %d", len(directories))
	}
	return filepath.Join(directory, directories[0]), nil
}

func runtimeError(message string, cause error) error {
	return oneerrors.New(oneerrors.AgentInstallFailed, message, oneerrors.WithRetryable(true), oneerrors.WithCause(cause))
}
