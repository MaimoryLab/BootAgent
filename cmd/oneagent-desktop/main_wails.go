//go:build wails

package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	oneagent "github.com/MaimoryLab/OneAgent"
	"github.com/MaimoryLab/OneAgent/internal/binding"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/process"
	"github.com/MaimoryLab/OneAgent/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func configureUpdater(appInstance *application.App) binding.UpdateBackend {
	current := version.UpdaterVersion()
	if current == "" {
		return nil
	}
	provider, err := github.New(github.Config{
		Repository:    "MaimoryLab/OneAgent",
		ChecksumAsset: "SHA256SUMS",
		// Every release ships a .dmg, an installer .exe and a .zip per
		// platform. Only the .zip is a format the updater unpacks.
		AssetMatcher: binding.ExtractableAssetMatcher,
	})
	if err != nil {
		slog.Error("OneAgent updater provider is unavailable", "error", err)
		return nil
	}
	if err := appInstance.Updater.Init(updater.Config{
		CurrentVersion: current,
		Providers:      []updater.Provider{provider},
		Window:         updater.WindowNone,
	}); err != nil {
		slog.Error("OneAgent updater is unavailable", "error", err)
		return nil
	}
	return appInstance.Updater
}

func main() {
	var appInstance *application.App
	core := newDesktopUseCases()
	var startupSyncOnce sync.Once
	var nativeSmokeOnce sync.Once
	var afterGetStatus func()
	if os.Getenv("ONEAGENT_NATIVE_SMOKE") == "1" {
		afterGetStatus = func() {
			nativeSmokeOnce.Do(func() {
				if result := os.Getenv("ONEAGENT_NATIVE_SMOKE_RESULT"); result != "" {
					_ = os.WriteFile(result, []byte("ok\n"), 0o600)
				}
				time.AfterFunc(250*time.Millisecond, func() {
					if appInstance != nil {
						appInstance.Quit()
					}
				})
			})
		}
	}
	services := binding.NewServicesWithOptions(core, func(url string) error {
		current := application.Get()
		if current == nil || current.Browser == nil {
			return oneerrors.New(oneerrors.InternalError, "Desktop browser is not ready")
		}
		return current.Browser.OpenURL(url)
	}, binding.ServicesOptions{
		AfterGetStatus: func() {
			startupSyncOnce.Do(func() {
				if _, err := core.ScanMCP(context.Background()); err != nil {
					slog.Warn("MCP startup scan failed", "error", err)
				}
				if _, err := core.ScanSkills(context.Background()); err != nil {
					slog.Warn("Skill startup scan failed", "error", err)
				}
			})
			if afterGetStatus != nil {
				afterGetStatus()
			}
		},
		InstallOutput: func(output process.Output) {
			if appInstance != nil {
				appInstance.Event.Emit("oneagent:install-output", output)
			}
		},
	})

	// No Route or RawMessageHandler is configured. The default Wails transport
	// is internal IPC; the production app does not expose a business HTTP port.
	appInstance = application.New(application.Options{
		Name:        "OneAgent",
		Description: "Local AI development environment activator",
		LogLevel:    slog.LevelInfo,
		Services: []application.Service{
			application.NewServiceWithOptions(services.Status, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Provider, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Agent, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Profile, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Runtime, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.DesktopAgent, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Transfer, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.MCP, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Skill, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
		},
		MarshalError: oneerrors.Marshal,
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(oneagent.FrontendAssets),
			DisableLogging: true,
		},
		Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	updateBackend := configureUpdater(appInstance)
	appInstance.Event.On(updater.EventDownloadProgress, func(event *application.CustomEvent) {
		if event == nil {
			return
		}
		if output, ok := binding.UpdateProgressOutput(event.Data); ok {
			appInstance.Event.Emit("oneagent:install-output", output)
		}
	})
	appInstance.RegisterService(application.NewServiceWithOptions(binding.NewUpdateService(updateBackend), application.ServiceOptions{MarshalError: oneerrors.Marshal}))
	if !application.System.IsServer() {
		// A floor, not a second breakpoint: the sidebar deliberately collapses to
		// a 72px icon rail under 900px, and the layout is verified down to 560px.
		// This only stops the window being dragged narrower than any breakpoint
		// accounts for, where the Agent rows and page padding have nothing left.
		window := appInstance.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:     "OneAgent",
			Width:     1180,
			Height:    760,
			MinWidth:  560,
			MinHeight: 480,
			URL:       "/",
		})
		var closingBypass atomic.Bool
		window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			if closingBypass.Swap(false) {
				return
			}
			dirty, locale := core.DraftState()
			if !dirty {
				return
			}
			event.Cancel()
			message, discard, cancel := "MCP 或 Skills 草稿尚未应用，确定放弃并关闭吗？", "放弃并关闭", "取消"
			if locale == "en" {
				message, discard, cancel = "MCP or Skills changes are not applied. Discard them and close?", "Discard and close", "Cancel"
			}
			confirmed := false
			dialog := application.Get().Dialog.Question().SetTitle("OneAgent").SetMessage(message)
			dialog.AddButton(discard).OnClick(func() { confirmed = true })
			dialog.AddButton(cancel).SetAsCancel()
			dialog.Show()
			if confirmed {
				core.SetMCPDraftState(false, locale)
				core.SetSkillDraftState(false, locale)
				closingBypass.Store(true)
				window.Close()
			}
		})
	}
	if err := appInstance.Run(); err != nil {
		// Do not print an arbitrary Wails error containing binding arguments.
		_, _ = os.Stderr.WriteString("OneAgent desktop failed to start\n")
		os.Exit(1)
	}
}
