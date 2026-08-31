package binding

import (
	"context"
	"encoding/base64"
	"os"
	"runtime"

	"github.com/MaimoryLab/BootAgent/internal/app"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

type TransferService struct {
	core     *app.UseCases
	openFile func() (string, error)
	saveFile func() (string, error)
}

func (s *TransferService) ExportV2(ctx context.Context, providerIDs, profileIDs, mcpIDs, skillIDs []string) ([]byte, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("Transfer service is not configured")
	}
	return s.core.ExportTransferV2(ctx, providerIDs, profileIDs, mcpIDs, skillIDs)
}

func (s *TransferService) selectImport() (string, error) {
	if s.openFile != nil {
		return s.openFile()
	}
	return selectImportFile()
}

func (s *TransferService) selectExport() (string, error) {
	if s.saveFile != nil {
		return s.saveFile()
	}
	return selectExportFile()
}

func (s *TransferService) Read(ctx context.Context) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	path, err := s.selectImport()
	if err != nil || path == "" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "file selection cancelled")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Cannot read import file", oneerrors.WithCause(err))
	}
	return string(data), nil
}

func (s *TransferService) Write(ctx context.Context, data string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	path, err := s.selectExport()
	if err != nil || path == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "file selection cancelled")
	}
	filesystem := securefs.New(securefs.Options{OS: runtime.GOOS})
	if _, err := filesystem.AtomicWrite(ctx, path, []byte(data), false); err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot write export file", oneerrors.WithCause(err))
	}
	return nil
}

func (s *TransferService) WriteBytes(ctx context.Context, encoded string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	path, err := s.selectExport()
	if err != nil || path == "" {
		return oneerrors.New(oneerrors.InvalidRequest, "file selection cancelled")
	}
	filesystem := securefs.New(securefs.Options{OS: runtime.GOOS})
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return oneerrors.New(oneerrors.InvalidRequest, "invalid export payload", oneerrors.WithCause(err))
	}
	if _, err := filesystem.AtomicWrite(ctx, path, data, false); err != nil {
		return oneerrors.New(oneerrors.ConfigWriteFailed, "Cannot write export file", oneerrors.WithCause(err))
	}
	return nil
}
