package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

// settingsCore builds a UseCases whose region answer is fixed by LANG rather
// than by the developer's own machine. Without that, a workstation set to
// Chinese would silently flip the regional default and make these tests report
// the host's configuration instead of the behavior under test.
func settingsCore(t *testing.T, home string, locale ...string) *UseCases {
	t.Helper()
	language := "en_US.UTF-8"
	if len(locale) > 0 {
		language = locale[0]
	}
	// A PATH naming a directory that does not exist keeps the developer's own uv
	// from satisfying the runtime and skipping the download entirely.
	environment := map[string]string{"HOME": home, "PATH": filepath.Join(home, "empty"), "LANG": language}
	return NewUseCases(StatusOptions{
		Home:        home,
		Platform:    platform.For("linux", "x64"),
		Environment: environment,
		Runner:      process.New(environment),
	})
}

func TestSettingsDefaultToTheOfficialSourceAndSurviveARestart(t *testing.T) {
	home := t.TempDir()
	core := settingsCore(t, home)

	// A machine with no settings file must not silently prefer a mirror.
	settings, err := core.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.PreferMirror {
		t.Fatal("a fresh install defaulted to the mirror")
	}

	if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: true}); err != nil {
		t.Fatal(err)
	}
	// A second UseCases reads the same home, which is what a restart looks like.
	reloaded, err := settingsCore(t, home).Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.PreferMirror {
		t.Fatalf("the preference did not survive a restart: %#v", reloaded)
	}
	if reloaded.SchemaVersion != settingsSchemaVersion {
		t.Fatalf("schema version = %d", reloaded.SchemaVersion)
	}

	// Turning it back off must persist too, not fall back to the stored true.
	if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: false}); err != nil {
		t.Fatal(err)
	}
	if again, _ := settingsCore(t, home).Settings(context.Background()); again.PreferMirror {
		t.Fatal("clearing the preference did not persist")
	}
}

// A corrupt preferences file must not make the app unusable: the download host
// is a convenience, and refusing to report status over it would be worse than
// falling back to the official source.
func TestUnreadableSettingsFallBackToDefaults(t *testing.T) {
	home := t.TempDir()
	core := settingsCore(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".oneagent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.settingsPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := core.Settings(context.Background())
	if err != nil {
		t.Fatalf("a corrupt settings file became an error: %v", err)
	}
	if settings.PreferMirror {
		t.Fatal("a corrupt settings file enabled the mirror")
	}
	// The next write repairs the file rather than leaving it broken.
	if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: true}); err != nil {
		t.Fatal(err)
	}
	if repaired, _ := core.Settings(context.Background()); !repaired.PreferMirror {
		t.Fatal("the settings file was not repaired by a write")
	}
}

// The setting has to reach the download, not just the disk. A downloader that
// records which host was asked first shows whether InstallRuntime consulted the
// stored preference, without needing a real archive.
func TestStoredMirrorPreferenceReachesTheDownload(t *testing.T) {
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		t.Fatal(err)
	}
	// settingsCore pins the platform so the region answer is deterministic; read
	// the artifact for that same platform rather than for the host.
	artifact := manifest.Runtimes["uv"].Artifacts[catalog.RuntimeArtifactKey("linux", "x64")]
	if artifact.URL == "" || artifact.MirrorURL == "" {
		t.Skip("no locked uv artifact with a mirror for this platform")
	}

	for _, testCase := range []struct {
		name    string
		stored  bool
		request *bool
		first   string
	}{
		{"official by default", false, nil, artifact.URL},
		{"mirror when stored", true, nil, artifact.MirrorURL},
		// A per-request override exists so a future retry button can switch host
		// without changing the saved preference.
		{"request overrides the setting", true, new(false), artifact.URL},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			core := settingsCore(t, home)
			if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: testCase.stored}); err != nil {
				t.Fatal(err)
			}
			// Every request 404s, so the install fails; the recorded order is the
			// assertion and nothing is downloaded.
			downloader := &archiveDoer{bodies: map[string][]byte{}}
			core.SetRuntimeDownloader(downloader)
			if _, err := core.InstallRuntime(context.Background(), InstallRuntimeOptions{RuntimeID: "uv", PreferMirror: testCase.request}); err == nil {
				t.Fatal("a downloader serving nothing produced a successful install")
			}
			if len(downloader.order) == 0 || downloader.order[0] != testCase.first {
				t.Fatalf("first host = %v, want %s", downloader.order, testCase.first)
			}
		})
	}
}

//go:fix inline
func boolPointer(value bool) *bool { return new(value) }

// A first run on a machine set to Chinese should not have to discover the mirror
// on its own: the official hosts are consistently slow from there.
func TestAChineseMachineDefaultsToTheMirror(t *testing.T) {
	settings, err := settingsCore(t, t.TempDir(), "zh_CN.UTF-8").Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !settings.PreferMirror {
		t.Fatal("a Chinese locale did not take the mirror by default")
	}
	// The UI needs to distinguish this from a choice the user made, so it can say
	// why the box is ticked.
	if !settings.MirrorFromRegion {
		t.Fatal("a region-derived default was not reported as such")
	}

	other, err := settingsCore(t, t.TempDir(), "en_US.UTF-8").Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if other.PreferMirror || other.MirrorFromRegion {
		t.Fatalf("a non-Chinese locale took the mirror: %#v", other)
	}
}

// The regional default is a default, not an override. A user in China who
// prefers the official source says so once; re-ticking the box on every launch
// would make the setting useless to exactly the people it targets.
func TestAnExplicitChoiceOutranksTheRegionalDefault(t *testing.T) {
	home := t.TempDir()
	core := settingsCore(t, home, "zh_CN.UTF-8")
	if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: false}); err != nil {
		t.Fatal(err)
	}
	// A fresh UseCases is what the next launch looks like.
	reloaded, err := settingsCore(t, home, "zh_CN.UTF-8").Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.PreferMirror {
		t.Fatal("the regional default overrode a stored 'off'")
	}
	if reloaded.MirrorFromRegion {
		t.Fatal("a stored choice was reported as region-derived")
	}
	// And the install path agrees with what was reported.
	if settingsCore(t, home, "zh_CN.UTF-8").preferMirror(context.Background()) {
		t.Fatal("the download still preferred the mirror")
	}
}

// The regional default must reach the download, not only the settings screen: a
// first-run user in China installs a runtime before ever opening the setting.
func TestTheRegionalDefaultReachesTheDownload(t *testing.T) {
	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		t.Fatal(err)
	}
	artifact := manifest.Runtimes["uv"].Artifacts[catalog.RuntimeArtifactKey("linux", "x64")]
	if artifact.MirrorURL == "" {
		t.Skip("no locked uv mirror for this platform")
	}
	core := settingsCore(t, t.TempDir(), "zh_CN.UTF-8")
	downloader := &archiveDoer{bodies: map[string][]byte{}}
	core.SetRuntimeDownloader(downloader)
	if _, err := core.InstallRuntime(context.Background(), InstallRuntimeOptions{RuntimeID: "uv"}); err == nil {
		t.Fatal("a downloader serving nothing produced a successful install")
	}
	if len(downloader.order) == 0 || downloader.order[0] != artifact.MirrorURL {
		t.Fatalf("first host = %v, want the mirror %s", downloader.order, artifact.MirrorURL)
	}
}

// The probe runs at most once per process. Settings is read on every status
// poll, and spawning `defaults` or PowerShell each time would be a visible cost
// for an answer that cannot change without a system setting changing.
func TestTheRegionProbeRunsOnlyOnce(t *testing.T) {
	home := t.TempDir()
	environment := map[string]string{"HOME": home, "PATH": filepath.Join(home, "empty")}
	counter := &countingRunner{Runner: process.New(environment)}
	core := NewUseCases(StatusOptions{
		// macOS needs a lookup, so this exercises the cached subprocess path.
		Home: home, Platform: platform.For("darwin", "arm64"),
		Environment: environment, Runner: counter,
	})
	for range 5 {
		if _, err := core.Settings(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if counter.runs != 1 {
		t.Fatalf("region probe ran %d times, want 1", counter.runs)
	}
}

type countingRunner struct {
	process.Runner
	runs int
}

func (r *countingRunner) Run(ctx context.Context, argv []string, env map[string]string, timeout time.Duration) (process.Result, error) {
	r.runs++
	return r.Runner.Run(ctx, argv, env, timeout)
}

// The same preference has to reach npm, not just the runtime download. The fake
// runner records every argv, so the assertion is the registry npm was actually
// pointed at. That first call is the locked dist.integrity check, which is what
// makes the mirror safe: it is verified against agents.lock.json before install.
func TestStoredMirrorPreferenceReachesTheNPMInstall(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		stored   bool
		request  string
		registry string
	}{
		{"official by default", false, "", "https://registry.npmjs.org/"},
		{"mirror when stored", true, "", "https://registry.npmmirror.com/"},
		// An explicit registry is the user's own instruction for this one run and
		// must not be overridden by the saved preference.
		{"request overrides the setting", true, "official", "https://registry.npmjs.org/"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			// codex is absent so the install actually runs instead of short-circuiting
			// on an already-locked version.
			runner := &installAppRunner{paths: map[string]string{"npm": "/fake/npm"}}
			core := installCore(t, home, runner, installAppDoer(func(*http.Request) (*http.Response, error) {
				return installAppResponse(http.StatusNoContent, ""), nil
			}))
			if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: testCase.stored}); err != nil {
				t.Fatal(err)
			}
			options := installOptions("codex")
			options.InstallAgent = true
			options.LockedVersion = true
			options.Registry = testCase.request
			if _, err := core.InstallAgents(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			var got string
			for _, argv := range runner.calls {
				for _, argument := range argv {
					if after, ok := strings.CutPrefix(argument, "--registry="); ok {
						got = after
					}
				}
			}
			if got != testCase.registry {
				t.Fatalf("npm registry = %q, want %q", got, testCase.registry)
			}
		})
	}
}
