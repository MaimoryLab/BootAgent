// Package securefs contains the small filesystem primitives used by profile
// and configuration writers. It deliberately does not know about any file
// format or credential name.
package securefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

// CommandRunner is injectable so Windows ACL behavior can be tested without
// invoking a host command. argv[0] is the executable name.
type CommandRunner func(context.Context, []string) error

// SecurePathFunc is an optional security hook used by fault-injection tests.
// The default implementation applies Unix modes or Windows ACLs.
type SecurePathFunc func(path string, directory bool) error

type Options struct {
	OS         string
	Username   string
	Now        func() time.Time
	Run        CommandRunner
	Secure     SecurePathFunc
	BackupRoot string
	Retention  func() int
}

type Store struct {
	os            string
	username      string
	now           func() time.Time
	commandRunner CommandRunner
	secure        SecurePathFunc
	backupRoot    string
	retention     func() int
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
		backupRoot:    options.BackupRoot,
		retention:     options.Retention,
	}
}

// BackupRoot is the private root used for managed backups. An empty value
// means this Store keeps the legacy beside-file behavior for standalone users.
func (s Store) BackupRoot() string { return s.backupRoot }

// BackupRetention returns the effective per-target history limit.
func (s Store) BackupRetention() int {
	retention := 3
	if s.retention != nil {
		retention = s.retention()
	}
	if retention < 1 {
		return 3
	}
	if retention > 100 {
		return 100
	}
	return retention
}

// BackupGroupPath returns a readable, escaped directory for one target.
func BackupGroupPath(root, target string) string {
	if root == "" {
		return ""
	}
	file := filepath.Base(target)
	agent := strings.TrimPrefix(filepath.Base(filepath.Dir(target)), ".")
	if strings.HasPrefix(file, ".") || agent == "config" {
		agent = strings.SplitN(strings.TrimPrefix(file, "."), ".", 2)[0]
	}
	return filepath.Join(root, "files", url.PathEscape(agent)+"-"+url.PathEscape(file))
}

// LegacyBackupGroupPath returns the pre-readable-name SHA-256 group path.
func LegacyBackupGroupPath(root, target string) string {
	absolute, err := filepath.Abs(target)
	if err != nil {
		absolute = filepath.Clean(target)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(absolute)))
	return filepath.Join(root, "files", hex.EncodeToString(sum[:]))
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

// Backup copies an existing file into the managed target group. A missing
// source is not an error and returns an empty path. Callers decide whether the
// backup is a secret and therefore needs an additional permission check.
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
	managed := s.backupRoot != ""
	nameBase := path
	if managed {
		nameBase = BackupGroupPath(s.backupRoot, path)
		if err := s.ensureManagedBackupGroup(ctx, nameBase); err != nil {
			return "", err
		}
		if err := s.migrateHashedBackupGroup(ctx, path, nameBase); err != nil {
			return "", err
		}
	}
	candidate := s.backupName(nameBase, managed)
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

	temporary, err := os.CreateTemp(parent, ".bootagent-tmp-")
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
	if err = s.cleanupBackups(ctx, path, secret); err != nil {
		return backup, err
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

func (s Store) backupName(directory string, managed bool) string {
	now := s.now
	if now == nil {
		now = time.Now
	}
	stamp := now().UTC().Format("20060102150405")
	base := ""
	if managed {
		base = filepath.Join(directory, "backup-"+stamp)
	} else {
		base = directory + ".backup-" + stamp
	}
	candidate := base
	for counter := 1; ; counter++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, counter)
	}
}

func (s Store) cleanupBackups(ctx context.Context, path string, secret bool) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if s.backupRoot == "" {
		return s.pruneLegacyBackups(path)
	}
	group := BackupGroupPath(s.backupRoot, path)
	legacy, err := filepath.Glob(path + ".backup-*")
	if err != nil {
		return writeError("Cannot find legacy backups for %s: %v", path, err)
	}
	if _, statErr := os.Stat(group); os.IsNotExist(statErr) && len(legacy) == 0 {
		return nil
	}
	if err := s.ensureManagedBackupGroup(ctx, group); err != nil {
		return err
	}
	if err := s.migrateLegacyBackups(ctx, path, group, secret); err != nil {
		return err
	}
	entries, err := os.ReadDir(group)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return writeError("Cannot list backups for %s: %v", path, err)
	}
	type backupEntry struct {
		name string
		info os.FileInfo
	}
	backups := make([]backupEntry, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "backup-") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return writeError("Cannot inspect backup %s: %v", entry.Name(), infoErr)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		backups = append(backups, backupEntry{name: entry.Name(), info: info})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].info.ModTime().Equal(backups[j].info.ModTime()) {
			return backups[i].name > backups[j].name
		}
		return backups[i].info.ModTime().After(backups[j].info.ModTime())
	})
	retention := s.BackupRetention()
	if len(backups) <= retention {
		return nil
	}
	for _, old := range backups[retention:] {
		if err := os.Remove(filepath.Join(group, old.name)); err != nil && !os.IsNotExist(err) {
			return writeError("Cannot remove old backup %s: %v", old.name, err)
		}
	}
	return nil
}

func (s Store) ensureManagedBackupGroup(ctx context.Context, group string) error {
	if s.backupRoot == "" {
		return writeError("Managed backup root is not configured")
	}
	for _, path := range []string{s.backupRoot, filepath.Join(s.backupRoot, "files"), group} {
		if err := rejectManagedComponents(s.backupRoot, path); err != nil {
			return err
		}
		if err := s.EnsurePrivateDir(ctx, path); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) migrateHashedBackupGroup(ctx context.Context, target, group string) error {
	legacy := LegacyBackupGroupPath(s.backupRoot, target)
	if err := rejectManagedComponents(s.backupRoot, legacy); err != nil {
		return err
	}
	entries, err := os.ReadDir(legacy)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return writeError("Cannot list legacy backup group %s: %v", legacy, err)
	}
	for _, entry := range entries {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), "backup-") {
			continue
		}
		source := filepath.Join(legacy, entry.Name())
		destination := filepath.Join(group, entry.Name())
		for counter := 1; ; counter++ {
			if _, statErr := os.Lstat(destination); os.IsNotExist(statErr) {
				break
			} else if statErr != nil {
				return writeError("Cannot inspect backup destination %s: %v", destination, statErr)
			}
			destination = fmt.Sprintf("%s-%d", filepath.Join(group, entry.Name()), counter)
		}
		if err := os.Rename(source, destination); err != nil {
			return writeError("Cannot migrate legacy backup %s: %v", source, err)
		}
	}
	if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
		return writeError("Cannot remove legacy backup group %s: %v", legacy, err)
	}
	return nil
}

func rejectManagedComponents(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return writeError("Managed backup path escapes its root: %s", path)
	}
	current := root
	if info, statErr := os.Lstat(current); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return writeError("Managed backup path contains a symlink: %s", current)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return writeError("Cannot inspect managed backup path %s: %v", current, statErr)
	}
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return writeError("Managed backup path contains a symlink: %s", current)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return writeError("Cannot inspect managed backup path %s: %v", current, statErr)
		}
	}
	return nil
}

func (s Store) migrateLegacyBackups(ctx context.Context, path, group string, secret bool) error {
	matches, err := filepath.Glob(path + ".backup-*")
	if err != nil {
		return writeError("Cannot find legacy backups for %s: %v", path, err)
	}
	type legacyBackup struct {
		path string
		info os.FileInfo
	}
	backups := make([]legacyBackup, 0, len(matches))
	for _, source := range matches {
		info, statErr := os.Lstat(source)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return writeError("Cannot inspect legacy backup %s: %v", source, statErr)
		}
		if info.Mode().IsRegular() {
			backups = append(backups, legacyBackup{path: source, info: info})
		}
	}
	sort.Slice(backups, func(i, j int) bool {
		if !backups[i].info.ModTime().Equal(backups[j].info.ModTime()) {
			return backups[i].info.ModTime().Before(backups[j].info.ModTime())
		}
		return backups[i].path < backups[j].path
	})
	keepFrom := 0
	if excess := len(backups) - s.BackupRetention(); excess > 0 {
		keepFrom = excess
	}
	for _, backup := range backups[keepFrom:] {
		if err := checkContext(ctx); err != nil {
			return err
		}
		candidate := s.backupName(group, true)
		if err := copyFile(backup.path, candidate, backup.info.Mode().Perm()); err != nil {
			return writeError("Cannot migrate legacy backup %s: %v", backup.path, err)
		}
		if err := os.Chtimes(candidate, backup.info.ModTime(), backup.info.ModTime()); err != nil {
			_ = os.Remove(candidate)
			return writeError("Cannot preserve legacy backup time %s: %v", backup.path, err)
		}
		if secret {
			if err := s.SecureFile(ctx, candidate); err != nil {
				_ = os.Remove(candidate)
				return err
			}
		}
		if err := os.Remove(backup.path); err != nil && !os.IsNotExist(err) {
			return writeError("Cannot remove legacy backup %s: %v", backup.path, err)
		}
	}
	for _, old := range backups[:keepFrom] {
		if err := os.Remove(old.path); err != nil && !os.IsNotExist(err) {
			return writeError("Cannot remove old legacy backup %s: %v", old.path, err)
		}
	}
	return nil
}

func (s Store) pruneLegacyBackups(path string) error {
	matches, err := filepath.Glob(path + ".backup-*")
	if err != nil {
		return writeError("Cannot find backups for %s: %v", path, err)
	}
	regular := matches[:0]
	for _, match := range matches {
		info, statErr := os.Lstat(match)
		if statErr == nil && info.Mode().IsRegular() {
			regular = append(regular, match)
		}
	}
	if len(regular) <= s.BackupRetention() {
		return nil
	}
	sort.Strings(regular)
	for _, old := range regular[:len(regular)-s.BackupRetention()] {
		if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
			return writeError("Cannot remove old backup %s: %v", old, err)
		}
	}
	return nil
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
