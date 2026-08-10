//go:build !wails

package binding

import "fmt"

func selectImportFile() (string, error) { return "", fmt.Errorf("desktop file dialog is unavailable") }

func selectExportFile() (string, error) { return "", fmt.Errorf("desktop file dialog is unavailable") }
