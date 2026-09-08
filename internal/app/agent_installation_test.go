package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
)

func TestNPMInstallationFollowsExecutableSymlinkToPackage(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, ".npm-global")
	packageRoot := filepath.Join(prefix, "lib", "node_modules", "openclaw")
	launcher := filepath.Join(packageRoot, "dist", "cli.js")
	if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte(`{"name":"openclaw"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("#!/usr/bin/env node\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(home, ".local", "bin", "openclaw")
	if err := os.MkdirAll(filepath.Dir(command), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(launcher, command); err != nil {
		t.Fatal(err)
	}

	gotPrefix, gotPackage, ok := npmInstallationForPath(command, catalog.Agent{Package: &catalog.Package{Name: "openclaw"}})
	canonicalPrefix, _ := filepath.EvalSymlinks(prefix)
	if !ok || gotPrefix != canonicalPrefix || gotPackage != "openclaw" {
		t.Fatalf("npm Antrag from symlink = prefix %q package %q ok=%v", gotPrefix, gotPackage, ok)
	}
}
