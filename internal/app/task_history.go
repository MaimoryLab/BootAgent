package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const maxTaskHistoryRecords = 50

type TaskHistoryEvent struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"`
	Phase   string `json:"phase,omitempty"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message"`
}

type TaskHistoryRecord struct {
	ID             string             `json:"id"`
	Kind           string             `json:"kind"`
	Target         string             `json:"target"`
	Title          string             `json:"title"`
	Route          string             `json:"route"`
	ProgressTarget string             `json:"progressTarget"`
	State          string             `json:"state"`
	Phase          string             `json:"phase"`
	Version        string             `json:"version,omitempty"`
	Source         string             `json:"source,omitempty"`
	Message        string             `json:"message,omitempty"`
	ErrorCode      string             `json:"errorCode,omitempty"`
	ExitCode       *int               `json:"exitCode,omitempty"`
	Retryable      bool               `json:"retryable,omitempty"`
	StartedAt      int64              `json:"startedAt"`
	Log            string             `json:"log,omitempty"`
	Events         []TaskHistoryEvent `json:"events,omitempty"`
}

func (u *UseCases) taskHistoryPath() string {
	return filepath.Join(u.status.Home, ".bootagent", "task-history.json")
}

func (u *UseCases) LoadTaskHistory(ctx context.Context) ([]TaskHistoryRecord, error) {
	if u == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Task service is not configured", oneerrors.WithStatus(501))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(u.taskHistoryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []TaskHistoryRecord{}, nil
		}
		return nil, oneerrors.New(oneerrors.InternalError, "Cannot read task history", oneerrors.WithCause(err))
	}
	var records []TaskHistoryRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return []TaskHistoryRecord{}, oneerrors.New(oneerrors.InternalError, "Task history is corrupt", oneerrors.WithCause(err))
	}
	if len(records) > maxTaskHistoryRecords {
		records = records[len(records)-maxTaskHistoryRecords:]
	}
	return records, nil
}

func (u *UseCases) SaveTaskHistory(ctx context.Context, records []TaskHistoryRecord) error {
	if u == nil {
		return oneerrors.New(oneerrors.InternalError, "Task service is not configured", oneerrors.WithStatus(501))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(records) > maxTaskHistoryRecords {
		records = records[len(records)-maxTaskHistoryRecords:]
	}
	for index := range records {
		records[index].Log = truncateTaskLog(records[index].Log)
		if len(records[index].Events) > 200 {
			records[index].Events = records[index].Events[len(records[index].Events)-200:]
		}
		if len(records[index].Route) > 512 {
			records[index].Route = records[index].Route[:512]
		}
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return oneerrors.New(oneerrors.InternalError, "Cannot encode task history", oneerrors.WithCause(err))
	}
	if _, err := u.filesystem.AtomicWrite(ctx, u.taskHistoryPath(), append(data, '\n'), false); err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot save task history", oneerrors.WithCause(err))
	}
	return nil
}

func truncateTaskLog(log string) string {
	const maxBytes = 1 << 20
	if len(log) <= maxBytes {
		return log
	}
	return log[len(log)-maxBytes:]
}
