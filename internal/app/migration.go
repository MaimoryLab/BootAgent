package app

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	configMigration "github.com/MaimoryLab/BootAgent/internal/config"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

const (
	legacyHomeName  = ".oneagent"
	currentHomeName = ".bootagent"
)

// migrateLegacyHome moves configuration only. Managed runtimes and installed
// Agent packages live under legacy/runtimes and are not copied. The agents
// directory contains only the Agent-to-Profile binding files.
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
	entries := []string{"mcp.json", "skill-registry.json", "skills", "providers.json", "profile.json", "profiles", "secrets", "agents", "aider.env"}
	for _, name := range entries {
		source := filepath.Join(legacy, name)
		if _, err := os.Stat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("inspect legacy %s: %w", name, err)
		}
		if err := copyMigrationEntry(source, filepath.Join(current, name)); err != nil {
			return "", fmt.Errorf("migrate legacy %s: %w", name, err)
		}
	}
	filesystem := securefs.New(securefs.Options{OS: osID, BackupRoot: filepath.Join(home, currentHomeName, "backup")})
	if err := configMigration.MigrateLegacyAgentConfigs(context.Background(), home, filesystem); err != nil {
		return "", fmt.Errorf("migrate Agent configurations: %w", err)
	}
	if err := os.RemoveAll(legacy); err != nil {
		return "", fmt.Errorf("remove legacy configuration: %w", err)
	}
	return "OneAgent configuration was migrated to BootAgent. Node.js, uv, and installed Agents were not migrated; install them again from BootAgent.", nil
}

func copyMigrationEntry(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			destination := target
			if rel != "." {
				destination = filepath.Join(target, rel)
			}
			if entry.IsDir() {
				return os.MkdirAll(destination, 0o700)
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink is not supported: %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(destination, data, 0o600)
		})
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink is not supported: %s", source)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o600)
}
