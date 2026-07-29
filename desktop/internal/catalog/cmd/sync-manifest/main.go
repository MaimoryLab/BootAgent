// Command sync-manifest refreshes the embedded copy of agents.lock.json.
//
// go:embed cannot reach outside its package directory and refuses symlinks, so
// the manifest has to be copied in. Running this by hand would eventually be
// forgotten, which is why embed_parity_test.go fails on any difference and
// names this command in its message.
//
//	cd desktop && go generate ./internal/catalog/
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sync-manifest:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	source := filepath.Join(root, "agents.lock.json")
	raw, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("cannot read the manifest at %s: %w", source, err)
	}
	destination := filepath.Join(root, "desktop", "internal", "catalog", "agents.lock.embed.json")
	// Compared first so an unchanged manifest does not touch the file's mtime
	// and trigger a rebuild for nothing.
	if existing, err := os.ReadFile(destination); err == nil && string(existing) == string(raw) {
		fmt.Println("sync-manifest: already current")
		return nil
	}
	if err := os.WriteFile(destination, raw, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", destination, err)
	}
	fmt.Printf("sync-manifest: copied %d bytes to %s\n", len(raw), destination)
	return nil
}

// repoRoot walks up until it finds the manifest, so this works from the module
// root or from the package directory.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "agents.lock.json")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("walked to the filesystem root without finding agents.lock.json")
		}
		dir = parent
	}
}
