package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
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
		Runner:      process.OSRunner{Env: environment},
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
	if settings.BackupRetention != 3 {
		t.Fatalf("fresh backup retention = %d, want 3", settings.BackupRetention)
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

func TestBackupRetentionPersistsAndBoundsValues(t *testing.T) {
	home := t.TempDir()
	core := settingsCore(t, home)
	if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: true, BackupRetention: 7}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := settingsCore(t, home).Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.BackupRetention != 7 || !reloaded.PreferMirror {
		t.Fatalf("reloaded settings = %#v", reloaded)
	}
	for _, value := range []int{0, -1, 101, 1000} {
		if _, err := core.SaveSettings(context.Background(), Settings{BackupRetention: value}); err != nil {
			t.Fatalf("save retention %d: %v", value, err)
		}
		got, err := core.Settings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := 3
		if value == 0 {
			want = 7
		} else if value < 1 {
			want = 1
		}
		if value > 100 {
			want = 100
		}
		if got.BackupRetention != want {
			t.Fatalf("saved retention %d read as %d, want %d", value, got.BackupRetention, want)
		}
	}
}

func TestBackupRetentionReadRejectsOutOfRangeValues(t *testing.T) {
	for _, value := range []int{0, -1, 101, 1000} {
		home := t.TempDir()
		core := settingsCore(t, home)
		if err := os.MkdirAll(filepath.Dir(core.settingsPath()), 0o700); err != nil {
			t.Fatal(err)
		}
		data := []byte(`{"schema_version":1,"prefer_mirror":false,"backup_retention":` + fmt.Sprint(value) + `}`)
		if err := os.WriteFile(core.settingsPath(), data, 0o600); err != nil {
			t.Fatal(err)
		}
		settings, err := core.Settings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if settings.BackupRetention != 3 {
			t.Fatalf("stored retention %d read as %d, want 3", value, settings.BackupRetention)
		}
	}
}

func TestBackupRetentionDefaultsForLegacySettingsAndOldCallers(t *testing.T) {
	home := t.TempDir()
	core := settingsCore(t, home)
	if err := os.MkdirAll(filepath.Dir(core.settingsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.settingsPath(), []byte(`{"schema_version":1,"prefer_mirror":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := core.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.BackupRetention != 3 || !settings.PreferMirror {
		t.Fatalf("legacy settings = %#v", settings)
	}
	if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: false}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := core.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.BackupRetention != 3 || reloaded.PreferMirror {
		t.Fatalf("old caller changed defaults unexpectedly: %#v", reloaded)
	}
}

// A corrupt preferences file must not make the app unusable: the download host
// is a convenience, and refusing to report status over it would be worse than
// falling back to the official source.
func TestUnreadableSettingsFallBackToDefaults(t *testing.T) {
	home := t.TempDir()
	core := settingsCore(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".bootagent"), 0o700); err != nil {
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

func TestNativeWindowsRegionDefaultsToTheMirrorWithoutPowerShell(t *testing.T) {
	home := t.TempDir()
	environment := map[string]string{"USERPROFILE": home, "PATH": filepath.Join(home, "empty")}
	runner := &scriptedRegionRunner{}
	core := NewUseCases(StatusOptions{
		Home: home, Platform: platform.For("windows", "amd64"),
		Environment: environment, Runner: runner, SystemRegion: "en-US\n45\n",
	})
	settings, err := core.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !settings.PreferMirror || !settings.MirrorFromRegion {
		t.Fatalf("Windows China region did not enable the mirror: %#v", settings)
	}
	if runner.runs != 0 {
		t.Fatalf("native Windows region still spawned PowerShell %d time(s)", runner.runs)
	}

	manifest, err := catalog.LoadEmbeddedRuntimes()
	if err != nil {
		t.Fatal(err)
	}
	artifact := manifest.Runtimes["uv"].Artifacts[catalog.RuntimeArtifactKey("windows", "x64")]
	downloader := &archiveDoer{bodies: map[string][]byte{}}
	core.SetRuntimeDownloader(downloader)
	if _, err := core.InstallRuntime(context.Background(), InstallRuntimeOptions{RuntimeID: "uv"}); err == nil {
		t.Fatal("a downloader serving nothing produced a successful install")
	}
	if len(downloader.order) == 0 || downloader.order[0] != artifact.MirrorURL {
		t.Fatalf("first Windows runtime host = %v, want mirror %s", downloader.order, artifact.MirrorURL)
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
	counter := &countingRunner{Runner: process.OSRunner{Env: environment}}
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

// A probe that could not answer must not be remembered. Settings is read on
// every status poll, so the first read can easily arrive with a context the UI
// has already cancelled; caching that "no" would cost every download for the
// rest of the session its mirror, on exactly the machines that need it.
func TestAFailedRegionProbeIsRetriedRatherThanCached(t *testing.T) {
	home := t.TempDir()
	environment := map[string]string{"HOME": home, "PATH": filepath.Join(home, "empty")}
	// macOS asks a subprocess, so the answer depends on the probe rather than on
	// the environment alone.
	runner := &scriptedRegionRunner{answer: "zh_CN\n"}
	core := NewUseCases(StatusOptions{
		Home: home, Platform: platform.For("darwin", "arm64"),
		Environment: environment, Runner: runner,
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if first, err := core.Settings(cancelled); err != nil || first.PreferMirror {
		t.Fatalf("a failing probe defaulted to the mirror: %#v, %v", first, err)
	}

	// The machine is willing to answer now, and the answer is China.
	runner.fail = false
	second, err := core.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.PreferMirror || !second.MirrorFromRegion {
		t.Fatalf("the retried probe did not take the mirror: %#v", second)
	}
	// And the successful answer is still cached, so status polling stays cheap.
	if _, err := core.Settings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.runs != 2 {
		t.Fatalf("probe ran %d times, want 2: the retry, then a cache hit", runner.runs)
	}
}

// scriptedRegionRunner fails until told otherwise, which is how a cancelled or
// hung locale lookup looks to Settings.
type scriptedRegionRunner struct {
	process.Runner
	answer string
	fail   bool
	runs   int
}

func (r *scriptedRegionRunner) Run(context.Context, []string, map[string]string, time.Duration) (process.Result, error) {
	r.runs++
	if r.runs == 1 {
		return process.Result{ExitCode: 1}, context.Canceled
	}
	if r.fail {
		return process.Result{ExitCode: 1}, nil
	}
	return process.Result{ExitCode: 0, Stdout: r.answer}, nil
}

func (r *scriptedRegionRunner) LookPath(string) (string, bool) { return "", false }

type countingRunner struct {
	process.Runner
	runs int
}

// Answers the probe itself rather than delegating to a real process. Delegating
// made the result depend on the host: the caller simulates macOS, so the argv is
// `defaults read -g AppleLocale`, which succeeds on a developer's Mac and is not
// a command at all on Linux. A probe that cannot run reports "unanswered" and is
// retried by design, so on Linux the count was 5 rather than 1 -- the assertion
// failed while the caching it describes was working correctly.
func (r *countingRunner) Run(context.Context, []string, map[string]string, time.Duration) (process.Result, error) {
	r.runs++
	return process.Result{ExitCode: 0, Stdout: "en_US\n"}, nil
}

func (r *countingRunner) LookPath(string) (string, bool) { return "", false }

// The same preference has to reach npm, not just the runtime download. The fake
// runner records every environment, so the assertion is the registry npm was
// actually pointed at.
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
			// codex is absent so the install actually runs instead of short-circuiting.
			runner := &installAppRunner{paths: map[string]string{"npm": "/fake/npm"}}
			core := installCore(t, home, runner, installAppDoer(func(*http.Request) (*http.Response, error) {
				return installAppResponse(http.StatusNoContent, ""), nil
			}))
			if _, err := core.SaveSettings(context.Background(), Settings{PreferMirror: testCase.stored}); err != nil {
				t.Fatal(err)
			}
			options := installOptions("codex")
			options.InstallAgent = true
			options.Registry = testCase.request
			if _, err := core.InstallAgents(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			got := "https://registry.npmjs.org/"
			for _, environment := range runner.envs {
				if registry := environment["npm_config_registry"]; registry != "" {
					got = registry
				}
			}
			if got != testCase.registry {
				t.Fatalf("npm registry = %q, want %q", got, testCase.registry)
			}
			if testCase.registry == "https://registry.npmmirror.com/" {
				found := false
				for _, call := range runner.calls {
					if strings.Contains(strings.Join(call, " "), "--registry=https://registry.npmmirror.com/") {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("npm argv omitted the mirror registry: %#v", runner.calls)
				}
			}
		})
	}
}
