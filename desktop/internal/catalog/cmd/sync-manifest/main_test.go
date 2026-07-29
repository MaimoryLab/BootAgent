package main

import (
	"os"
	"path/filepath"
	"testing"
)

// This command decides whether the embedded manifest matches the real one, so a
// bug here would make the parity test compare a copy against itself.

func TestRepoRootFindsTheManifestByWalkingUp(t *testing.T) {
	// Walking up rather than counting ".." means the command works from the
	// module root or from the package directory.
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "agents.lock.json")); err != nil {
		t.Fatalf("repoRoot returned %q, which holds no manifest: %v", root, err)
	}
}

func TestRepoRootReportsFailureRatherThanGuessingWhenThereIsNoManifest(t *testing.T) {
	// Writing the copy to an invented path would leave a stale embed that the
	// parity test then compares against nothing.
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot read the working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("cannot restore the working directory: %v", err)
		}
	}()

	if err := os.Chdir(t.TempDir()); err != nil {
		t.Skipf("cannot change directory in this environment: %v", err)
	}
	if _, err := repoRoot(); err == nil {
		t.Fatal("expected an error when no manifest exists above the working directory")
	}
}

func TestRunIsIdempotentSoAnUnchangedManifestDoesNotForceARebuild(t *testing.T) {
	// Rewriting an identical file would touch its mtime and make every build
	// recompile the catalog package for nothing.
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	destination := filepath.Join(root, "desktop", "internal", "catalog", "agents.lock.embed.json")

	if err := run(); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	before, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("cannot stat the embedded copy: %v", err)
	}

	if err := run(); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	after, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("cannot stat the embedded copy: %v", err)
	}

	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("an unchanged manifest was rewritten, which forces a needless rebuild")
	}
}

func TestRunLeavesTheCopyByteIdenticalToTheSource(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := run(); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	source, err := os.ReadFile(filepath.Join(root, "agents.lock.json"))
	if err != nil {
		t.Fatalf("cannot read the source manifest: %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(root, "desktop", "internal", "catalog", "agents.lock.embed.json"))
	if err != nil {
		t.Fatalf("cannot read the embedded copy: %v", err)
	}
	if string(source) != string(copied) {
		t.Error("the copy differs from the source after a successful run")
	}
}
