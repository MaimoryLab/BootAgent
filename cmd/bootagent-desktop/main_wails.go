//go:build wails

package main

import (
	"context"
	_ "embed"
	"errors"
	"io"
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

const updateMirrorMetadata = "bootagent.update.mirror"

type updateProvider struct {
	official, mirror updater.Provider
	preferMirror     func(context.Context) bool
}

func (p updateProvider) Name() string { return "bootagent-release" }

// Check asks the preferred source, and falls back to the other source when the
// selected source could not answer. This keeps a transient API outage or rate
// limit from making update checks fail when the other source is available.
//
// The mirror needs the fallback because it fails in ways the official source does
// not. Fetching SHA256SUMS from Gitee answered 403 on roughly a third of
// consecutive attempts, and the mirror is missing ota-BootAgent-windows-arm64.zip
// altogether, which is a permanent "no asset for windows/arm64". Neither reaches
// the user as a failure: the automatic check treats any error as "no update"
// (AppUpdater), so before this a mirror user simply stopped being offered
// updates, silently and with nothing to retry.
func (p updateProvider) Check(ctx context.Context, request updater.CheckRequest) (*updater.Release, error) {
	preferredMirror := p.preferMirror(ctx)
	release, err := p.check(ctx, request, preferredMirror)
	if err == nil {
		return release, nil
	}
	// A cancelled check is the caller's decision, not a source that failed;
	// retrying elsewhere would reopen exactly the request that was abandoned.
	if ctx.Err() != nil {
		return release, err
	}
	slog.Warn("BootAgent update check failed; trying the alternate source", "mirror", preferredMirror, "error", err)
	fallbackRelease, fallbackErr := p.check(ctx, request, !preferredMirror)
	if fallbackErr != nil {
		// Both errors are kept: the mirror's is what the user's configured route
		// did, and joining preserves the sentinels updateError matches on.
		return nil, errors.Join(err, fallbackErr)
	}
	return fallbackRelease, nil
}

// check runs one source and records which one answered, so Download goes back to
// the host the release was resolved from.
//
// A nil release with a nil error means that source has nothing newer, which is an
// answer rather than a failure and so is returned as-is. A mirror that lags a
// release behind therefore delays the update until it syncs; that is the cost of
// honouring the preference, and the alternative -- querying both hosts on every
// check -- spends the slow link the preference exists to avoid.
func (p updateProvider) check(ctx context.Context, request updater.CheckRequest, mirror bool) (*updater.Release, error) {
	provider := p.official
	if mirror {
		provider = p.mirror
	}
	release, err := provider.Check(ctx, request)
	if release != nil {
		if release.Metadata == nil {
			release.Metadata = make(map[string]any)
		}
		release.Metadata[updateMirrorMetadata] = mirror
	}
	return release, err
}

func (p updateProvider) Download(ctx context.Context, release *updater.Release, dst io.Writer, progress func(int64, int64)) error {
	if release == nil || release.Metadata == nil {
		return errors.New("update release source is missing")
	}
	mirror, ok := release.Metadata[updateMirrorMetadata].(bool)
	if !ok {
		return errors.New("update release source is invalid")
	}
	if mirror {
		return p.mirror.Download(ctx, release, dst, progress)
	}
	return p.official.Download(ctx, release, dst, progress)
}

func configureUpdater(appInstance *application.App, core *app.UseCases) binding.UpdateBackend {
	current := version.UpdaterVersion()
	if current == "" {
		return nil
	}
	providerConfig := github.Config{
		Repository:    "MaimoryLab/BootAgent",
		ChecksumAsset: "SHA256SUMS",
		// Every release ships platform-specific installers plus one OTA .zip.
		// Only the .zip is a format the updater unpacks.
		AssetMatcher: binding.ExtractableAssetMatcher,
		// Supplied rather than left nil: the provider's own default carries a
		// 30-second Client.Timeout, which bounds the body transfer too and so
		// made the update impossible on a slow link. See NewUpdateHTTPClient.
		HTTPClient: binding.NewUpdateHTTPClient(),
	}
	official, err := github.New(providerConfig)
	if err != nil {
		slog.Error("BootAgent updater provider is unavailable", "error", err)
		return nil
	}
	providerConfig.Repository = "maimory/BootAgent"
	providerConfig.BaseURL = "https://gitee.com/api/v5"
	mirror, err := github.New(providerConfig)
	if err != nil {
		slog.Error("BootAgent mirror updater provider is unavailable", "error", err)
		return nil
	}
	provider := updateProvider{official: official, mirror: mirror, preferMirror: func(ctx context.Context) bool {
		settings, err := core.Settings(ctx)
		return err == nil && settings.PreferMirror
	}}
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

// acceptQuit answers a quit request, and records that one was made.
//
// Wails asks this only when the user has actually asked the app to quit: the
// Dock's Quit, Cmd+Q, the app menu, or a logout. It used to answer "no" unless
// the tray's own Quit item had run first, which made every one of those routes a
// no-op -- on macOS a false answer becomes NSTerminateCancel, so the Dock
// reported the quit as cancelled and the process stayed alive. A logout is
// refused the same way.
//
// Staying resident when the window is closed is a separate question, and it is
// already answered by the WindowClosing hook, which hides the window instead of
// closing it. Recording the intent here is what lets that hook tell a real quit
// from a window close, so on the way out the window closes rather than being
// hidden while the process is already terminating.
func acceptQuit(quitting *atomic.Bool) bool {
	quitting.Store(true)
	return true
}

type quitAwareUpdater struct {
	binding.UpdateBackend
	quitting   *atomic.Bool
	restarting *atomic.Bool
}

func (u quitAwareUpdater) Restart(ctx context.Context) error {
	if !u.restarting.CompareAndSwap(false, true) {
		return nil
	}
	u.quitting.Store(true)
	if err := u.UpdateBackend.Restart(ctx); err != nil {
		u.quitting.Store(false)
		u.restarting.Store(false)
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
	openInBrowser := func(url string) error {
		current := application.Get()
		if current == nil || current.Browser == nil {
			return oneerrors.New(oneerrors.InternalError, "Desktop browser is not ready")
		}
		return current.Browser.OpenURL(url)
	}
	// Launching a web-app Agent ends in the browser, so the core needs the same
	// opener the Provider pages use.
	core.SetURLOpener(openInBrowser)
	services := binding.NewServicesWithOptions(core, openInBrowser, binding.ServicesOptions{
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
			application.NewServiceWithOptions(services.Marketplace, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
			application.NewServiceWithOptions(services.Task, application.ServiceOptions{MarshalError: oneerrors.Marshal}),
		},
		MarshalError: oneerrors.Marshal,
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(bootagent.FrontendAssets),
			DisableLogging: true,
		},
		Mac:        application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		ShouldQuit: func() bool { return acceptQuit(&quitting) },
	})
	appInstance.OnShutdown(func() { _ = core.CloseConversion() })
	updateBackend := configureUpdater(appInstance, core)
	if updateBackend != nil {
		updateBackend = quitAwareUpdater{UpdateBackend: updateBackend, quitting: &quitting, restarting: &atomic.Bool{}}
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
