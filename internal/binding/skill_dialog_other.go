//go:build !wails

package binding

import "fmt"

func selectSkillPath(source string) (string, error) {
	return "", fmt.Errorf("desktop file dialog is unavailable")
}
