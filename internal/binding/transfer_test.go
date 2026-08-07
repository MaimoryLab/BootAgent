package binding

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTransferServiceWritesAndReadsSelectedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	service := &TransferService{}
	if err := service.Write(context.Background(), WriteFileRequest{Path: path, Data: `{"version":1}`}); err != nil {
		t.Fatal(err)
	}
	data, err := service.Read(context.Background(), FilePathRequest{Path: path})
	if err != nil || data != `{"version":1}` {
		t.Fatalf("Read() = %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}
