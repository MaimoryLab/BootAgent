//go:build wails

package main

import (
	"log/slog"
	"os"

	oneagent "github.com/MaimoryLab/OneAgent"
	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/binding"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	core := app.NewUseCasesFromEnvironment()
	services := binding.NewServices(core, func(url string) error {
		current := application.Get()
		if current == nil || current.Browser == nil {
			return oneerrors.New(oneerrors.InternalError, "Desktop browser is not ready")
		}
		return current.Browser.OpenURL(url)
	})

	// No Route or RawMessageHandler is configured. The default Wails transport
	// is internal IPC; the production app does not expose a business HTTP port.
	appInstance := application.New(application.Options{
		Name:        "OneAgent",
		Description: "Local AI development environment activator",
		LogLevel:    slog.LevelInfo,
		Services: []application.Service{
			application.NewServiceWithOptions(services.Status, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Provider, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Agent, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Profile, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
		},
		MarshalError: oneerrors.Marshal,
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(oneagent.FrontendAssets),
			DisableLogging: true,
		},
		Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	appInstance.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "OneAgent",
		Width:  1180,
		Height: 760,
		URL:    "/",
	})
	if err := appInstance.Run(); err != nil {
		// Do not print an arbitrary Wails error containing binding arguments.
		_, _ = os.Stderr.WriteString("OneAgent desktop failed to start\n")
		os.Exit(1)
	}
}
