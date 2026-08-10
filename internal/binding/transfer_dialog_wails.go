//go:build wails

package binding

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func selectImportFile() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("desktop file dialog is unavailable")
	}
	return app.Dialog.OpenFile().SetTitle("Select import file").AddFilter("JSON", "*.json").PromptForSingleSelection()
}

func selectExportFile() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("desktop file dialog is unavailable")
	}
	return app.Dialog.SaveFile().SetMessage("Choose export location").SetFilename("oneagent-settings.json").AddFilter("JSON", "*.json").PromptForSingleSelection()
}
