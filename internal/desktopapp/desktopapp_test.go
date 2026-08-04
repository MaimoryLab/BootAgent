package desktopapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

type probeRunner struct {
	macValues map[string]string
	windows   []process.Result
	calls     [][]string
	started   [][]string
}

type scriptedRunner struct {
	results []process.Result
	calls   [][]string
	started [][]string
}

type cancelRunner struct {
	cancel  context.CancelFunc
	started bool
}

func (r *cancelRunner) LookPath(string) (string, bool) { return "", false }

func (r *cancelRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.cancel()
	return process.Result{Args: argv, ExitCode: 0}, nil
}

func (r *cancelRunner) Start([]string, map[string]string) error {
	r.started = true
	return nil
}

func (r *scriptedRunner) LookPath(string) (string, bool) { return "", false }

func (r *scriptedRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	if len(r.results) == 0 {
		return process.Result{Args: argv, ExitCode: 0}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	result.Args = argv
	return result, nil
}

func (r *scriptedRunner) Start(argv []string, _ map[string]string) error {
	r.started = append(r.started, append([]string(nil), argv...))
	return nil
}

func (r *probeRunner) LookPath(string) (string, bool) { return "", false }

func (r *probeRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	if argv[0] == "/usr/bin/plutil" {
		key := argv[2]
		if value, ok := r.macValues[key]; ok {
			return process.Result{Args: argv, ExitCode: 0, Stdout: value + "\n"}, nil
		}
		return process.Result{Args: argv, ExitCode: 1, Stderr: "missing key"}, nil
	}
	if len(r.windows) == 0 {
		return process.Result{Args: argv, ExitCode: 0}, nil
	}
	result := r.windows[0]
	r.windows = r.windows[1:]
	result.Args = argv
	return result, nil
}

func (r *probeRunner) Start(argv []string, _ map[string]string) error {
	r.started = append(r.started, append([]string(nil), argv...))
	return nil
}

func makeBundle(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name, "Contents")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(path)
}

func TestInspectMacOSValidatesBundleIDAndFallsBackToBuildVersion(t *testing.T) {
	root := t.TempDir()
	appPath := makeBundle(t, root, "ChatGPT.app")
	if err := os.WriteFile(filepath.Join(appPath, "Contents", "Info.plist"), []byte("plist"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &probeRunner{macValues: map[string]string{
		"CFBundleIdentifier":         CodexBundleID,
		"CFBundleVersion":            "26.727.51351",
		"CFBundleShortVersionString": "",
	}}
	status := Inspect(context.Background(), Options{
		Platform:    platform.For("darwin", "arm64"),
		SearchRoots: []string{root},
		Runner:      runner,
	})
	if !status.Installed || status.Path != appPath {
		t.Fatalf("status = %#v", status)
	}
	if status.Version == nil || *status.Version != "26.727.51351" {
		t.Fatalf("version = %#v", status.Version)
	}
	if len(runner.calls) != 3 || runner.calls[1][2] != "CFBundleShortVersionString" || runner.calls[2][2] != "CFBundleVersion" {
		t.Fatalf("plutil calls = %#v", runner.calls)
	}
	direct := Inspect(context.Background(), Options{
		Platform:    platform.For("darwin", "arm64"),
		SearchRoots: []string{appPath},
		Runner:      &probeRunner{macValues: runner.macValues},
	})
	if !direct.Installed || direct.Path != appPath {
		t.Fatalf("direct app root status = %#v", direct)
	}
}

func TestInspectMacOSDoesNotTraverseApplicationRoots(t *testing.T) {
	root := t.TempDir()
	nested := makeBundle(t, filepath.Join(root, "nested"), "ChatGPT.app")
	if err := os.WriteFile(filepath.Join(nested, "Contents", "Info.plist"), []byte("plist"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &probeRunner{macValues: map[string]string{"CFBundleIdentifier": CodexBundleID}}
	status := Inspect(nil, Options{Platform: platform.For("macos", "x64"), SearchRoots: []string{root}, Runner: runner})
	if status.Installed {
		t.Fatalf("nested app was detected: %#v", status)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("root probe unexpectedly traversed into nested app: %#v", runner.calls)
	}
}

func TestParseWindowsPackagesSortsFourPartVersions(t *testing.T) {
	output := `[{"Name":"OpenAI.Codex","PackageFullName":"OpenAI.Codex_26.1.0.0_neutral__2p2nqsd0c76g0","Version":"26.1.0.0","PackageFamilyName":"OpenAI.Codex_2p2nqsd0c76g0","InstallLocation":"C:\\Apps\\old"},{"Name":"OpenAI.Codex","PackageFullName":"OpenAI.Codex_26.10.3.0_neutral__2p2nqsd0c76g0","Version":"26.10.3.0","PackageFamilyName":"OpenAI.Codex_2p2nqsd0c76g0","InstallLocation":"C:\\Apps\\new"}]`
	item, ok := parseWindowsPackage(output)
	if !ok || item.Version != "26.10.3.0" || item.InstallLocation != `C:\Apps\new` {
		t.Fatalf("item = %#v, ok=%v", item, ok)
	}
	if versionFromPackageFullName(item.PackageFullName) != "26.10.3.0" {
		t.Fatalf("full-name version = %q", versionFromPackageFullName(item.PackageFullName))
	}
	if packageFamilyFromFullName(item.PackageFullName) != "OpenAI.Codex_2p2nqsd0c76g0" {
		t.Fatalf("family = %q", packageFamilyFromFullName(item.PackageFullName))
	}
}

func TestInspectWindowsUsesRegisteredPackageQueriesOnly(t *testing.T) {
	runner := &probeRunner{windows: []process.Result{{
		ExitCode: 0,
		Stdout:   `{"Name":"OpenAI.Codex","PackageFullName":"OpenAI.Codex_26.727.51351.0_neutral__2p2nqsd0c76g0","Version":"26.727.51351.0","PackageFamilyName":"OpenAI.Codex_2p2nqsd0c76g0","InstallLocation":"C:\\Program Files\\OpenAI\\Codex"}`,
	}}}
	status := Inspect(context.Background(), Options{Platform: platform.For("windows", "amd64"), Runner: runner})
	if !status.Installed || status.Version == nil || *status.Version != "26.727.51351.0" {
		t.Fatalf("status = %#v", status)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "powershell.exe" {
		t.Fatalf("Windows inspection command = %#v", runner.calls)
	}
	if strings.Contains(strings.Join(runner.calls[0], " "), "WindowsApps") {
		t.Fatal("Windows inspection must not scan WindowsApps")
	}
}

func TestInspectWindowsStartAppsFallbackReportsUnknownMetadata(t *testing.T) {
	for _, appID := range []string{
		"OpenAI.Codex_2p2nqsd0c76g0!App",
		"OpenAI.CodexBeta_2p2nqsd0c76g0!App",
		"OpenAI.ChatGPT-Desktop_2p2nqsd0c76g0!App",
	} {
		runner := &probeRunner{windows: []process.Result{
			{ExitCode: 0, Stdout: ""},
			{ExitCode: 0, Stdout: appID + "\n"},
		}}
		status := Inspect(context.Background(), Options{Platform: platform.For("windows", "amd64"), Runner: runner})
		if !status.Installed || status.PackageFamily != strings.Split(appID, "!")[0] || status.Version != nil {
			t.Fatalf("appID=%q status = %#v", appID, status)
		}
		if status.InspectionUnavailable == nil {
			t.Fatalf("appID=%q fallback did not explain unavailable version metadata", appID)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("appID=%q queries = %#v", appID, runner.calls)
		}
	}
}

func TestParseMountPointKeepsSpaces(t *testing.T) {
	got, err := parseMountPoint("/dev/disk4s1\tApple_HFS\t/Volumes/ChatGPT Installer\n")
	if err != nil || got != "/Volumes/ChatGPT Installer" {
		t.Fatalf("mount point = %q, err=%v", got, err)
	}
}

func TestMacDownloadURLFollowsArchitectureAndOverride(t *testing.T) {
	if got := macDownloadURL(Options{Platform: platform.For("macos", "amd64")}); got != MacDownloadURLX64 {
		t.Fatalf("x64 URL = %q", got)
	}
	if got := macDownloadURL(Options{Platform: platform.For("macos", "arm64")}); got != MacDownloadURL {
		t.Fatalf("arm64 URL = %q", got)
	}
	if got := macDownloadURL(Options{Platform: platform.For("macos", "amd64"), DownloadURL: "https://mirror.test/app.dmg"}); got != "https://mirror.test/app.dmg" {
		t.Fatalf("override URL = %q", got)
	}
}

func TestMacInstallStopsOnDownloadExitCode(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{ExitCode: 22, Stderr: "not found"}}}
	_, err := Install(context.Background(), Options{
		Platform:    platform.For("macos", "arm64"),
		SearchRoots: []string{t.TempDir()},
		Runner:      runner,
	})
	if err == nil || !strings.Contains(err.Error(), "download ChatGPT installer") {
		t.Fatalf("install error = %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "/usr/bin/curl" {
		t.Fatalf("installer calls = %#v", runner.calls)
	}
}

func TestWindowsInstallOpensOfficialBootstrapperWithoutFilesystemScan(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{ExitCode: 0}, {ExitCode: 0}}}
	result, err := Install(context.Background(), Options{Platform: platform.For("windows", "amd64"), Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "external-installer-opened" || len(runner.started) != 1 {
		t.Fatalf("result=%#v started=%#v", result, runner.started)
	}
	argv := strings.Join(runner.started[0], " ")
	if !strings.Contains(argv, "powershell.exe") || !strings.Contains(argv, WindowsInstallerURL) {
		t.Fatalf("bootstrapper argv = %q", argv)
	}
}

func TestWindowsInstallDoesNotOpenInstallerAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &cancelRunner{cancel: cancel}
	_, err := Install(ctx, Options{Platform: platform.For("windows", "amd64"), Runner: runner})
	if err == nil || runner.started {
		t.Fatalf("install error=%v, installer started=%v", err, runner.started)
	}
}

func TestCompareVersionHandlesMissingComponents(t *testing.T) {
	if compareVersion("26.10.0", "26.9.99.0") <= 0 {
		t.Fatal("26.10.0 should be newer")
	}
	if compareVersion("26.10.0.0", "26.10") != 0 {
		t.Fatal("missing components should compare as zero")
	}
}
