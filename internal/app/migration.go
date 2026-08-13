package app

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	configMigration "github.com/MaimoryLab/BootAgent/internal/config"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

const (
	legacyHomeName  = ".oneagent"
	currentHomeName = ".bootagent"
)

// migrateLegacyHome copies the legacy home except for managed runtimes. The
// original tree is retained under a timestamped name until the user removes it.
func migrateLegacyHome(home, osID string) (string, error) {
	legacy := filepath.Join(home, legacyHomeName)
	current := filepath.Join(home, currentHomeName)
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		filesystem := securefs.New(securefs.Options{OS: osID, BackupRoot: filepath.Join(home, currentHomeName, "backup")})
		return "", configMigration.MigrateLegacyAgentConfigs(context.Background(), home, filesystem)
	} else if err != nil {
		return "", fmt.Errorf("inspect legacy configuration: %w", err)
	}
	if err := os.MkdirAll(current, 0o700); err != nil {
		return "", fmt.Errorf("create BootAgent configuration directory: %w", err)
	}
	if err := copyMigrationTree(legacy, current, "runtimes"); err != nil {
		return "", fmt.Errorf("migrate legacy configuration: %w", err)
	}
	filesystem := securefs.New(securefs.Options{OS: osID, BackupRoot: filepath.Join(home, currentHomeName, "backup")})
	if err := configMigration.MigrateLegacyAgentConfigs(context.Background(), home, filesystem); err != nil {
		return "", fmt.Errorf("migrate Agent configurations: %w", err)
	}
	retained := filepath.Join(home, legacyHomeName+"-migrated-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Rename(legacy, retained); err != nil {
		return "", fmt.Errorf("retain legacy configuration: %w", err)
	}
	return fmt.Sprintf("OneAgent configuration was migrated to BootAgent. Node.js, uv, and installed Agents were not migrated; install them again from BootAgent. The original configuration was retained at %s for recovery; delete it after verifying the migration.", retained), nil
}

func copyMigrationTree(source, target, excludedTopLevel string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == excludedTopLevel || strings.HasPrefix(rel, excludedTopLevel+string(filepath.Separator)) {
			if rel == excludedTopLevel {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			if info, err := os.Stat(destination); err == nil && !info.IsDir() {
				return fmt.Errorf("migration destination is not a directory: %s", destination)
			} else if err != nil && !os.IsNotExist(err) {
				return err
			}
			return os.MkdirAll(destination, 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not supported: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if info, err := os.Lstat(destination); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
				return fmt.Errorf("migration destination is not a regular file: %s", destination)
			}
			existing, err := os.ReadFile(destination)
			if err != nil {
				return err
			}
			if bytes.Equal(existing, data) {
				return nil
			}
			return fmt.Errorf("migration destination already contains different data: %s", destination)
		} else if !os.IsNotExist(err) {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	})
}
