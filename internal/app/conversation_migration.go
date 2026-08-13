package app

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/pelletier/go-toml/v2"
	_ "modernc.org/sqlite"
)

const (
	codexBootAgentProvider = "bootagent"
	codexStateDB           = "state_5.sqlite"
)

type ConversationMigrationResult struct {
	Files   int `json:"files"`
	Threads int `json:"threads"`
}

// MigrateCodexConversations moves Codex and ChatGPT Desktop history into the
// provider bucket written by BootAgent. Both clients share CODEX_HOME.
func (u *UseCases) MigrateCodexConversations(ctx context.Context) (ConversationMigrationResult, error) {
	if u == nil {
		return ConversationMigrationResult{}, oneerrors.New(oneerrors.InternalError, "Conversation migration is not configured", oneerrors.WithStatus(501))
	}
	if err := ctx.Err(); err != nil {
		return ConversationMigrationResult{}, err
	}

	u.writeMu.Lock()
	defer u.writeMu.Unlock()

	codexHome := filepath.Join(u.status.Home, ".codex")
	if configured := strings.TrimSpace(u.environment["CODEX_HOME"]); configured != "" {
		codexHome = resolveCodexPath(u.status.Home, configured)
	}

	result := ConversationMigrationResult{}
	for _, root := range []string{"sessions", "archived_sessions"} {
		err := filepath.WalkDir(filepath.Join(codexHome, root), func(path string, entry os.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return filepath.SkipDir
			}
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			changed, err := migrateCodexSessionFile(ctx, path)
			if changed {
				result.Files++
			}
			return err
		})
		if err != nil {
			return ConversationMigrationResult{}, migrationError(err)
		}
	}

	config, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ConversationMigrationResult{}, migrationError(err)
	}
	dbHomes := []string{codexHome}
	var settings struct {
		SQLiteHome string `toml:"sqlite_home"`
	}
	if toml.Unmarshal(config, &settings) == nil && strings.TrimSpace(settings.SQLiteHome) != "" {
		dbHomes = appendUnique(dbHomes, resolveCodexPath(u.status.Home, settings.SQLiteHome))
	} else if configured := strings.TrimSpace(u.environment["CODEX_SQLITE_HOME"]); configured != "" {
		dbHomes = appendUnique(dbHomes, resolveCodexPath(u.status.Home, configured))
	}
	for _, home := range dbHomes {
		changed, err := migrateCodexStateDB(ctx, filepath.Join(home, codexStateDB))
		if err != nil {
			return ConversationMigrationResult{}, migrationError(err)
		}
		result.Threads += changed
	}
	return result, nil
}

func migrateCodexSessionFile(ctx context.Context, path string) (bool, error) {
	input, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer input.Close()

	info, err := input.Stat()
	if err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".bootagent-conversations-")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	changed := false
	reader := bufio.NewReader(input)
	for {
		if err := ctx.Err(); err != nil {
			_ = temporary.Close()
			return false, err
		}
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			ending := ""
			content := line
			if strings.HasSuffix(content, "\n") {
				ending = "\n"
				content = strings.TrimSuffix(content, "\n")
			}
			if next, ok := migrateCodexSessionMeta(content); ok {
				content, changed = next, true
			}
			if _, err := io.WriteString(temporary, content+ending); err != nil {
				_ = temporary.Close()
				return false, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = temporary.Close()
			return false, readErr
		}
	}
	if !changed {
		return false, temporary.Close()
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		_ = temporary.Close()
		return false, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if latest, err := os.Stat(path); err != nil || latest.Size() != info.Size() || !latest.ModTime().Equal(info.ModTime()) {
		return false, fmt.Errorf("Codex session changed while it was being migrated: %s", path)
	}
	return true, replaceFile(temporaryPath, path)
}

func migrateCodexSessionMeta(line string) (string, bool) {
	if !strings.Contains(line, `"session_meta"`) || !strings.Contains(line, `"model_provider"`) {
		return "", false
	}
	var record map[string]any
	if json.Unmarshal([]byte(line), &record) != nil || record["type"] != "session_meta" {
		return "", false
	}
	payload, ok := record["payload"].(map[string]any)
	if !ok {
		return "", false
	}
	provider, ok := payload["model_provider"].(string)
	if !ok || provider == "" || provider == codexBootAgentProvider {
		return "", false
	}
	payload["model_provider"] = codexBootAgentProvider
	data, err := json.Marshal(record)
	return string(data), err == nil
}

func migrateCodexStateDB(ctx context.Context, path string) (int, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return 0, err
	}
	var exists int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('threads') WHERE name = 'model_provider'").Scan(&exists); err != nil || exists == 0 {
		return 0, err
	}
	result, err := db.ExecContext(ctx, "UPDATE threads SET model_provider = ? WHERE model_provider <> ?", codexBootAgentProvider, codexBootAgentProvider)
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	return int(changed), err
}

func resolveCodexPath(home, path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(home, filepath.FromSlash(rest))
	}
	return path
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func migrationError(err error) error {
	return oneerrors.New(oneerrors.ConfigWriteFailed, "Unable to migrate Codex conversations", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
}
