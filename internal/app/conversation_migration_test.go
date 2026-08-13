package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	_ "modernc.org/sqlite"
)

func TestMigrateCodexConversationsMovesEveryProviderToBootAgent(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	sessions := filepath.Join(codexHome, "sessions", "2026", "08", "13")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessions, "rollout.jsonl")
	content := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"official","model_provider":"openai"}}`,
		`{"type":"session_meta","payload":{"id":"relay","model_provider":"custom"}}`,
		`{"type":"session_meta","payload":{"id":"ours","model_provider":"bootagent"}}`,
		`{"type":"response_item","payload":{"model_provider":"openai"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(codexHome, codexStateDB))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, model_provider TEXT NOT NULL);
		INSERT INTO threads VALUES ('official', 'openai'), ('relay', 'custom'), ('ours', 'bootagent')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	result, err := core.MigrateCodexConversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 1 || result.Threads != 2 {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, `"model_provider":"bootagent"`) != 3 {
		t.Fatalf("session providers were not migrated: %s", text)
	}
	if !strings.Contains(text, `"type":"response_item","payload":{"model_provider":"openai"}`) {
		t.Fatalf("non-session metadata was modified: %s", text)
	}
	if _, err := os.Stat(filepath.Join(home, ".bootagent", "backup")); !os.IsNotExist(err) {
		t.Fatalf("migration created a backup directory: %v", err)
	}

	db, err = sql.Open("sqlite", filepath.Join(codexHome, codexStateDB))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM threads WHERE model_provider = 'bootagent'`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("bootagent thread count = %d, err = %v", count, err)
	}
}

func TestMigrateCodexConversationsRollsBackAPartialFileCommit(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(dir, "a.jsonl"), filepath.Join(dir, "b.jsonl")}
	original := `{"type":"session_meta","payload":{"id":"one","model_provider":"openai"}}` + "\n"
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	realReplace := replaceConversationFile
	calls := 0
	replaceConversationFile = func(source, destination string) error {
		calls++
		if calls == 2 {
			return errors.New("simulated replace failure")
		}
		return realReplace(source, destination)
	}
	t.Cleanup(func() { replaceConversationFile = realReplace })

	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64")})
	if _, err := core.MigrateCodexConversations(context.Background()); err == nil {
		t.Fatal("migration succeeded after a replace failure")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != original {
			t.Fatalf("%s was not rolled back: %q, %v", path, data, err)
		}
	}
}
