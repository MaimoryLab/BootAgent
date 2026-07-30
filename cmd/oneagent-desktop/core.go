//go:build wails && !e2e

package main

import "github.com/MaimoryLab/OneAgent/internal/app"

func newDesktopUseCases() *app.UseCases {
	return app.NewUseCasesFromEnvironment()
}
