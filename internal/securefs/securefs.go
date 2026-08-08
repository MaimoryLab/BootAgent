// Package securefs contains the small filesystem primitives used by profile
// and configuration writers. It deliberately does not know about any file
// format or credential name.
package securefs

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

// CommandRunner is injectable so Windows ACL behavior can be tested without
// invoking a host command. argv[0] is the executable name.
type CommandRunner func(context.Context, []string) error

// SecurePathFunc is an optional security hook used by fault-injection tests.
// The default implementation applies Unix modes or Windows ACLs.
type SecurePathFunc func(path string, directory bool) error

type Options struct {
	OS       string
	Username string
	Now      func() time.Time
	Run      CommandRunner
	Secure   SecurePathFunc
}

type Store struct {
	os            string
	username      string
	now           func() time.Time
	commandRunner CommandRunner
	secure        SecurePathFunc
}

func New(options Options) Store {
	platformID := options.OS
	if platformID == "" {
		platformID = runtime.GOOS
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Run == nil {
		options.Run = runCommand
	}
	return Store{
		os:            platformID,
		username:      options.Username,
		now:           options.Now,
		commandRunner: options.Run,
		secure:        options.Secure,
	}
}

func (s Store) EnsurePrivateDir(ctx context.Context, path string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if path == "" {
		return writeError("Cannot secure an empty directory path")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return writeError("Cannot create private directory %s: %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return writeError("Cannot inspect private directory %s: %v", path, err)
	}
	if !info.IsDir() {
		return writeError("Private path %s is not a directory", path)
	}
	if err := s.securePath(ctx, path, true); err != nil {
		return err
	}
	return nil
}

func (s Store) SecureFile(ctx context.Context, path string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if path == "" {
		return writeError("Cannot secure an empty file path")
	}
	if err := s.securePath(ctx, path, false); err != nil {
		return err
	}
	return nil
}

// Backup copies an existing file beside itself. A missing source is not an
// error and returns an empty path. Callers decide whether the backup is a
// secret and therefore needs an additional permission check.
func (s Store) Backup(ctx context.Context, path string) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", writeError("Cannot inspect %s before backup: %v", path, err)
	}
	if info.IsDir() {
		return "", writeError("Cannot back up directory %s", path)
	}
	candidate := s.backupName(path)
	if err := copyFile(path, candidate, info.Mode().Perm()); err != nil {
		return "", writeError("Cannot back up %s: %v", path, err)
	}
	return candidate, nil
}

// AtomicWrite publishes content only after the temporary file has been
// written, synced, and secured. The temporary inode always lives beside the
// target so a successful rename cannot cross filesystems.
func (s Store) AtomicWrite(ctx context.Context, path string, content []byte, secret bool) (backup string, err error) {
	if err = checkContext(ctx); err != nil {
		return "", err
	}
	parent := filepath.Dir(path)
	if err = s.EnsurePrivateDir(ctx, parent); err != nil {
		return "", err
	}
	backup, err = s.Backup(ctx, path)
	if err != nil {
		return "", err
	}
	if backup != "" && secret {
		if err = s.SecureFile(ctx, backup); err != nil {
			if removeErr := os.Remove(backup); removeErr != nil {
				return "", writeError("Cannot remove insecure secret backup %s: %v", backup, removeErr)
			}
			return "", err
		}
	}

	temporary, err := os.CreateTemp(parent, ".oneagent-tmp-")
	if err != nil {
		return backup, writeError("Cannot create temporary file for %s: %v", path, err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() error {
		_ = temporary.Close()
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return writeError("Cannot remove temporary %sfile %s: %v", secretWord(secret), temporaryPath, removeErr)
		}
		return nil
	}
	defer func() {
		if cleanupErr := cleanup(); err == nil && cleanupErr != nil {
			err = cleanupErr
		}
	}()

	if _, err = temporary.Write(content); err != nil {
		return backup, writeError("Cannot write temporary file for %s: %v", path, err)
	}
	if err = temporary.Sync(); err != nil {
		return backup, writeError("Cannot flush temporary file for %s: %v", path, err)
	}
	if err = temporary.Close(); err != nil {
		return backup, writeError("Cannot close temporary file for %s: %v", path, err)
	}
	if err = s.SecureFile(ctx, temporaryPath); err != nil {
		return backup, err
	}
	if err = checkContext(ctx); err != nil {
		return backup, err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return backup, writeError("Cannot replace %s: %v", path, err)
	}
	return backup, nil
}

func (s Store) securePath(ctx context.Context, path string, directory bool) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if s.secure != nil {
		if err := s.secure(path, directory); err != nil {
			return err
		}
		return nil
	}
	if s.os != "windows" {
		mode := os.FileMode(0o600)
		if directory {
			mode = 0o700
		}
		if err := os.Chmod(path, mode); err != nil {
			return writeError("Cannot secure %s: %v", path, err)
		}
		return nil
	}
	username := s.username
	if username == "" {
		username = os.Getenv("USERNAME")
	}
	if username == "" {
		if current, err := user.Current(); err == nil {
			username = current.Username
		}
	}
	if username == "" {
		return writeError("Cannot secure %s: Windows username is unavailable", path)
	}
	grants := username + ":F"
	system := "*S-1-5-18:F"
	if directory {
		grants = username + ":(OI)(CI)F"
		system = "*S-1-5-18:(OI)(CI)F"
	}
	for _, argv := range [][]string{
		{"icacls", path, "/reset"},
		{"icacls", path, "/inheritance:r", "/grant:r", grants, system},
	} {
		if err := s.execute(ctx, argv); err != nil {
			if ctx.Err() != nil {
				return checkContext(ctx)
			}
			return writeError("Failed to secure Windows ACL for %s", path)
		}
	}
	return nil
}

func (s Store) backupName(path string) string {
	now := s.now
	if now == nil {
		now = time.Now
	}
	stamp := now().UTC().Format("20060102150405")
	base := path + ".backup-" + stamp
	candidate := base
	for counter := 1; ; counter++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, counter)
	}
}

func (s Store) execute(ctx context.Context, argv []string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if s.commandRunner == nil {
		return runCommand(ctx, argv)
	}
	return s.commandRunner(ctx, argv)
}

func runCommand(ctx context.Context, argv []string) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return fmt.Errorf("empty command")
	}
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	process.HideWindow(command)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	closeOutput := func() error {
		if closeErr := output.Close(); closeErr != nil {
			return closeErr
		}
		return nil
	}
	if _, err = io.Copy(output, input); err != nil {
		_ = output.Close()
		_ = os.Remove(destination)
		return err
	}
	if err = output.Sync(); err != nil {
		_ = output.Close()
		_ = os.Remove(destination)
		return err
	}
	if err = os.Chmod(destination, mode); err != nil {
		_ = output.Close()
		_ = os.Remove(destination)
		return err
	}
	return closeOutput()
}

func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return oneerrors.New(oneerrors.Timeout, "Filesystem request was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return nil
}

// Retryable because every failure that reaches here is a condition outside the
// process: permission or ownership on the target, a full disk, a path held open
// by the Agent itself, an unavailable Windows account. The user fixes those and
// the same write succeeds -- so this is exactly the case that needs a retry
// button, and it had none, because Retryable defaults to false.
func writeError(format string, values ...any) error {
	return oneerrors.New(oneerrors.ConfigWriteFailed, fmt.Sprintf(format, values...), oneerrors.WithRetryable(true))
}

func secretWord(secret bool) string {
	if secret {
		return "secret "
	}
	return ""
}
