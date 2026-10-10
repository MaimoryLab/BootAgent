package install

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
)

// A managed npm's packages land under BootAgent's own prefix, and the cache is
// npm's default for the user. Both have to be watched: either one going quiet
// while the other grows is still a download in progress.
func TestNPMActivityPathsCoverTheManagedPrefixAndCache(t *testing.T) {
	home := t.TempDir()
	runtime := Runtime{Home: home, Platform: platform.For("darwin", "arm64"), Env: map[string]string{}}
	npm := filepath.Join(RuntimeRoot(home), "node", "bin", "npm")
	paths := NPMActivityPaths(runtime, npm, "@anthropic-ai/claude-code", NPMEnvironment(runtime, npm, ""))
	for _, want := range []string{
		filepath.Join(GlobalPrefix(home), "lib", "node_modules", "@anthropic-ai", "claude-code"),
		filepath.Join(home, ".npm", "_cacache", "tmp"),
	} {
		if !slices.Contains(paths, want) {
			t.Errorf("paths = %v, want %s among them", paths, want)
		}
	}
}

// On Windows the global node_modules sits directly under the prefix, the
// default prefix is %APPDATA%\npm rather than where npm.cmd lives, and the
// cache is under %LOCALAPPDATA%. An explicit npm_config_cache wins over the
// default, matched without regard to case as Windows environments are.
//
// Built from this host's absolute paths so it runs everywhere; what it pins is
// the Windows layout, which is keyed off Platform rather than the host.
func TestNPMActivityPathsFollowWindowsLayoutAndOverrides(t *testing.T) {
	base := t.TempDir()
	roaming := filepath.Join(base, "Roaming")
	local := filepath.Join(base, "Local")
	nodejs := filepath.Join(base, "nodejs")
	runtime := Runtime{
		Home:     base,
		Platform: platform.For("windows", "amd64"),
		Env:      map[string]string{"APPDATA": roaming, "LOCALAPPDATA": local},
	}
	npm := filepath.Join(nodejs, "npm.cmd")
	paths := NPMActivityPaths(runtime, npm, "@anthropic-ai/claude-code", nil)
	for _, want := range []string{
		filepath.Join(roaming, "npm", "node_modules", "@anthropic-ai", "claude-code"),
		filepath.Join(nodejs, "node_modules", "@anthropic-ai", "claude-code"),
		filepath.Join(local, "npm-cache", "_cacache", "tmp"),
	} {
		if !slices.Contains(paths, want) {
			t.Errorf("paths = %v, want %s among them", paths, want)
		}
	}
	if slices.Contains(paths, filepath.Join(nodejs, "lib", "node_modules", "@anthropic-ai", "claude-code")) {
		t.Errorf("paths = %v: Windows has no lib/ under the prefix", paths)
	}

	configured := filepath.Join(base, "custom-cache")
	runtime.Env["NPM_CONFIG_CACHE"] = configured
	paths = NPMActivityPaths(runtime, npm, "@anthropic-ai/claude-code", nil)
	if !slices.Contains(paths, filepath.Join(configured, "_cacache", "tmp")) {
		t.Errorf("paths = %v, want the configured cache", paths)
	}
	if slices.Contains(paths, filepath.Join(local, "npm-cache", "_cacache", "tmp")) {
		t.Errorf("paths = %v: the default cache is not where an overridden npm writes", paths)
	}
}

// A prefix or cache set in an npmrc rather than the environment is where npm
// actually writes, so it has to be watched; missing it would let a stall stop a
// download that was progressing. Parsed as npm parses it: ${VAR}, a leading ~,
// quotes, and both comment styles.
func TestNPMActivityPathsReadThePrefixAndCacheFromNPMRC(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "elsewhere")
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte(strings.Join([]string{
		"; npm's own comment style",
		"# and the other one",
		"registry=https://registry.example/",
		"prefix = ${CUSTOM_ROOT}/npm-global",
		`cache="~/my-cache"`,
	}, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := Runtime{Home: home, Platform: platform.For("linux", "amd64"), Env: map[string]string{"CUSTOM_ROOT": custom}}
	paths := NPMActivityPaths(runtime, "", "lodash", nil)
	for _, want := range []string{
		filepath.Join(custom, "npm-global", "lib", "node_modules", "lodash"),
		filepath.Join(home, "my-cache", "_cacache", "tmp"),
	} {
		if !slices.Contains(paths, want) {
			t.Errorf("paths = %v, want %s among them", paths, want)
		}
	}
	if slices.Contains(paths, filepath.Join(home, ".npm", "_cacache", "tmp")) {
		t.Errorf("paths = %v: the default cache is not where an npmrc-configured npm writes", paths)
	}
}

// The environment outranks the npmrc for the cache, as it does in npm.
func TestNPMActivityPathsLetTheEnvironmentOutrankTheNPMRCCache(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte("cache=/from/npmrc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(home, "from-env")
	runtime := Runtime{Home: home, Platform: platform.For("linux", "amd64"), Env: map[string]string{"npm_config_cache": env}}
	paths := NPMActivityPaths(runtime, "", "lodash", nil)
	if !slices.Contains(paths, filepath.Join(env, "_cacache", "tmp")) || slices.Contains(paths, filepath.Join("/from/npmrc", "_cacache", "tmp")) {
		t.Errorf("paths = %v, want the environment's cache only", paths)
	}
}

// Homebrew links bin/npm to npm's own script in lib/node_modules/npm/bin. The
// link is followed, and that directory -- also named bin -- must not be taken
// for the prefix's bin, or the watched package directory would point inside
// npm's own package.
func TestNPMActivityPathsFollowASymlinkedNPMToItsInstallation(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "lib", "node_modules", "npm", "bin", "npm-cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "bin", "npm")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(script, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	runtime := Runtime{Home: t.TempDir(), Platform: platform.For("darwin", "arm64"), Env: map[string]string{}}
	paths := NPMActivityPaths(runtime, link, "lodash", nil)
	if want := filepath.Join(resolvedRoot, "lib", "node_modules", "lodash"); !slices.Contains(paths, want) {
		t.Errorf("paths = %v, want %s", paths, want)
	}
	for _, path := range paths {
		if strings.Contains(path, filepath.Join("node_modules", "npm", "lib")) {
			t.Errorf("paths = %v: %s points inside npm's own package", paths, path)
		}
	}
}

// npm found on PATH as a bare name, or a prefix variable that was never
// expanded, would otherwise be walked relative to the working directory.
func TestNPMActivityPathsDropRelativeCandidates(t *testing.T) {
	runtime := Runtime{Home: "", Platform: platform.For("linux", "amd64"), Env: map[string]string{"npm_config_prefix": "relative/prefix"}}
	for _, path := range NPMActivityPaths(runtime, "npm", "lodash", nil) {
		if !filepath.IsAbs(path) {
			t.Errorf("relative candidate %q was kept", path)
		}
	}
}

// The package name comes from the manifest, but it becomes a path the watchdog
// walks, so anything that could leave the package's own directory is refused.
func TestNPMActivityPathsRefuseUnsafePackageNames(t *testing.T) {
	runtime := Runtime{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Env: map[string]string{}}
	for _, name := range []string{"", "..", "../etc", "@scope/../x", "a/b", "@scope/", "@/x", `@s\x`, "a/b/c"} {
		if paths := NPMActivityPaths(runtime, "/usr/bin/npm", name, nil); paths != nil {
			t.Errorf("NPMActivityPaths(%q) = %v, want nil", name, paths)
		}
	}
}
