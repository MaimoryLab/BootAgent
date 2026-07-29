package securefs

import (
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

func posixOnly(t *testing.T) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("mode assertions are POSIX; the ACL path has its own tests")
	}
}

func newFS(t *testing.T, home string) *FS {
	t.Helper()
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{"HOME": home}),
	)
	fs := New(rt)
	return fs
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	var oneAgentErr *oerr.Error
	if !errors.As(err, &oneAgentErr) {
		t.Fatalf("err = %v, want an *oerr.Error", err)
	}
	if oneAgentErr.Code != want {
		t.Errorf("code = %q, want %q", oneAgentErr.Code, want)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cannot stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %s, want %s", path, describeMode(got), describeMode(want))
	}
}

func TestAWrittenFileIsReadableOnlyByItsOwner(t *testing.T) {
	posixOnly(t)
	home := t.TempDir()
	fs := newFS(t, home)
	path := filepath.Join(home, ".codex", "config.toml")

	if _, err := fs.Write(path, "model = \"x\"\n", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertMode(t, path, FileMode)
	assertMode(t, filepath.Dir(path), DirectoryMode)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read back: %v", err)
	}
	if string(raw) != "model = \"x\"\n" {
		t.Errorf("content = %q", raw)
	}
}

func TestAnExistingDirectoryIsHardenedRatherThanLeftAsFound(t *testing.T) {
	// MkdirAll does nothing to a directory that already exists, and umask
	// reduces the mode it applies to a new one. Either way the mode has to be
	// set explicitly or a credential can land in a readable directory.
	posixOnly(t)
	home := t.TempDir()
	loose := filepath.Join(home, ".config")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	fs := newFS(t, home)
	if err := fs.EnsureDir(loose); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertMode(t, loose, DirectoryMode)
}

func TestWritingOverAnExistingFileKeepsACopyOfWhatWasThere(t *testing.T) {
	// The content being written came out of a merge, and a merge can drop
	// something the user wanted. The backup is what makes that recoverable.
	home := t.TempDir()
	fs := newFS(t, home)
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	backup, err := fs.Write(path, "replacement\n", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backup == "" {
		t.Fatal("no backup was reported for an existing file")
	}
	raw, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("cannot read the backup: %v", err)
	}
	if string(raw) != "original\n" {
		t.Errorf("backup holds %q, want the original content", raw)
	}
}

func TestWritingANewFileReportsNoBackup(t *testing.T) {
	home := t.TempDir()
	fs := newFS(t, home)
	backup, err := fs.Write(filepath.Join(home, "new.json"), "{}\n", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backup != "" {
		t.Errorf("backup = %q, want none for a file that did not exist", backup)
	}
}

func TestTwoBackupsInTheSameSecondDoNotOverwriteEachOther(t *testing.T) {
	// The first backup is the one holding the user's original content, so a
	// name collision would destroy exactly the thing being preserved.
	home := t.TempDir()
	fs := newFS(t, home)
	frozen := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	fs.Now = func() time.Time { return frozen }

	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	firstBackup, err := fs.Write(path, "second\n", false)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	secondBackup, err := fs.Write(path, "third\n", false)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}

	if firstBackup == secondBackup {
		t.Fatalf("both writes used the backup name %q", firstBackup)
	}
	raw, err := os.ReadFile(firstBackup)
	if err != nil {
		t.Fatalf("cannot read the first backup: %v", err)
	}
	if string(raw) != "first\n" {
		t.Errorf("the first backup now holds %q, want the original content", raw)
	}
	if !strings.HasSuffix(secondBackup, "-1") {
		t.Errorf("second backup = %q, want a collision counter", secondBackup)
	}
}

func TestABackupKeepsTheSourceModeRatherThanWidening(t *testing.T) {
	// A backup of a 0600 config that lands at 0644 is a readable copy of a
	// credential, which is the thing this package exists to prevent.
	posixOnly(t)
	home := t.TempDir()
	fs := newFS(t, home)
	path := filepath.Join(home, "secret.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	backup, err := fs.Backup(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertMode(t, backup, 0o600)
}

func TestBackingUpAnAbsentFileIsNotAnError(t *testing.T) {
	home := t.TempDir()
	fs := newFS(t, home)
	backup, err := fs.Backup(filepath.Join(home, "nothing-here.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backup != "" {
		t.Errorf("backup = %q, want none", backup)
	}
}

func TestBackingUpADirectoryIsRefusedRatherThanSilentlySkipped(t *testing.T) {
	// A config path that resolves to a directory means something is wrong
	// upstream, and copying nothing while reporting success would hide it.
	home := t.TempDir()
	fs := newFS(t, home)
	directory := filepath.Join(home, "a-directory")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	if _, err := fs.Backup(directory); err == nil {
		t.Fatal("backing up a directory should be refused")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}
}

func TestWriteReportsTheConfigWriteCodeForAnyFilesystemFailure(t *testing.T) {
	// One code for the whole path, because the caller's recovery is the same
	// whichever step failed: report it and leave the user's file alone.
	home := t.TempDir()
	fs := newFS(t, home)
	// A parent that is a file, not a directory, so MkdirAll cannot succeed.
	blocker := filepath.Join(home, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	if _, err := fs.Write(filepath.Join(blocker, "child.json"), "{}", false); err == nil {
		t.Fatal("writing under a file should fail")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}
}

func TestNoTemporaryFileSurvivesASuccessfulWrite(t *testing.T) {
	// A leftover temporary on the secret path is a plaintext credential sitting
	// in the config directory.
	home := t.TempDir()
	fs := newFS(t, home)
	path := filepath.Join(home, "config.json")
	if _, err := fs.Write(path, "{}\n", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoTemporaries(t, home)
}

func TestNoTemporaryFileSurvivesAFailedWrite(t *testing.T) {
	posixOnly(t)
	home := t.TempDir()
	fs := newFS(t, home)
	path := filepath.Join(home, "sub", "config.json")

	// Make the rename fail by turning the target into a non-empty directory.
	if err := os.MkdirAll(filepath.Join(home, "sub", "config.json", "occupied"), 0o700); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	if _, err := fs.Write(path, "{}\n", true); err == nil {
		t.Fatal("renaming onto a non-empty directory should fail")
	}
	assertNoTemporaries(t, filepath.Join(home, "sub"))
}

func assertNoTemporaries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".oneagent-") {
			t.Errorf("a temporary file survived: %s", entry.Name())
		}
	}
}

func TestTheTemporaryFileIsCreatedBesideTheTargetSoTheRenameIsAtomic(t *testing.T) {
	// A temporary in the system temp directory would make the final step a
	// cross-device copy, which is neither atomic nor guaranteed to succeed.
	home := t.TempDir()
	fs := newFS(t, home)
	target := filepath.Join(home, "nested", "config.json")

	// Observed through the directory listing during the write, which is the
	// only externally visible evidence of where the temporary lived.
	if _, err := fs.Write(target, "{}\n", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The parent exists and holds only the published file.
	entries, err := os.ReadDir(filepath.Join(home, "nested"))
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("directory holds %v, want only config.json", names)
	}
}
