package securefs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

func fixedClock() time.Time {
	return time.Date(2026, time.July, 30, 12, 34, 56, 0, time.UTC)
}

func TestAtomicWriteCreatesPrivateFileAndCollisionSafeBackup(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, ".oneagent", "profile.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(Options{OS: "linux", Now: fixedClock})
	backupPath, err := store.AtomicWrite(context.Background(), target, []byte("new"), true)
	if err != nil {
		t.Fatal(err)
	}
	if backupPath != target+".backup-20260730123456" {
		t.Fatalf("backup path = %q", backupPath)
	}
	assertMode(t, filepath.Dir(target), 0o700)
	assertMode(t, target, 0o600)
	assertMode(t, backupPath, 0o600)
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Fatalf("target content = %q", got)
	}
	if got, _ := os.ReadFile(backupPath); string(got) != "old" {
		t.Fatalf("backup content = %q", got)
	}
	if err := os.WriteFile(target+".backup-20260730123456", []byte("collision"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AtomicWrite(context.Background(), target, []byte("newer"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target + ".backup-20260730123456-1"); err != nil {
		t.Fatalf("collision backup missing: %v", err)
	}
}

func TestAtomicWriteUsesManagedBackupRootAndPrunesPerTarget(t *testing.T) {
	home := t.TempDir()
	backupRoot := filepath.Join(home, ".oneagent", "backup")
	first := filepath.Join(home, ".config", "first.json")
	second := filepath.Join(home, ".config", "second.json")
	if err := os.MkdirAll(filepath.Dir(first), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := New(Options{
		OS:         "linux",
		BackupRoot: backupRoot,
		Retention:  func() int { return 3 },
		Now:        fixedClock,
	})
	for i := 0; i < 5; i++ {
		if _, err := store.AtomicWrite(context.Background(), first, []byte(fmt.Sprintf("first-%d", i)), false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AtomicWrite(context.Background(), second, []byte("second-new"), false); err != nil {
		t.Fatal(err)
	}
	firstEntries, err := os.ReadDir(BackupGroupPath(backupRoot, first))
	if err != nil {
		t.Fatal(err)
	}
	if len(firstEntries) != 3 {
		t.Fatalf("first backups = %d, want 3", len(firstEntries))
	}
	secondEntries, err := os.ReadDir(BackupGroupPath(backupRoot, second))
	if err != nil {
		t.Fatal(err)
	}
	if len(secondEntries) != 1 {
		t.Fatalf("second backups = %d, want 1", len(secondEntries))
	}
	if matches, _ := filepath.Glob(first + ".backup-*"); len(matches) != 0 {
		t.Fatalf("legacy beside-file backups remain: %v", matches)
	}
}

func TestAtomicWriteUsesDefaultBackupRetention(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "config.json")
	if err := os.WriteFile(target, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(Options{OS: "linux", BackupRoot: filepath.Join(home, ".oneagent", "backup"), Now: fixedClock})
	for i := 0; i < 5; i++ {
		if _, err := store.AtomicWrite(context.Background(), target, []byte(fmt.Sprintf("value-%d", i)), false); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(BackupGroupPath(filepath.Join(home, ".oneagent", "backup"), target))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("default backups = %d, want 3", len(entries))
	}
}

func TestAtomicWriteMigratesAndPrunesLegacyBackups(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "config.json")
	if err := os.WriteFile(target, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		path := fmt.Sprintf("%s.backup-2026073000000%d", target, i)
		if err := os.WriteFile(path, []byte(fmt.Sprintf("legacy-%d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Date(2026, time.July, 30, 0, 0, i, 0, time.UTC)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	backupRoot := filepath.Join(home, ".oneagent", "backup")
	store := New(Options{OS: "linux", BackupRoot: backupRoot, Retention: func() int { return 3 }, Now: fixedClock})
	if _, err := store.AtomicWrite(context.Background(), target, []byte("replacement"), false); err != nil {
		t.Fatal(err)
	}
	if matches, _ := filepath.Glob(target + ".backup-*"); len(matches) != 0 {
		t.Fatalf("legacy backups remain after migration: %v", matches)
	}
	entries, err := os.ReadDir(BackupGroupPath(backupRoot, target))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("managed backups = %d, want 3", len(entries))
	}
	contents := map[string]bool{}
	for _, entry := range entries {
		data, readErr := os.ReadFile(filepath.Join(BackupGroupPath(backupRoot, target), entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		contents[string(data)] = true
	}
	for _, want := range []string{"current", "legacy-3", "legacy-4"} {
		if !contents[want] {
			t.Fatalf("retained contents = %v, missing %q", contents, want)
		}
	}
}

func TestAtomicWriteRejectsSymlinkedManagedBackupRoot(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("symlink permissions are environment-dependent on Windows")
	}
	home := t.TempDir()
	target := filepath.Join(home, "config.json")
	if err := os.WriteFile(target, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	backupRoot := filepath.Join(home, ".oneagent", "backup")
	if err := os.MkdirAll(filepath.Dir(backupRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, backupRoot); err != nil {
		t.Fatal(err)
	}
	store := New(Options{OS: "linux", BackupRoot: backupRoot})
	if _, err := store.AtomicWrite(context.Background(), target, []byte("replacement"), false); err == nil {
		t.Fatal("write followed a symlinked managed backup root")
	}
	if entries, err := os.ReadDir(outside); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("symlink target was modified: %v", entries)
	}
}

func TestSecretBackupSecurityFailureRemovesBackup(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "secret")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(Options{
		OS:  "linux",
		Now: fixedClock,
		Secure: func(path string, directory bool) error {
			if !directory && strings.Contains(path, ".backup-") {
				return oneerrors.New(oneerrors.ConfigWriteFailed, "backup ACL failed")
			}
			return nil
		},
	})
	_, err := store.AtomicWrite(context.Background(), target, []byte("new"), true)
	if err == nil || !strings.Contains(err.Error(), "backup ACL failed") {
		t.Fatalf("security failure = %v", err)
	}
	matches, _ := filepath.Glob(target + ".backup-*")
	if len(matches) != 0 {
		t.Fatalf("insecure backups remain: %v", matches)
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Fatalf("original changed after security failure: %q", got)
	}
}

func TestTemporarySecurityFailureLeavesNoPublishedReplacement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, ".oneagent", "new")
	callCount := 0
	store := New(Options{
		OS:  "linux",
		Now: fixedClock,
		Secure: func(_ string, directory bool) error {
			if !directory {
				callCount++
				if callCount == 1 {
					return oneerrors.New(oneerrors.ConfigWriteFailed, "temporary ACL failed")
				}
			}
			return nil
		},
	})
	_, err := store.AtomicWrite(context.Background(), target, []byte("replacement"), false)
	if err == nil || !strings.Contains(err.Error(), "temporary ACL failed") {
		t.Fatalf("temporary failure = %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("replacement was published: %v", statErr)
	}
	entries, readErr := os.ReadDir(filepath.Dir(target))
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".oneagent-tmp-") {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}

func TestWindowsACLCommandsAreRestrictedAndContextAware(t *testing.T) {
	var calls [][]string
	store := New(Options{
		OS:       "windows",
		Username: "tester",
		Run: func(_ context.Context, argv []string) error {
			calls = append(calls, append([]string(nil), argv...))
			return nil
		},
	})
	root := t.TempDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.SecureFile(context.Background(), filepath.Join(root, "secret.ps1")); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsurePrivateDir(context.Background(), filepath.Join(root, "nested")); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("ACL calls = %v", calls)
	}
	if !reflect.DeepEqual(calls[0], []string{"icacls", filepath.Join(root, "secret.ps1"), "/reset"}) || !reflect.DeepEqual(calls[1], []string{"icacls", filepath.Join(root, "secret.ps1"), "/inheritance:r", "/grant:r", "tester:F", "*S-1-5-18:F"}) {
		t.Fatalf("file ACL calls = %v", calls[:2])
	}
	if !strings.Contains(strings.Join(calls[3], " "), "tester:(OI)(CI)F") {
		t.Fatalf("directory ACL grant = %v", calls[3])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.SecureFile(ctx, filepath.Join(root, "cancelled")); err == nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("cancelled ACL = %v", err)
	}
}

func TestAtomicWriteMapsFilesystemFailuresAndDoesNotLeakContent(t *testing.T) {
	store := New(Options{OS: "linux"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.AtomicWrite(ctx, filepath.Join(t.TempDir(), "missing", "target"), []byte("secret-value"), false)
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("cancelled filesystem request = %v", err)
	}
}

// Every failure writeError reports is a condition outside the process --
// permission, ownership, a full disk, a file held open by the Agent. The user
// fixes it and the same write succeeds, so the activation page has to offer a
// retry; it renders that button only when Retryable is set.
func TestConfigWriteFailuresAreRetryable(t *testing.T) {
	store := New(Options{OS: "linux"})
	directory := t.TempDir()
	blocker := filepath.Join(directory, "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A file where a directory has to be: the temporary-file create fails with
	// ENOTDIR, which is the same shape as the permission failures this guards.
	_, err := store.AtomicWrite(context.Background(), filepath.Join(blocker, "target"), []byte("secret-value"), false)
	converted := oneerrors.As(err)
	if err == nil || converted.Code != oneerrors.ConfigWriteFailed {
		t.Fatalf("write into a non-directory = %v", err)
	}
	if !converted.Retryable {
		t.Fatalf("a filesystem write failure is fixable outside the app, so it must be retryable: %+v", converted)
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("write failure leaked the payload: %q", err.Error())
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
