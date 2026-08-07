package binding

import (
	"context"
	"os"
	"strings"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

type TransferService struct{}

type FilePathRequest struct {
	Path string `json:"path"`
}

type WriteFileRequest struct {
	Path string `json:"path"`
	Data string `json:"data"`
}

func (s *TransferService) Read(ctx context.Context, request FilePathRequest) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	path := strings.TrimSpace(request.Path)
	if path == "" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Cannot read import file", oneerrors.WithCause(err))
	}
	return string(data), nil
}

func (s *TransferService) Write(ctx context.Context, request WriteFileRequest) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	path := strings.TrimSpace(request.Path)
	if path == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "file path is required")
	}
	if err := os.WriteFile(path, []byte(request.Data), 0o600); err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot write export file", oneerrors.WithCause(err))
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot secure export file", oneerrors.WithCause(err))
	}
	return nil
}
