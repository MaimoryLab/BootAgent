package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

type fakeDownloader struct {
	bodies map[string][]byte
	hits   []string
}

func (d *fakeDownloader) Do(request *http.Request) (*http.Response, error) {
	url := request.URL.String()
	d.hits = append(d.hits, url)
	body, ok := d.bodies[url]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
}

// tarball builds a node-shaped archive: a single root directory, a bin/ payload
// and the relative bin/npm symlink node actually ships.
func tarball(t *testing.T, root string) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	compressor := gzip.NewWriter(buffer)
	writer := tar.NewWriter(compressor)
	write := func(header *tar.Header, content string) {
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if content != "" {
			if _, err := writer.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(&tar.Header{Name: root + "/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	write(&tar.Header{Name: root + "/bin/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	// node ships npm-cli.js executable; bin/npm is a symlink to it, so the mode
	// on the target is what makes the resolved npm runnable.
	write(&tar.Header{Name: root + "/lib/npm-cli.js", Typeflag: tar.TypeReg, Mode: 0o755, Size: 3}, "cli")
	write(&tar.Header{Name: root + "/bin/node", Typeflag: tar.TypeReg, Mode: 0o755, Size: 4}, "node")
	write(&tar.Header{Name: root + "/bin/npm", Typeflag: tar.TypeSymlink, Linkname: "../lib/npm-cli.js", Mode: 0o777}, "")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// zipball builds a uv-shaped archive: flat executables, no root directory.
func zipball(t *testing.T, names ...string) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o755)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("binary")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func bootstrapRuntime(t *testing.T, home, osID string) Runtime {
	t.Helper()
	runner := process.New(map[string]string{"HOME": home, "PATH": filepath.Join(home, "empty")})
	return NewRuntime(home, platform.For(osID, "arm64"), runner, runner.Env)
}

func TestEnsureRuntimeInstallsVerifiesAndExposesOnPath(t *testing.T) {
	home := t.TempDir()
	archive := tarball(t, "node-v1.2.3-darwin-arm64")
	downloader := &fakeDownloader{bodies: map[string][]byte{"https://example.test/node.tar.gz": archive}}
	entry := catalog.Runtime{
		Name: "Node.js", Version: "1.2.3", Commands: []string{"node", "npm"}, ProbeCommand: "npm",
		Artifacts: map[string]catalog.RuntimeArtifact{
			"macos-arm64": {URL: "https://example.test/node.tar.gz", SHA256: digestOf(archive), Archive: "tar.gz", StripRoot: true, BinDir: "bin"},
		},
	}
	runtime := bootstrapRuntime(t, home, "darwin")

	updated, installed, err := EnsureRuntime(context.Background(), runtime, downloader, "node", entry)
	if err != nil || !installed {
		t.Fatalf("install = %v, %v", installed, err)
	}
	binDir := filepath.Join(RuntimeRoot(home), "node", "v1.2.3", "bin")
	if _, err := os.Stat(filepath.Join(binDir, "node")); err != nil {
		t.Fatalf("node binary missing: %v", err)
	}
	// node's bin/npm is a symlink into lib/. Losing it would leave the runtime
	// installed but with no package manager.
	target, err := os.Readlink(filepath.Join(binDir, "npm"))
	if err != nil || target != filepath.FromSlash("../lib/npm-cli.js") {
		t.Fatalf("npm symlink = %q, %v", target, err)
	}
	if !hasPathEntry(updated.Env["PATH"], binDir) {
		t.Fatalf("managed bin dir is not on PATH: %q", updated.Env["PATH"])
	}
	// The runner must resolve through the updated PATH, otherwise the installer
	// reports npm as missing right after installing it.
	if resolved, ok := updated.Runner.LookPath("npm"); !ok || filepath.Dir(resolved) != binDir {
		t.Fatalf("npm lookup = %q, %v", resolved, ok)
	}

	// A second call is a no-op that only re-exposes the existing tree.
	before := len(downloader.hits)
	again, installedAgain, err := EnsureRuntime(context.Background(), bootstrapRuntime(t, home, "darwin"), downloader, "node", entry)
	if err != nil || installedAgain {
		t.Fatalf("second install = %v, %v", installedAgain, err)
	}
	if len(downloader.hits) != before {
		t.Fatalf("second call downloaded again: %v", downloader.hits)
	}
	if !hasPathEntry(again.Env["PATH"], binDir) {
		t.Fatalf("second call lost the managed PATH entry: %q", again.Env["PATH"])
	}
}

func TestEnsureRuntimeRejectsChecksumMismatchAndKeepsNothing(t *testing.T) {
	home := t.TempDir()
	archive := tarball(t, "node-v1.2.3-darwin-arm64")
	downloader := &fakeDownloader{bodies: map[string][]byte{"https://example.test/node.tar.gz": archive}}
	entry := catalog.Runtime{
		Name: "Node.js", Version: "1.2.3", Commands: []string{"npm"}, ProbeCommand: "npm",
		Artifacts: map[string]catalog.RuntimeArtifact{
			"macos-arm64": {URL: "https://example.test/node.tar.gz", SHA256: strings.Repeat("a", 64), Archive: "tar.gz", StripRoot: true, BinDir: "bin"},
		},
	}
	_, _, err := EnsureRuntime(context.Background(), bootstrapRuntime(t, home, "darwin"), downloader, "node", entry)
	if err == nil || oneerrors.As(err).Code != oneerrors.AgentInstallFailed {
		t.Fatalf("checksum mismatch error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(RuntimeRoot(home), "node", "v1.2.3")); !os.IsNotExist(statErr) {
		t.Fatalf("a rejected download left a runtime directory behind")
	}
}

func TestEnsureRuntimeSupportsFlatZipAndSkipsExistingCommand(t *testing.T) {
	home := t.TempDir()
	archive := zipball(t, "uv.exe", "uvx.exe")
	downloader := &fakeDownloader{bodies: map[string][]byte{"https://example.test/uv.zip": archive}}
	entry := catalog.Runtime{
		Name: "uv", Version: "0.1.0", Commands: []string{"uv"}, ProbeCommand: "uv",
		Artifacts: map[string]catalog.RuntimeArtifact{
			"windows-arm64": {URL: "https://example.test/uv.zip", SHA256: digestOf(archive), Archive: "zip", StripRoot: false, BinDir: "."},
		},
	}
	updated, installed, err := EnsureRuntime(context.Background(), bootstrapRuntime(t, home, "windows"), downloader, "uv", entry)
	if err != nil || !installed {
		t.Fatalf("zip install = %v, %v", installed, err)
	}
	if _, statErr := os.Stat(filepath.Join(RuntimeRoot(home), "uv", "v0.1.0", "uv.exe")); statErr != nil {
		t.Fatalf("uv.exe missing: %v", statErr)
	}
	if !hasPathEntry(updated.Env["PATH"], filepath.Join(RuntimeRoot(home), "uv", "v0.1.0")) {
		t.Fatalf("uv directory is not on PATH: %q", updated.Env["PATH"])
	}

	// A host that already provides the command is left untouched: OneAgent does
	// not replace a user's own installation.
	existing := Runtime{Home: t.TempDir(), Platform: platform.For("windows", "arm64"), Env: map[string]string{}, Runner: &fakeInstallRunner{paths: map[string]string{"uv": "C:\\tools\\uv.exe"}}}
	_, installedAgain, err := EnsureRuntime(context.Background(), existing, downloader, "uv", entry)
	if err != nil || installedAgain {
		t.Fatalf("existing uv should not be reinstalled: %v, %v", installedAgain, err)
	}
}

func TestExtractRefusesEscapingPaths(t *testing.T) {
	destination := t.TempDir()
	buffer := &bytes.Buffer{}
	compressor := gzip.NewWriter(buffer)
	writer := tar.NewWriter(compressor)
	if err := writer.WriteHeader(&tar.Header{Name: "../escaped", Typeflag: tar.TypeReg, Mode: 0o644, Size: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	compressor.Close()
	archive := filepath.Join(t.TempDir(), "evil.tar.gz")
	if err := os.WriteFile(archive, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGz(archive, destination); err == nil {
		t.Fatal("a traversal entry was accepted")
	}

	// A symlink whose target escapes must be refused even though its own name
	// resolves inside the destination.
	if err := writeSymlink(destination, filepath.Join(destination, "link"), "../../etc/passwd"); err == nil {
		t.Fatal("an escaping symlink was accepted")
	}
}

func TestPersistRuntimePathIsIdempotentAndGuardsDuplicates(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(profile, []byte("export EDITOR=vi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := bootstrapRuntime(t, home, "darwin")
	filesystem := securefs.New(securefs.Options{OS: "macos"})
	binDir := filepath.Join(RuntimeRoot(home), "node", "v1.2.3", "bin")

	changed, err := PersistRuntimePath(context.Background(), runtime, filesystem, []string{binDir})
	if err != nil || !changed {
		t.Fatalf("first persist = %v, %v", changed, err)
	}
	first, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "export EDITOR=vi") {
		t.Fatalf("persist dropped existing profile content: %q", first)
	}
	script, err := os.ReadFile(filepath.Join(RuntimeRoot(home), envScriptName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), binDir) {
		t.Fatalf("script does not export the managed directory: %q", script)
	}
	// The generated script must not grow PATH when it is sourced twice.
	if !strings.Contains(string(script), `case ":$PATH:" in`) {
		t.Fatalf("script has no duplicate guard: %q", script)
	}

	changed, err = PersistRuntimePath(context.Background(), runtime, filesystem, []string{binDir})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("a second persist rewrote an unchanged profile")
	}
	second, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(second), pathMarkerBegin) != 1 {
		t.Fatalf("profile has duplicate OneAgent blocks: %q", second)
	}

	// A version bump replaces the block instead of appending a second one.
	next := filepath.Join(RuntimeRoot(home), "node", "v2.0.0", "bin")
	if _, err := PersistRuntimePath(context.Background(), runtime, filesystem, []string{next}); err != nil {
		t.Fatal(err)
	}
	third, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(third), pathMarkerBegin) != 1 {
		t.Fatalf("version bump duplicated the block: %q", third)
	}
	updatedScript, err := os.ReadFile(filepath.Join(RuntimeRoot(home), envScriptName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updatedScript), binDir) {
		t.Fatalf("script still references the superseded directory: %q", updatedScript)
	}
}

func TestRuntimeLockCoversEveryDesktopPlatform(t *testing.T) {
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"node", "uv"} {
		entry, present := manifest.Runtimes[id]
		if !present {
			t.Fatalf("runtime lock is missing %s", id)
		}
		for _, osID := range []string{"macos", "windows", "linux"} {
			for _, arch := range []string{"x64", "arm64"} {
				key := catalog.RuntimeArtifactKey(osID, arch)
				if _, ok := entry.Artifacts[key]; !ok {
					t.Fatalf("%s has no locked artifact for %s", id, key)
				}
			}
		}
	}
}

func TestRuntimeForCommandMapsPackageManagers(t *testing.T) {
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		t.Fatal(err)
	}
	// npm must resolve to node, not to a runtime named after the command.
	if id, _, ok := RuntimeForCommand(manifest, "npm"); !ok || id != "node" {
		t.Fatalf("npm maps to %q, %v", id, ok)
	}
	if id, _, ok := RuntimeForCommand(manifest, "uv"); !ok || id != "uv" {
		t.Fatalf("uv maps to %q, %v", id, ok)
	}
	if _, _, ok := RuntimeForCommand(manifest, "pip"); ok {
		t.Fatal("pip should not resolve to a bootstrappable runtime")
	}
}
