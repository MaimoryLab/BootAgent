package binding

import (
	"context"
	"os"
	"runtime"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

type TransferService struct {
	openFile func() (string, error)
	saveFile func() (string, error)
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
