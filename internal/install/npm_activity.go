package install

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// NPMActivityPaths lists where a global npm install or update of packageName
// writes while it downloads, for the stall watchdog to treat as progress (see
// process.WithActivityPaths).
//
// Two places, both measured during a deliberately slow `npm update -g`: the
// package's directory under the global node_modules, which npm recreates after
// moving the old copy aside and fills as the tarball streams in, and the cache's
// tmp directory, where the tarball itself is staged byte by byte. npm printed
// nothing at all while both grew.
//
// Getting these right matters more than it looks: a stall now stops the whole
// install tree, so a download whose directories were missed would be cut off
// mid-way rather than merely misreported. The prefix and cache are therefore
// read from wherever npm reads them -- the environment and the npmrc files --
// rather than assumed, and every plausible prefix is listed. Asking npm would
// cost a process per install; a candidate that does not exist is skipped by the
// watchdog for free, and either signal alone is enough.
func NPMActivityPaths(runtime Runtime, npm, packageName string, overrides map[string]string) []string {
	packageName = strings.TrimSpace(packageName)
	if !safePackagePath(packageName) {
		return nil
	}
	lookup := func(name string) string {
		for _, source := range []map[string]string{overrides, runtime.Env} {
			for key, value := range source {
				if strings.EqualFold(key, name) && strings.TrimSpace(value) != "" {
					return strings.TrimSpace(value)
				}
			}
		}
		return ""
	}
	windows := runtime.Platform.OS == "windows"
	home := runtime.Home
	config := npmConfigFiles(npmrcLocations(runtime, npm, lookup), home, lookup)

	var prefixes []string
	if prefix := lookup("npm_config_prefix"); prefix != "" {
		prefixes = append(prefixes, prefix)
	}
	prefixes = append(prefixes, config.values("prefix")...)
	if managedNPM(runtime, npm) {
		prefixes = append(prefixes, GlobalPrefix(home))
	}
	if root := npmNodeRoot(npm, windows); root != "" {
		prefixes = append(prefixes, root)
	}
	if windows {
		// The Node installer's default global prefix, which is not where npm.cmd
		// lives (that is Program Files).
		if appData := lookup("APPDATA"); appData != "" {
			prefixes = append(prefixes, filepath.Join(appData, "npm"))
		}
	}

	paths := make([]string, 0, len(prefixes)+1)
	seen := map[string]bool{}
	add := func(path string) {
		path = filepath.Clean(path)
		// A relative candidate -- npm found on PATH as a bare name, an unexpanded
		// variable -- would be walked from the working directory, which is not
		// where npm writes.
		if !filepath.IsAbs(path) {
			return
		}
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	for _, prefix := range prefixes {
		root := filepath.Join(prefix, "lib", "node_modules")
		if windows {
			root = filepath.Join(prefix, "node_modules")
		}
		add(filepath.Join(root, filepath.FromSlash(packageName)))
	}

	// Unlike the prefix, the cache is one place: the first source npm consults
	// that sets it is the one npm writes to.
	cache := lookup("npm_config_cache")
	if cache == "" {
		if values := config.values("cache"); len(values) > 0 {
			cache = values[0]
		}
	}
	if cache == "" {
		if windows {
			if local := lookup("LOCALAPPDATA"); local != "" {
				cache = filepath.Join(local, "npm-cache")
			}
		} else if home != "" {
			cache = filepath.Join(home, ".npm")
		}
	}
	if cache != "" {
		// Only tmp: the rest of the cache is every package ever fetched, which
		// would make each sample slow and tell nothing about this download.
		add(filepath.Join(cache, "_cacache", "tmp"))
	}
	return paths
}

// npmNodeRoot is the directory npm runs from: <root>/bin/npm on POSIX and
// <root>\npm.cmd on Windows. A symlinked npm, as Homebrew installs it, is
// followed to the real installation.
func npmNodeRoot(npm string, windows bool) string {
	if npm == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(npm); err == nil {
		npm = resolved
	}
	dir := filepath.Dir(npm)
	if windows {
		return dir
	}
	// Following the link usually lands on npm's own script, in
	// <root>/lib/node_modules/npm/bin; checked first, because that directory is
	// also called bin.
	if packageBin := filepath.Join("lib", "node_modules", "npm", "bin"); strings.HasSuffix(dir, string(filepath.Separator)+packageBin) {
		return strings.TrimSuffix(dir, string(filepath.Separator)+packageBin)
	}
	if filepath.Base(dir) == "bin" {
		return filepath.Dir(dir)
	}
	return dir
}

// npmrcLocations lists the config files npm reads for a global command, in
// the order it consults them: the user's, the global one under the Node
// install, and the npmrc shipped inside npm itself -- which is where the
// Windows installer puts its %APPDATA%\npm prefix. A project .npmrc is not
// read for a global command, so it is not listed.
func npmrcLocations(runtime Runtime, npm string, lookup func(string) string) []string {
	windows := runtime.Platform.OS == "windows"
	var files []string
	if user := lookup("npm_config_userconfig"); user != "" {
		files = append(files, user)
	} else if runtime.Home != "" {
		files = append(files, filepath.Join(runtime.Home, ".npmrc"))
	}
	root := npmNodeRoot(npm, windows)
	if global := lookup("npm_config_globalconfig"); global != "" {
		files = append(files, global)
	} else if root != "" {
		files = append(files, filepath.Join(root, "etc", "npmrc"))
	}
	if root != "" {
		if windows {
			files = append(files, filepath.Join(root, "node_modules", "npm", "npmrc"))
		} else {
			files = append(files, filepath.Join(root, "lib", "node_modules", "npm", "npmrc"))
		}
	}
	return files
}

type npmConfig [][2]string

// values returns every setting of key across the files, in precedence order.
func (c npmConfig) values(key string) []string {
	var found []string
	for _, entry := range c {
		if entry[0] == key {
			found = append(found, entry[1])
		}
	}
	return found
}

// maxNPMRCBytes bounds what is read of one npmrc. Real ones are a handful of
// lines; this only keeps a stray large file from being read on every install.
const maxNPMRCBytes = 64 << 10

var npmrcVariable = regexp.MustCompile(`\$\{([^}?]+)\??\}`)

// npmConfigFiles reads prefix and cache out of the given npmrc files the way
// npm parses them: `key = value` lines, `;` and `#` comments, optional quotes,
// ${VAR} expansion and a leading ~ for the home directory. A file that is
// missing or unreadable contributes nothing.
func npmConfigFiles(files []string, home string, lookup func(string) string) npmConfig {
	var config npmConfig
	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(io.LimitReader(file, maxNPMRCBytes))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.ToLower(strings.TrimSpace(key))
			if key != "prefix" && key != "cache" {
				continue
			}
			value = strings.TrimSpace(value)
			if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
				value = value[1 : len(value)-1]
			}
			value = npmrcVariable.ReplaceAllStringFunc(value, func(match string) string {
				return lookup(npmrcVariable.FindStringSubmatch(match)[1])
			})
			if home != "" && (value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`)) {
				value = filepath.Join(home, value[1:])
			}
			if value != "" {
				config = append(config, [2]string{key, value})
			}
		}
		_ = file.Close()
	}
	return config
}

// safePackagePath accepts the name shapes npm allows, "name" and
// "@scope/name", so a manifest value can never turn the watched path into
// something outside the package's own directory.
func safePackagePath(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) > 2 || (len(parts) == 2 && !strings.HasPrefix(parts[0], "@")) {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || part == "@" || strings.ContainsAny(part, `\:`) {
			return false
		}
	}
	return true
}
