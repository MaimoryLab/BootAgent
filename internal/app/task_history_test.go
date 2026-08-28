package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskHistoryRoundTripTrimsAndLimits(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home})
	records := make([]TaskHistoryRecord, maxTaskHistoryRecords+3)
	for index := range records {
		records[index] = TaskHistoryRecord{ID: string(rune('a' + index%26)), Target: "agent", Kind: "install", Title: "Install", Route: strings.Repeat("/", 600), Log: strings.Repeat("x", (1<<20)+10)}
	}
	if err := core.SaveTaskHistory(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadTaskHistory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != maxTaskHistoryRecords {
		t.Fatalf("loaded %d records, want %d", len(loaded), maxTaskHistoryRecords)
	}
	if len(loaded[0].Route) != 512 || len(loaded[0].Log) != 1<<20 {
		t.Fatalf("record limits not applied: route=%d log=%d", len(loaded[0].Route), len(loaded[0].Log))
	}
	if _, err := os.Stat(filepath.Join(home, ".bootagent", "task-history.json")); err != nil {
		t.Fatalf("history file missing: %v", err)
	}
}

func TestTaskHistoryCorruptFileDoesNotBecomeSuccess(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home})
	path := filepath.Join(home, ".bootagent", "task-history.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := core.LoadTaskHistory(context.Background()); err == nil {
		t.Fatal("corrupt history unexpectedly loaded")
	}
}
