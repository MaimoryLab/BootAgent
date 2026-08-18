//go:build wails

package main

import (
	"context"
	_ "embed"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	bootagent "github.com/MaimoryLab/BootAgent"
	"github.com/MaimoryLab/BootAgent/internal/app"
	"github.com/MaimoryLab/BootAgent/internal/binding"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/process"
	"github.com/MaimoryLab/BootAgent/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

//go:embed appicon.png
var appIcon []byte

func configureUpdater(appInstance *application.App) binding.UpdateBackend {
	current := version.UpdaterVersion()
	if current == "" {
		return nil
	}
	provider, err := github.New(github.Config{
		Repository:    "MaimoryLab/BootAgent",
		ChecksumAsset: "SHA256SUMS",
		// Every release ships platform-specific installers plus one OTA .zip.
		// Only the .zip is a format the updater unpacks.
		AssetMatcher: binding.ExtractableAssetMatcher,
		// Supplied rather than left nil: the provider's own default carries a
		// 30-second Client.Timeout, which bounds the body transfer too and so
		// made the update impossible on a slow link. See NewUpdateHTTPClient.
		HTTPClient: binding.NewUpdateHTTPClient(),
	})
	if err != nil {
		slog.Error("BootAgent updater provider is unavailable", "error", err)
		return nil
	}
	if err := appInstance.Updater.Init(updater.Config{
		CurrentVersion: current,
		Providers:      []updater.Provider{provider},
		Window:         updater.WindowNone,
	}); err != nil {
		slog.Error("BootAgent updater is unavailable", "error", err)
		return nil
	}
	return appInstance.Updater
}

type quitAwareUpdater struct {
	binding.UpdateBackend
	quitting *atomic.Bool
}

func (u quitAwareUpdater) Restart(ctx context.Context) error {
	u.quitting.Store(true)
	if err := u.UpdateBackend.Restart(ctx); err != nil {
		u.quitting.Store(false)
		return err
	}
	return nil
}

func configureSystemTray(appInstance *application.App, core *app.UseCases, window application.Window, quitting *atomic.Bool) {
	tray := appInstance.SystemTray.New()
	refresh := func() {}
	refresh = func() {
		config, err := core.Conversion(context.Background())
		if err != nil {
			slog.Warn("conversion tray state unavailable", "error", err)
		}
		menu := appInstance.Menu.New()
		menu.Add("显示 BootAgent").OnClick(func(*application.Context) { tray.ShowWindow() })
		menu.AddSeparator()
		enabled := config.Enabled
		toggleLabel := "启动 API 转换"
		if enabled {
			toggleLabel = "停止 API 转换"
		}
		menu.AddCheckbox(toggleLabel, enabled).OnClick(func(*application.Context) {
			if _, err := core.SetConversionEnabled(context.Background(), !enabled); err != nil {
				slog.Warn("conversion tray toggle failed", "error", err)
			}
			refresh()
		})
		profilesMenu := menu.AddSubmenu("转换目标 Profile")
		profiles, err := core.ListProfiles(context.Background())
		if err == nil {
			for _, profile := range profiles {
				if profile.Protocol != "openai" || strings.HasPrefix(profile.ID, "bootagent-converter-") || strings.HasPrefix(profile.ID, "bootagent_converter_") {
					continue
				}
				label := profile.Label
				if label == "" {
					label = profile.ID
				}
				profileID := profile.ID
				profilesMenu.AddRadio(label, profileID == config.TargetProfile).OnClick(func(*application.Context) {
					if _, err := core.SetConversionTargetProfile(context.Background(), profileID); err != nil {
						slog.Warn("conversion tray profile switch failed", "error", err)
					}
					refresh()
				})
			}
		}
		menu.AddSeparator()
		menu.Add("退出 BootAgent").OnClick(func(*application.Context) {
			quitting.Store(true)
			appInstance.Quit()
		})
		tray.SetMenu(menu)
	}
	refresh()
	tray.SetTooltip("BootAgent")
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(appIcon)
	} else {
		tray.SetIcon(appIcon)
	}
	tray.AttachWindow(window).WindowOffset(5)
}

func main() {
	var appInstance *application.App
	var mainWindow application.Window
	var windowMu sync.RWMutex
	activatePending := false
	activateMainWindow := func() {
		windowMu.Lock()
		window := mainWindow
		if window == nil {
			activatePending = true
			windowMu.Unlock()
			return
		}
		windowMu.Unlock()
		window.Show().Focus()
	}
	core := newDesktopUseCases()
	var quitting atomic.Bool
	var startupSyncOnce sync.Once
	services := binding.NewServicesWithOptions(core, func(url string) error {
		current := application.Get()
		if current == nil || current.Browser == nil {
			return oneerrors.New(oneerrors.InternalError, "Desktop browser is not ready")
		}
		return current.Browser.OpenURL(url)
	}, binding.ServicesOptions{
		Autostart: binding.AutostartCallbacks{
			IsEnabled: func() (bool, error) {
				if appInstance == nil || appInstance.Autostart == nil {
					return false, oneerrors.New(oneerrors.InternalError, "Autostart manager is not ready")
				}
				return appInstance.Autostart.IsEnabled()
			},
			SetEnabled: func(enabled bool) error {
				if appInstance == nil || appInstance.Autostart == nil {
					return oneerrors.New(oneerrors.InternalError, "Autostart manager is not ready")
				}
				if enabled {
					return appInstance.Autostart.Enable()
				}
				return appInstance.Autostart.Disable()
			},
		},
		AfterGetStatus: func(status app.StatusResponse) {
			if status.FirstRun {
				return
			}
			startupSyncOnce.Do(func() {
				if _, err := core.ScanMCP(context.Background()); err != nil {
					slog.Warn("MCP startup scan failed", "error", err)
				}
				if _, err := core.ScanSkills(context.Background()); err != nil {
					slog.Warn("Skill startup scan failed", "error", err)
				}
			})
		},
		InstallOutput: func(output process.Output) {
			if appInstance != nil {
				appInstance.Event.Emit("bootagent:install-output", output)
			}
		},
	})
	var singleInstance *application.SingleInstanceOptions
	if !application.System.IsServer() {
		singleInstance = &application.SingleInstanceOptions{
			UniqueID: "com.maimorylab.bootagent",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				activateMainWindow()
			},
		}
	}

	// No Route or RawMessageHandler is configured. The default Wails transport
	// is internal IPC; the production app does not expose a business HTTP port.
	appInstance = application.New(application.Options{
		Name:           "BootAgent",
		Description:    "Local AI development environment activator",
		LogLevel:       slog.LevelInfo,
		SingleInstance: singleInstance,
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
			application.NewServiceWithOptions(services.Conversion, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
		},
		MarshalError: oneerrors.Marshal,
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(bootagent.FrontendAssets),
			DisableLogging: true,
		},
		Mac:        application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		ShouldQuit: func() bool { return quitting.Load() },
	})
	appInstance.OnShutdown(func() { _ = core.CloseConversion() })
	updateBackend := configureUpdater(appInstance)
	if updateBackend != nil {
		updateBackend = quitAwareUpdater{UpdateBackend: updateBackend, quitting: &quitting}
	}
	appInstance.Event.On(updater.EventDownloadProgress, func(event *application.CustomEvent) {
		if event == nil {
			return
		}
		if output, ok := binding.UpdateProgressOutput(event.Data); ok {
			appInstance.Event.Emit("bootagent:install-output", output)
		}
	})
	appInstance.RegisterService(application.NewServiceWithOptions(binding.NewUpdateService(updateBackend), application.ServiceOptions{MarshalError: oneerrors.Marshal}))
	if settings, err := core.Settings(context.Background()); err != nil {
		slog.Warn("autostart setting unavailable", "error", err)
	} else if settings.Autostart {
		if err := appInstance.Autostart.Enable(); err != nil {
			slog.Warn("autostart registration failed", "error", err)
		}
	}
	if !application.System.IsServer() {
		// A floor, not a second breakpoint: the sidebar deliberately collapses to
		// a 72px icon rail under 900px, and the layout is verified down to 560px.
		// This only stops the window being dragged narrower than any breakpoint
		// accounts for, where the Agent rows and page padding have nothing left.
		window := appInstance.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:     "BootAgent",
			Width:     1180,
			Height:    760,
			MinWidth:  560,
			MinHeight: 480,
			URL:       "/",
		})
		windowMu.Lock()
		mainWindow = window
		pending := activatePending
		activatePending = false
		windowMu.Unlock()
		if pending {
			window.Show().Focus()
		}
		window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			if quitting.Load() {
				return
			}
			event.Cancel()
			window.Hide()
		})
		configureSystemTray(appInstance, core, window, &quitting)
	}
	if err := appInstance.Run(); err != nil {
		// Do not print an arbitrary Wails error containing binding arguments.
		_, _ = os.Stderr.WriteString("BootAgent desktop failed to start\n")
		os.Exit(1)
	}
}
