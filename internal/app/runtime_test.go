package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/install"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

// archiveDoer serves the locked artifact for one runtime from memory so the
// bootstrap chain is exercised without reaching the network.
type archiveDoer struct {
	bodies map[string][]byte
	hits   int
}

func (d *archiveDoer) Do(request *http.Request) (*http.Response, error) {
	d.hits++
	body, ok := d.bodies[request.URL.String()]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
}

func TestRuntimeStatusesReportLockedVersionsAndRequirements(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("darwin", "arm64"),
		Lookup:   func(command string) (string, bool) { return "", false },
	})
	states, err := core.RuntimeStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("expected node and uv, got %d", len(states))
	}
	byID := map[string]RuntimeStatus{}
	for _, state := range states {
		byID[state.ID] = state
	}
	node := byID["node"]
	if node.Installed || node.Managed || !node.Supported {
		t.Fatalf("node state on a bare machine = %#v", node)
	}
	if node.Command != "npm" || node.LockedVersion == "" {
		t.Fatalf("node contract = %#v", node)
	}
	// The hint is what lets the UI explain why a runtime matters.
	if !strings.Contains(node.RequiredByHint, "Codex") {
		t.Fatalf("node requiredBy = %q", node.RequiredByHint)
	}
	if uv := byID["uv"]; uv.RequiredByHint != "Aider" {
		t.Fatalf("uv requiredBy = %q", uv.RequiredByHint)
	}
}

func TestStatusReportsBootstrappableRuntimeInsteadOfADeadEnd(t *testing.T) {
	home := t.TempDir()
	// Nothing on PATH: every npm and uv Agent is uninstallable, and each one must
	// name the runtime that would fix it.
	core := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("darwin", "arm64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Capabilities.MissingRuntime["codex"] != "node" {
		t.Fatalf("codex missing runtime = %q", status.Capabilities.MissingRuntime["codex"])
	}
	if status.Capabilities.MissingRuntime["aider"] != "uv" {
		t.Fatalf("aider missing runtime = %q", status.Capabilities.MissingRuntime["aider"])
	}
	if status.Capabilities.CanInstall["codex"] {
		t.Fatal("codex should not be installable without npm")
	}
	// A guide-only Agent has no package manager, so it must not be reported as
	// blocked on a runtime.
	if _, present := status.Capabilities.MissingRuntime["cline"]; present {
		t.Fatal("a guide-only Agent should not claim a missing runtime")
	}

	// With npm present the Agent becomes installable and stops asking for Node.
	withNPM := NewUseCases(StatusOptions{
		Home:     home,
		Platform: platform.For("darwin", "arm64"),
		Lookup: func(command string) (string, bool) {
			if command == "npm" {
				return "/usr/local/bin/npm", true
			}
			return "", false
		},
	})
	status, err = withNPM.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Capabilities.CanInstall["codex"] {
		t.Fatal("codex should be installable with npm present")
	}
	if _, present := status.Capabilities.MissingRuntime["codex"]; present {
		t.Fatal("codex should not report a missing runtime when npm is present")
	}
}

func TestManagedRuntimeIsReusedRatherThanReinstalled(t *testing.T) {
	home := t.TempDir()
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Runtimes["node"]
	key := catalog.RuntimeArtifactKey("macos", "arm64")
	artifact := entry.Artifacts[key]

	// Simulate a tree installed by an earlier session, marker included.
	binDir := filepath.Join(home, ".oneagent", "runtimes", "node", "v"+entry.Version, artifact.BinDir)
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, ".oneagent-runtime-ok"), []byte(entry.Version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh\necho 11.0.0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	core := NewUseCases(StatusOptions{
		Home:        home,
		Platform:    platform.For("darwin", "arm64"),
		Environment: map[string]string{"HOME": home, "PATH": filepath.Join(home, "empty")},
	})
	// A downloader that fails on any request proves nothing was fetched.
	core.SetRuntimeDownloader(&archiveDoer{bodies: map[string][]byte{}})
	states, err := core.RuntimeStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.ID != "node" {
			continue
		}
		if !state.Managed || !state.Installed {
			t.Fatalf("an existing managed tree was not detected: %#v", state)
		}
		if state.Version != "11.0.0" {
			t.Fatalf("version probe ran outside the managed PATH: %#v", state)
		}
	}

	// EnsureRuntime must return the existing tree without downloading.
	runtime := install.NewRuntime(home, platform.For("darwin", "arm64"), process.New(map[string]string{"HOME": home, "PATH": filepath.Join(home, "empty")}), map[string]string{"HOME": home})
	updated, installed, err := install.EnsureRuntime(context.Background(), runtime, core.httpDoer, "node", entry)
	if err != nil || installed {
		t.Fatalf("existing tree triggered a download: %v, %v", installed, err)
	}
	if resolved, ok := updated.Runner.LookPath("npm"); !ok || filepath.Dir(resolved) != binDir {
		t.Fatalf("managed npm not resolvable: %q %v", resolved, ok)
	}
}

// A managed runtime is only usable if status resolves commands in the same
// environment installs run in. Resolving against the OneAgent process PATH
// instead would report an Agent CLI under the managed global prefix as missing
// and keep offering to install a runtime that is already there.
func TestStatusResolvesAgentsInTheManagedEnvironment(t *testing.T) {
	home := t.TempDir()
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Runtimes["node"]
	artifact := entry.Artifacts[catalog.RuntimeArtifactKey("macos", "arm64")]

	// A managed node tree plus an Agent CLI in the global prefix, exactly the
	// layout an npm install through OneAgent leaves behind.
	binDir := filepath.Join(home, ".oneagent", "runtimes", "node", "v"+entry.Version, artifact.BinDir)
	globalBin := install.GlobalBinDir(home, "macos")
	for _, directory := range []string{binDir, globalBin} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(binDir, ".oneagent-runtime-ok"), []byte(entry.Version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh\necho 11.0.0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalBin, "codex"), []byte("#!/bin/sh\necho 0.9.9\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	// PATH names a directory that does not exist, so anything reported as found
	// had to come from the managed directories rather than from the host.
	core := NewUseCases(StatusOptions{
		Home:        home,
		Platform:    platform.For("darwin", "arm64"),
		Environment: map[string]string{"HOME": home, "PATH": filepath.Join(home, "empty")},
	})
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	codex := status.Agents["codex"]
	if !codex.Installed {
		t.Fatalf("an Agent in the managed global prefix was reported missing: %#v", codex)
	}
	if codex.Version == nil || *codex.Version != "0.9.9" {
		t.Fatalf("version probe ran outside the managed PATH: %#v", codex.Version)
	}
	if !status.Capabilities.CanInstall["codex"] {
		t.Fatal("npm from the managed tree should make codex installable")
	}
	if _, present := status.Capabilities.MissingRuntime["codex"]; present {
		t.Fatal("a managed node tree should not be reported as a missing runtime")
	}
}
