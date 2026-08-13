//go:build wails && !e2e

package main

import "github.com/MaimoryLab/BootAgent/internal/app"

func newDesktopUseCases() *app.UseCases {
	return app.NewUseCasesFromEnvironment()
}
