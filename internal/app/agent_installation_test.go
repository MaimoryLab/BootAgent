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

	gotPrefix, gotPackage, ok := npmInstallationForPath(command, catalog.Agent{Package: &catalog.Package{Name: "openclaw"}}, "linux")
	canonicalPrefix, _ := filepath.EvalSymlinks(prefix)
	if !ok || gotPrefix != canonicalPrefix || gotPackage != "openclaw" {
		t.Fatalf("npm Antrag from symlink = prefix %q package %q ok=%v", gotPrefix, gotPackage, ok)
	}
}

func TestNPMInstallationRejectsLocalPackagesAndUnrelatedPrefixes(t *testing.T) {
	for _, layout := range []string{"node_modules/@openai/codex", "node_modules/.pnpm/@openai+codex@1.0.0/node_modules/@openai/codex"} {
		t.Run(layout, func(t *testing.T) {
			home := t.TempDir()
			packageRoot := filepath.Join(home, filepath.FromSlash(layout))
			launcher := filepath.Join(packageRoot, "bin", "codex.js")
			if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte(`{"name":"@openai/codex"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(launcher, []byte("test"), 0o700); err != nil {
				t.Fatal(err)
			}
			command := filepath.Join(home, "node_modules", ".bin", "codex")
			if err := os.MkdirAll(filepath.Dir(command), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(launcher, command); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			prefix := t.TempDir()
			unrelatedPackage := filepath.Join(prefix, "lib", "node_modules", "@openai", "codex")
			if err := os.MkdirAll(unrelatedPackage, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(unrelatedPackage, "package.json"), []byte(`{"name":"@openai/codex"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("NPM_CONFIG_PREFIX", prefix)
			t.Setenv("npm_config_prefix", prefix)
			for _, osID := range []string{"linux", "windows"} {
				if prefix, _, ok := npmInstallationForPath(command, catalog.Agent{Package: &catalog.Package{Name: "@openai/codex"}}, osID); ok {
					t.Errorf("%s: project-local executable was classified as global: %s", osID, prefix)
				}
			}
		})
	}
}

func TestNPMInstallationRecognizesWindowsGlobalShim(t *testing.T) {
	prefix := t.TempDir()
	packageRoot := filepath.Join(prefix, "node_modules", "@openai", "codex")
	if err := os.MkdirAll(packageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(prefix, "codex.cmd")
	if err := os.WriteFile(command, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(packageRoot, "package.json")
	for _, test := range []struct {
		metadata string
		want     bool
	}{
		{`{"name":"@openai/codex"}`, true},
		{`{"name":"unrelated"}`, false},
		{`{invalid`, false},
	} {
		if err := os.WriteFile(manifestPath, []byte(test.metadata), 0o600); err != nil {
			t.Fatal(err)
		}
		gotPrefix, _, ok := npmInstallationForPath(command, catalog.Agent{Package: &catalog.Package{Name: "@openai/codex"}}, "windows")
		if ok != test.want {
			t.Fatalf("metadata %q: recognized = %v, want %v", test.metadata, ok, test.want)
		}
		canonicalPrefix, _ := filepath.EvalSymlinks(prefix)
		if ok && gotPrefix != canonicalPrefix {
			t.Fatalf("prefix = %q, want %q", gotPrefix, canonicalPrefix)
		}
	}
}
