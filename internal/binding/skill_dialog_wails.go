//go:build wails

package binding

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func selectSkillPath(source string) (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("desktop file dialog is unavailable")
	}
	dialog := app.Dialog.OpenFile().SetTitle("Select Skill source")
	switch source {
	case "folder", "agent":
		return dialog.CanChooseFiles(false).CanChooseDirectories(true).PromptForSingleSelection()
	case "zip":
		return dialog.CanChooseFiles(true).CanChooseDirectories(false).AddFilter("ZIP archive", "*.zip").PromptForSingleSelection()
	default:
		return "", fmt.Errorf("invalid Skill import source")
	}
}
