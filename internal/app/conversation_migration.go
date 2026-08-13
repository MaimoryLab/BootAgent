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
	"time"

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

type stagedConversationFile struct {
	path     string
	updated  string
	original string
	size     int64
	modified time.Time
}

var replaceConversationFile = replaceFile

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
	files, err := stageCodexSessionFiles(ctx, codexHome)
	if err != nil {
		return ConversationMigrationResult{}, migrationError(err)
	}
	defer cleanupStagedConversationFiles(files)

	dbPaths, err := codexStateDBPaths(u.status.Home, codexHome, u.environment)
	if err != nil {
		return ConversationMigrationResult{}, migrationError(err)
	}
	dbs, threads, err := beginCodexStateMigrations(ctx, dbPaths)
	if err != nil {
		return ConversationMigrationResult{}, migrationError(err)
	}
	defer closeCodexStateMigrations(dbs)

	replaced := 0
	for index := range files {
		latest, err := os.Stat(files[index].path)
		if err != nil || latest.Size() != files[index].size || !latest.ModTime().Equal(files[index].modified) {
			rollbackConversationFiles(files[:replaced])
			if err == nil {
				err = fmt.Errorf("Codex session changed while it was being migrated: %s", files[index].path)
			}
			return ConversationMigrationResult{}, migrationError(err)
		}
		if err := replaceConversationFile(files[index].updated, files[index].path); err != nil {
			rollbackConversationFiles(files[:replaced])
			return ConversationMigrationResult{}, migrationError(err)
		}
		replaced++
	}
	for index, db := range dbs {
		if err := db.tx.Commit(); err != nil {
			rollbackConversationFiles(files)
			rollbackCommittedCodexState(dbs[:index])
			return ConversationMigrationResult{}, migrationError(err)
		}
		db.committed = true
	}
	return ConversationMigrationResult{Files: len(files), Threads: threads}, nil
}

func stageCodexSessionFiles(ctx context.Context, codexHome string) ([]stagedConversationFile, error) {
	var files []stagedConversationFile
	for _, root := range []string{"sessions", "archived_sessions"} {
		err := filepath.WalkDir(filepath.Join(codexHome, root), func(path string, entry os.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return filepath.SkipDir
			}
			if err != nil || entry.IsDir() || filepath.Ext(path) != ".jsonl" {
				return err
			}
			staged, changed, err := stageCodexSessionFile(ctx, path)
			if changed {
				files = append(files, staged)
			}
			return err
		})
		if err != nil {
			cleanupStagedConversationFiles(files)
			return nil, err
		}
	}
	return files, nil
}

func stageCodexSessionFile(ctx context.Context, path string) (stagedConversationFile, bool, error) {
	input, err := os.Open(path)
	if err != nil {
		return stagedConversationFile{}, false, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return stagedConversationFile{}, false, err
	}
	updated, err := os.CreateTemp(filepath.Dir(path), ".bootagent-conversations-new-")
	if err != nil {
		return stagedConversationFile{}, false, err
	}
	staged := stagedConversationFile{path: path, updated: updated.Name(), size: info.Size(), modified: info.ModTime()}
	defer func() {
		_ = updated.Close()
	}()

	changed := false
	reader := bufio.NewReader(input)
	for {
		if err := ctx.Err(); err != nil {
			os.Remove(staged.updated)
			return stagedConversationFile{}, false, err
		}
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			ending, content := "", line
			if trimmed, ok := strings.CutSuffix(content, "\n"); ok {
				ending, content = "\n", trimmed
			}
			if next, ok := migrateCodexSessionMeta(content); ok {
				content, changed = next, true
			}
			if _, err := io.WriteString(updated, content+ending); err != nil {
				os.Remove(staged.updated)
				return stagedConversationFile{}, false, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			os.Remove(staged.updated)
			return stagedConversationFile{}, false, readErr
		}
	}
	if !changed {
		os.Remove(staged.updated)
		return stagedConversationFile{}, false, nil
	}
	if err := updated.Chmod(info.Mode().Perm()); err != nil {
		os.Remove(staged.updated)
		return stagedConversationFile{}, false, err
	}
	if err := updated.Sync(); err != nil {
		os.Remove(staged.updated)
		return stagedConversationFile{}, false, err
	}
	if err := updated.Close(); err != nil {
		os.Remove(staged.updated)
		return stagedConversationFile{}, false, err
	}
	if latest, err := os.Stat(path); err != nil || latest.Size() != info.Size() || !latest.ModTime().Equal(info.ModTime()) {
		os.Remove(staged.updated)
		return stagedConversationFile{}, false, fmt.Errorf("Codex session changed while it was being migrated: %s", path)
	}
	original, err := copyConversationFile(path, info.Mode().Perm())
	if err != nil {
		os.Remove(staged.updated)
		return stagedConversationFile{}, false, err
	}
	staged.original = original
	return staged, true, nil
}

func copyConversationFile(path string, mode os.FileMode) (string, error) {
	source, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	temporary, err := os.CreateTemp(filepath.Dir(path), ".bootagent-conversations-old-")
	if err != nil {
		return "", err
	}
	name := temporary.Name()
	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		os.Remove(name)
		return "", err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		os.Remove(name)
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		os.Remove(name)
		return "", err
	}
	return name, temporary.Close()
}

func rollbackConversationFiles(files []stagedConversationFile) {
	for index := range slices.Backward(files) {
		_ = replaceConversationFile(files[index].original, files[index].path)
	}
}

func cleanupStagedConversationFiles(files []stagedConversationFile) {
	for _, file := range files {
		_ = os.Remove(file.updated)
		_ = os.Remove(file.original)
	}
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

type codexStateMigration struct {
	db        *sql.DB
	tx        *sql.Tx
	originals map[string]string
	committed bool
}

func beginCodexStateMigrations(ctx context.Context, paths []string) ([]*codexStateMigration, int, error) {
	var migrations []*codexStateMigration
	total := 0
	for _, path := range paths {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		db.SetMaxOpenConns(1)
		if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
			db.Close()
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			db.Close()
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		migration := &codexStateMigration{db: db, tx: tx, originals: map[string]string{}}
		migrations = append(migrations, migration)
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('threads') WHERE name = 'model_provider'").Scan(&exists); err != nil {
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		if exists == 0 {
			continue
		}
		rows, err := tx.QueryContext(ctx, "SELECT id, model_provider FROM threads WHERE model_provider <> ?", codexBootAgentProvider)
		if err != nil {
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		for rows.Next() {
			var id, provider string
			if err := rows.Scan(&id, &provider); err != nil {
				rows.Close()
				closeCodexStateMigrations(migrations)
				return nil, 0, err
			}
			migration.originals[id] = provider
		}
		if err := rows.Close(); err != nil {
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		result, err := tx.ExecContext(ctx, "UPDATE threads SET model_provider = ? WHERE model_provider <> ?", codexBootAgentProvider, codexBootAgentProvider)
		if err != nil {
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			closeCodexStateMigrations(migrations)
			return nil, 0, err
		}
		total += int(changed)
	}
	return migrations, total, nil
}

func closeCodexStateMigrations(migrations []*codexStateMigration) {
	for _, migration := range migrations {
		if !migration.committed {
			_ = migration.tx.Rollback()
		}
		_ = migration.db.Close()
	}
}

func rollbackCommittedCodexState(migrations []*codexStateMigration) {
	for _, migration := range migrations {
		tx, err := migration.db.Begin()
		if err != nil {
			continue
		}
		ok := true
		for id, provider := range migration.originals {
			if _, err := tx.Exec("UPDATE threads SET model_provider = ? WHERE id = ? AND model_provider = ?", provider, id, codexBootAgentProvider); err != nil {
				ok = false
				break
			}
		}
		if ok {
			_ = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
}

func codexStateDBPaths(home, codexHome string, environment map[string]string) ([]string, error) {
	config, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	dbHomes := []string{codexHome}
	var settings struct {
		SQLiteHome string `toml:"sqlite_home"`
	}
	if toml.Unmarshal(config, &settings) == nil && strings.TrimSpace(settings.SQLiteHome) != "" {
		dbHomes = appendUnique(dbHomes, resolveCodexPath(home, settings.SQLiteHome))
	} else if configured := strings.TrimSpace(environment["CODEX_SQLITE_HOME"]); configured != "" {
		dbHomes = appendUnique(dbHomes, resolveCodexPath(home, configured))
	}
	paths := make([]string, 0, len(dbHomes))
	for _, dbHome := range dbHomes {
		paths = append(paths, filepath.Join(dbHome, codexStateDB))
	}
	return paths, nil
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
