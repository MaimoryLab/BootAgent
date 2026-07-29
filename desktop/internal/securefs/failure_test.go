package securefs

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// The failure paths are the reason this package exists. A write that succeeds is
// easy; what matters is that a failure never leaves a readable credential behind
// and never destroys the file the user already had.

// windowsFS builds an FS that takes the Windows ACL path with a fake icacls.
// failOn decides which paths the ACL call rejects, so a test can fail hardening
// for the temporary file while leaving the directory alone -- which is what a
// real ACL failure looks like.
func windowsFS(t *testing.T, home string, failOn func(path string) bool) (*FS, *[]string) {
	t.Helper()
	calls := &[]string{}
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("windows"),
		runtime.WithEnv(map[string]string{"USERNAME": "tester"}),
		runtime.WithLookup(func(name string) (string, bool) {
			if name == "icacls" {
				return `C:\Windows\System32\icacls.exe`, true
			}
			return "", false
		}),
		runtime.WithRunner(func(_ context.Context, argv []string, _ runtime.RunOptions) (runtime.Result, error) {
			*calls = append(*calls, strings.Join(argv, " "))
			if len(argv) > 1 && failOn != nil && failOn(argv[1]) {
				return runtime.Result{ExitCode: 5, Stderr: "Access is denied."}, nil
			}
			return runtime.Result{}, nil
		}),
	)
	return New(rt), calls
}

func TestAnACLFailureOnTheTemporaryFileLeavesTheExistingFileIntact(t *testing.T) {
	// This is why hardening happens before the rename. Doing it after would
	// have already replaced the user's config, so a permission failure would
	// cost them their file as well as the write.
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte(`{"kept":"yes"}`), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	// Fail for anything that is not the directory: the temporary file and the
	// backup both live inside it.
	fs, _ := windowsFS(t, home, func(target string) bool { return target != home })

	if _, err := fs.Write(path, `{"replaced":"yes"}`, false); err == nil {
		t.Fatal("the write should have failed")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the original file is gone: %v", err)
	}
	if string(raw) != `{"kept":"yes"}` {
		t.Fatalf("the original content was replaced: %q", raw)
	}
	assertNoTemporaries(t, home)
}

func TestASecretBackupThatCannotBeHardenedIsDeleted(t *testing.T) {
	// A readable copy of a credential is worse than having no backup at all, so
	// the backup is removed and the hardening failure is what gets reported.
	home := t.TempDir()
	path := filepath.Join(home, "auth.json")
	if err := os.WriteFile(path, []byte(`{"key":"sk-original"}`), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	// Fail only for backup files, so the directory and the temporary succeed
	// and the test isolates the secret-backup branch.
	fs, _ := windowsFS(t, home, func(target string) bool {
		return strings.Contains(target, ".backup-")
	})

	if _, err := fs.Write(path, `{"key":"sk-new"}`, true); err == nil {
		t.Fatal("the write should have failed")
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".backup-") {
			t.Errorf("an unsecurable secret backup was left behind: %s", entry.Name())
		}
	}
}

func TestANonSecretBackupIsNotHardenedSeparately(t *testing.T) {
	// The extra hardening step exists for credential files. Applying it to
	// every backup would make an ACL quirk fail writes that carry no secret.
	home := t.TempDir()
	path := filepath.Join(home, "plain.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	fs, calls := windowsFS(t, home, nil)
	if _, err := fs.Write(path, `{"new":true}`, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, call := range *calls {
		if strings.Contains(call, ".backup-") {
			t.Errorf("a non-secret backup was hardened: %s", call)
		}
	}
}

func TestTheACLIsResetBeforeItIsGranted(t *testing.T) {
	// Granting without resetting first leaves whatever entries were already
	// there, so the file stays reachable by principals the grant never named.
	home := t.TempDir()
	fs, calls := windowsFS(t, home, nil)
	if err := fs.EnsureDir(filepath.Join(home, "private")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(*calls) < 2 {
		t.Fatalf("expected two icacls calls, got %v", *calls)
	}
	if !strings.Contains((*calls)[0], "/reset") {
		t.Errorf("first call = %q, want /reset", (*calls)[0])
	}
	second := (*calls)[1]
	for _, fragment := range []string{"/inheritance:r", "/grant:r", "tester:(OI)(CI)F", "*S-1-5-18:(OI)(CI)F"} {
		if !strings.Contains(second, fragment) {
			t.Errorf("second call = %q, missing %q", second, fragment)
		}
	}
}

func TestAFileGrantOmitsTheInheritanceFlags(t *testing.T) {
	// (OI)(CI) describes inheritance to children, which a file does not have.
	home := t.TempDir()
	target := filepath.Join(home, "file.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	fs, calls := windowsFS(t, home, nil)
	if err := fs.Secure(target, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	grant := (*calls)[1]
	if strings.Contains(grant, "(OI)(CI)") {
		t.Errorf("file grant = %q, want no inheritance flags", grant)
	}
	if !strings.Contains(grant, "tester:F") {
		t.Errorf("file grant = %q, want tester:F", grant)
	}
}

func TestAMissingIcaclsIsAHardFailureRatherThanAWarning(t *testing.T) {
	// Continuing without it would leave a credential file readable by every
	// account on the machine, which is exactly what this call prevents.
	home := t.TempDir()
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("windows"),
		runtime.WithEnv(map[string]string{"USERNAME": "tester"}),
		runtime.WithLookup(func(string) (string, bool) { return "", false }),
		runtime.WithRunner(func(_ context.Context, argv []string, _ runtime.RunOptions) (runtime.Result, error) {
			t.Errorf("no subprocess should run when icacls is absent; got %v", argv)
			return runtime.Result{}, nil
		}),
	)
	fs := New(rt)
	err := fs.EnsureDir(filepath.Join(home, "private"))
	if err == nil {
		t.Fatal("a missing icacls must fail the write")
	}
	assertCode(t, err, "CONFIG_WRITE_FAILED")
	if !strings.Contains(err.Error(), "icacls") {
		t.Errorf("message = %q, want it to name the missing tool", err.Error())
	}
}

func TestAnACLSubprocessThatCannotStartIsReportedNotIgnored(t *testing.T) {
	home := t.TempDir()
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("windows"),
		runtime.WithEnv(map[string]string{"USERNAME": "tester"}),
		runtime.WithLookup(func(string) (string, bool) { return `C:\icacls.exe`, true }),
		runtime.WithRunner(func(_ context.Context, argv []string, _ runtime.RunOptions) (runtime.Result, error) {
			return runtime.Result{}, &runtime.StartError{Argv: argv, Err: errors.New("not executable")}
		}),
	)
	if err := New(rt).EnsureDir(filepath.Join(home, "private")); err == nil {
		t.Fatal("a start failure must not be treated as success")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}
}

func TestTheACLCallCarriesATimeoutSoAHungToolCannotStallAWrite(t *testing.T) {
	home := t.TempDir()
	observed := []int64{}
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("windows"),
		runtime.WithEnv(map[string]string{"USERNAME": "tester"}),
		runtime.WithLookup(func(string) (string, bool) { return `C:\icacls.exe`, true }),
		runtime.WithRunner(func(_ context.Context, _ []string, opts runtime.RunOptions) (runtime.Result, error) {
			observed = append(observed, int64(opts.Timeout))
			return runtime.Result{}, nil
		}),
	)
	if err := New(rt).EnsureDir(filepath.Join(home, "private")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, timeout := range observed {
		if timeout <= 0 {
			t.Error("an ACL call ran with no timeout")
		}
	}
}

func TestTheUsernameFallsBackToTheOSWhenTheEnvironmentIsEmpty(t *testing.T) {
	home := t.TempDir()
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("windows"),
		runtime.WithEnv(map[string]string{}),
		runtime.WithLookup(func(string) (string, bool) { return `C:\icacls.exe`, true }),
		runtime.WithRunner(func(_ context.Context, _ []string, _ runtime.RunOptions) (runtime.Result, error) {
			return runtime.Result{}, nil
		}),
	)
	fs := New(rt)
	name, err := fs.username()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name == "" {
		t.Error("no username resolved")
	}
}

func TestWhenNothingCanNameTheUserTheWriteIsRefused(t *testing.T) {
	// Guessing a principal to grant would either fail obscurely or grant the
	// wrong account. Refusing is the only honest option.
	previous := currentUser
	currentUser = func() (*user.User, error) { return nil, errors.New("no passwd entry") }
	defer func() { currentUser = previous }()

	home := t.TempDir()
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("windows"),
		runtime.WithEnv(map[string]string{}),
		runtime.WithLookup(func(string) (string, bool) { return `C:\icacls.exe`, true }),
		runtime.WithRunner(func(_ context.Context, _ []string, _ runtime.RunOptions) (runtime.Result, error) {
			return runtime.Result{}, nil
		}),
	)
	if err := New(rt).EnsureDir(filepath.Join(home, "private")); err == nil {
		t.Fatal("the write should be refused when no user can be named")
	} else {
		assertCode(t, err, "CONFIG_WRITE_FAILED")
	}
}

func TestAnEmptyUsernameFromTheOSIsTreatedAsUnknown(t *testing.T) {
	previous := currentUser
	currentUser = func() (*user.User, error) { return &user.User{Username: ""}, nil }
	defer func() { currentUser = previous }()

	home := t.TempDir()
	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("windows"),
		runtime.WithEnv(map[string]string{}),
		runtime.WithLookup(func(string) (string, bool) { return `C:\icacls.exe`, true }),
		runtime.WithRunner(func(_ context.Context, _ []string, _ runtime.RunOptions) (runtime.Result, error) {
			return runtime.Result{}, nil
		}),
	)
	if err := New(rt).EnsureDir(filepath.Join(home, "private")); err == nil {
		t.Fatal("an empty username must not be granted")
	}
}

func TestTheACLCommandNeverGoesThroughAShell(t *testing.T) {
	// The path comes from a config location a user can influence, so a shell
	// string would be an injection point. argv stays a list.
	home := t.TempDir()
	fs, calls := windowsFS(t, home, nil)
	// A directory name with characters a shell would interpret.
	awkward := filepath.Join(home, "a & b; rm -rf")
	if err := fs.EnsureDir(awkward); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, call := range *calls {
		if strings.Contains(call, "cmd /c") || strings.Contains(call, "sh -c") {
			t.Errorf("the ACL call went through a shell: %s", call)
		}
	}
	// And the path arrived as one argument rather than being split.
	if _, err := os.Stat(awkward); err != nil {
		t.Errorf("the directory was not created as named: %v", err)
	}
}

func TestAFailedHardeningIsReportedRatherThanACleanupMessage(t *testing.T) {
	// The hardening failure is what the caller has to act on. The temporary file
	// is removed afterwards, and letting that cleanup replace the error would
	// bury the actual cause.
	home := t.TempDir()
	path := filepath.Join(home, "config.json")
	// Fail only for the temporary, so hardening is the single failing step.
	fs, _ := windowsFS(t, home, func(target string) bool {
		return strings.Contains(filepath.Base(target), ".oneagent-")
	})

	_, err := fs.Write(path, "{}", false)
	if err == nil {
		t.Fatal("the write should have failed")
	}
	if !strings.Contains(err.Error(), "Windows ACL") {
		t.Errorf("message = %q, want the hardening failure", err.Error())
	}
	if strings.Contains(err.Error(), "Cannot remove temporary") {
		t.Errorf("the cleanup message replaced the real cause: %q", err.Error())
	}
	assertNoTemporaries(t, home)
}

// The cleanup branch is only reachable on a failed write: a successful rename
// consumes the temporary file, so there is nothing left to remove. Python
// behaves the same way -- its finally block checks temporary.exists(), which is
// false once os.replace has succeeded. These tests therefore fail an earlier
// step first, and then check what happens when the cleanup also fails.

func TestALeftoverSecretTemporaryIsNamedAsSuchWhenItCannotBeRemoved(t *testing.T) {
	// A temporary on the secret path holds a plaintext credential. If it cannot
	// be removed, the message has to say so: an operator needs to know there is
	// a key sitting in the config directory.
	home := t.TempDir()
	// The rename never happens because hardening fails first.
	fs, _ := windowsFS(t, home, func(target string) bool {
		return strings.Contains(filepath.Base(target), ".oneagent-")
	})

	previous := removeFile
	removeFile = func(string) error { return errors.New("device busy") }
	defer func() { removeFile = previous }()

	_, err := fs.Write(filepath.Join(home, "auth.json"), `{"key":"sk-x"}`, true)
	if err == nil {
		t.Fatal("the write should have failed")
	}
	// The hardening failure is what the caller acts on, so it is the one
	// reported -- the cleanup message must not replace it.
	if !strings.Contains(err.Error(), "Windows ACL") {
		t.Errorf("message = %q, want the hardening failure", err.Error())
	}
}

func TestASuccessfulRenameLeavesNothingForTheCleanupToDo(t *testing.T) {
	// Asserted rather than assumed: if the rename ever stopped consuming the
	// temporary file, every successful write would leave one behind, and on the
	// secret path that is a plaintext credential.
	home := t.TempDir()
	fs := newFS(t, home)

	removals := 0
	previous := removeFile
	removeFile = func(path string) error {
		removals++
		return previous(path)
	}
	defer func() { removeFile = previous }()

	if _, err := fs.Write(filepath.Join(home, "auth.json"), `{"key":"sk-x"}`, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removals != 0 {
		t.Errorf("the cleanup ran %d times after a successful rename", removals)
	}
	assertNoTemporaries(t, home)
}

func TestACleanupFailureDoesNotHideAnEarlierFailure(t *testing.T) {
	// Both go wrong at once: hardening fails, and then the temporary cannot be
	// removed. The hardening error is the one the caller can act on.
	home := t.TempDir()
	fs, _ := windowsFS(t, home, func(target string) bool {
		return strings.Contains(filepath.Base(target), ".oneagent-")
	})

	previous := removeFile
	removeFile = func(string) error { return errors.New("device busy") }
	defer func() { removeFile = previous }()

	_, err := fs.Write(filepath.Join(home, "config.json"), "{}", false)
	if err == nil {
		t.Fatal("the write should have failed")
	}
	if !strings.Contains(err.Error(), "Windows ACL") {
		t.Errorf("message = %q, want the hardening failure to survive", err.Error())
	}
}

func TestABackupStillExistsOnDiskEvenWhenTheWriteFails(t *testing.T) {
	// The backup is taken before anything can go wrong, and it stays: the point
	// of it is to survive a failure, not to be rolled back with one. The user's
	// original file is also still in place, so they end up with both.
	home := t.TempDir()
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	// Hardening the temporary fails, so the rename never happens.
	fs, _ := windowsFS(t, home, func(target string) bool {
		return strings.Contains(filepath.Base(target), ".oneagent-")
	})

	if _, err := fs.Write(path, "replacement", false); err == nil {
		t.Fatal("the write should have failed")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the original file is gone: %v", err)
	}
	if string(raw) != "original" {
		t.Errorf("original content = %q, want it untouched", raw)
	}

	found := false
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("cannot read: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".backup-") {
			found = true
		}
	}
	if !found {
		t.Error("the backup was removed on failure; it exists precisely to survive one")
	}
}
