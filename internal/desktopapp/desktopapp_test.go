package desktopapp

import (
	"bytes"
	"context"
	"io"
	"net/http"
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
	results      []process.Result
	calls        [][]string
	environments []map[string]string
	started      [][]string
}

type cancelRunner struct {
	cancel  context.CancelFunc
	started bool
}

type fakeDownloader struct {
	body   []byte
	status int
	hits   []string
}

func (d *fakeDownloader) Do(request *http.Request) (*http.Response, error) {
	d.hits = append(d.hits, request.URL.String())
	status := d.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode:    status,
		Body:          io.NopCloser(bytes.NewReader(d.body)),
		ContentLength: int64(len(d.body)),
		Request:       request,
	}, nil
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

func (r *scriptedRunner) Run(_ context.Context, argv []string, environment map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	copyEnvironment := make(map[string]string, len(environment))
	for key, value := range environment {
		copyEnvironment[key] = value
	}
	r.environments = append(r.environments, copyEnvironment)
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
	status := Inspect(context.Background(), Options{Platform: platform.For("macos", "x64"), SearchRoots: []string{root}, Runner: runner})
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

func TestMacInstallStopsOnDownloadHTTPError(t *testing.T) {
	runner := &scriptedRunner{}
	downloader := &fakeDownloader{status: http.StatusNotFound}
	_, err := Install(context.Background(), Options{
		Platform:    platform.For("macos", "arm64"),
		SearchRoots: []string{t.TempDir()},
		Runner:      runner,
		Downloader:  downloader,
	})
	if err == nil || !strings.Contains(err.Error(), "download ChatGPT installer") {
		t.Fatalf("install error = %v", err)
	}
	if len(downloader.hits) != 1 || downloader.hits[0] != MacDownloadURL || len(runner.calls) != 0 {
		t.Fatalf("download hits=%#v installer calls=%#v", downloader.hits, runner.calls)
	}
}

func TestMacOpenInstallerDownloadsInsteadOfOpeningBrowser(t *testing.T) {
	runner := &scriptedRunner{}
	downloader := &fakeDownloader{status: http.StatusNotFound}
	_, err := OpenInstaller(context.Background(), Options{
		Platform:    platform.For("macos", "arm64"),
		SearchRoots: []string{t.TempDir()},
		Runner:      runner,
		Downloader:  downloader,
	})
	if err == nil || !strings.Contains(err.Error(), "download ChatGPT installer") {
		t.Fatalf("installer error = %v", err)
	}
	if len(downloader.hits) != 1 || len(runner.calls) != 0 || len(runner.started) != 0 {
		t.Fatalf("download hits=%#v installer calls=%#v started=%#v", downloader.hits, runner.calls, runner.started)
	}
}

func TestWindowsInstallDownloadsAndStartsOfficialBootstrapperWithoutFilesystemScan(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0},
		{ExitCode: 0, Stdout: `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Microsoft Corporation","Organization":"Microsoft Corporation","Subject":"CN=Microsoft Corporation, O=Microsoft Corporation","Issuer":"CN=Microsoft Marketplace CA G 024"}`},
	}}
	payload := []byte("official installer")
	downloader := &fakeDownloader{body: payload}
	var outputs []process.Output
	result, err := Install(context.Background(), Options{
		Platform:   platform.For("windows", "amd64"),
		Runner:     runner,
		Downloader: downloader,
		Output:     func(output process.Output) { outputs = append(outputs, output) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "installer-started" || len(runner.started) != 1 {
		t.Fatalf("result=%#v started=%#v", result, runner.started)
	}
	defer os.Remove(runner.started[0][0])
	if len(downloader.hits) != 1 || downloader.hits[0] != WindowsInstallerURL {
		t.Fatalf("download hits = %#v", downloader.hits)
	}
	downloaded, readErr := os.ReadFile(runner.started[0][0])
	if readErr != nil || !bytes.Equal(downloaded, payload) {
		t.Fatalf("downloaded installer = %q, %v", downloaded, readErr)
	}
	if len(runner.started[0]) != 1 || !strings.HasSuffix(strings.ToLower(runner.started[0][0]), ".exe") || strings.Contains(runner.started[0][0], "https://") {
		t.Fatalf("bootstrapper argv = %#v", runner.started[0])
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "WindowsApps") {
			t.Fatalf("Windows install must not scan WindowsApps: %#v", runner.calls)
		}
	}
	var progress []process.Output
	for _, output := range outputs {
		if output.Kind == "progress" {
			progress = append(progress, output)
		}
	}
	if len(progress) == 0 {
		t.Fatal("desktop installer download reported no progress")
	}
	last := progress[len(progress)-1]
	if last.Target != ID || last.Received != int64(len(payload)) || last.Total != int64(len(payload)) {
		t.Fatalf("final progress = %#v", last)
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

func TestVerifyWindowsInstallerRequiresValidMicrosoftAuthenticode(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{
		ExitCode: 0,
		Stdout:   `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Microsoft Corporation","Organization":"Microsoft Corporation","Subject":"CN=Microsoft Corporation, O=Microsoft Corporation","Issuer":"CN=Microsoft Marketplace CA G 024"}`,
	}}}

	if err := verifyWindowsInstaller(context.Background(), Options{Runner: runner}, `C:\Users\test\ChatGPT.exe`); err != nil {
		t.Fatalf("verifyWindowsInstaller() error = %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "powershell.exe" {
		t.Fatalf("verification calls = %#v", runner.calls)
	}
	if got := runner.environments[0]["ONEAGENT_AUTHENTICODE_PATH"]; got != `C:\Users\test\ChatGPT.exe` {
		t.Fatalf("Authenticode path environment = %q", got)
	}
}

func TestVerifyWindowsInstallerRejectsInvalidAuthenticodeStates(t *testing.T) {
	for _, status := range []string{"NotSigned", "HashMismatch", "NotTrusted", "UnknownError"} {
		t.Run(status, func(t *testing.T) {
			runner := &scriptedRunner{results: []process.Result{{
				ExitCode: 0,
				Stdout:   `{"Status":"` + status + `","StatusMessage":"signature failure","Publisher":"Microsoft Corporation","Organization":"Microsoft Corporation","Subject":"CN=Microsoft Corporation","Issuer":"CN=Microsoft Marketplace CA G 024"}`,
			}}}

			err := verifyWindowsInstaller(context.Background(), Options{Runner: runner}, `C:\Users\test\ChatGPT.exe`)
			if err == nil || !strings.Contains(err.Error(), status) {
				t.Fatalf("verifyWindowsInstaller() error = %v, want status %q rejection", err, status)
			}
		})
	}
}

func TestVerifyWindowsInstallerRejectsMissingSignerCertificate(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{
		ExitCode: 0,
		Stdout:   `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"","Organization":"","Subject":"","Issuer":""}`,
	}}}

	err := verifyWindowsInstaller(context.Background(), Options{Runner: runner}, `C:\Users\test\ChatGPT.exe`)
	if err == nil || !strings.Contains(err.Error(), "signer certificate") {
		t.Fatalf("verifyWindowsInstaller() error = %v, want missing signer certificate rejection", err)
	}
}

func TestVerifyWindowsInstallerRejectsUnexpectedPublisher(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{
		ExitCode: 0,
		Stdout:   `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Example Corporation","Organization":"Example Corporation","Subject":"CN=Example Corporation, O=Example Corporation","Issuer":"CN=Example CA"}`,
	}}}

	err := verifyWindowsInstaller(context.Background(), Options{Runner: runner}, `C:\Users\test\ChatGPT.exe`)
	if err == nil || !strings.Contains(err.Error(), "publisher") {
		t.Fatalf("verifyWindowsInstaller() error = %v, want publisher rejection", err)
	}
}

func TestWindowsInstallDoesNotStartUnsignedInstaller(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0},
		{ExitCode: 0, Stdout: `{"Status":"NotSigned","StatusMessage":"The file is not digitally signed.","Publisher":"","Organization":"","Subject":"","Issuer":""}`},
	}}
	downloader := &fakeDownloader{body: []byte("unsigned installer")}

	_, err := Install(context.Background(), Options{
		Platform:   platform.For("windows", "amd64"),
		Runner:     runner,
		Downloader: downloader,
	})
	if err == nil || !strings.Contains(err.Error(), "Authenticode") {
		t.Fatalf("Install() error = %v, want Authenticode rejection", err)
	}
	if len(runner.started) != 0 {
		t.Fatalf("unsigned installer was started: %#v", runner.started)
	}
}

func TestWindowsInstallRejectsUnapprovedDownloadHost(t *testing.T) {
	runner := &scriptedRunner{}
	downloader := &fakeDownloader{body: []byte("installer")}

	_, err := Install(context.Background(), Options{
		Platform:    platform.For("windows", "amd64"),
		Runner:      runner,
		Downloader:  downloader,
		DownloadURL: "https://example.test/installer.exe",
	})
	if err == nil || !strings.Contains(err.Error(), "validate ChatGPT installer URL") {
		t.Fatalf("Install() error = %v, want URL validation failure", err)
	}
	if len(downloader.hits) != 0 || len(runner.started) != 0 {
		t.Fatalf("unapproved URL was used: downloads=%#v starts=%#v", downloader.hits, runner.started)
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

func TestProfileAgentIDKeepsChatGPTOnCodexAndScopesOtherApps(t *testing.T) {
	if got := ProfileAgentID(ID); got != SharedConfigAgentID || !SharesProfile(ID) {
		t.Fatalf("ChatGPT profile mapping = %q, shared=%v", got, SharesProfile(ID))
	}
	if got := ProfileAgentID("  " + ID + " "); got != SharedConfigAgentID || !SharesProfile("  "+ID+" ") {
		t.Fatalf("trimmed ChatGPT profile mapping = %q, shared=%v", got, SharesProfile("  "+ID+" "))
	}
	if got := ProfileAgentID("workbuddy"); got != "workbuddy" || SharesProfile("workbuddy") {
		t.Fatalf("other desktop mapping = %q, shared=%v", got, SharesProfile("workbuddy"))
	}
	if SharesProfile("  workbuddy ") {
		t.Fatal("whitespace around a non-shared desktop ID changed its ownership")
	}
}

func TestVerifyMacOSAppRequiresExpectedTeamAndNotarization(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{
			ExitCode: 0,
			Stderr: strings.Join([]string{
				"Identifier=com.openai.codex",
				"Authority=Developer ID Application: OpenAI OpCo, LLC (2DC432GLL2)",
				"TeamIdentifier=2DC432GLL2",
			}, "\n"),
		},
		{ExitCode: 0, Stdout: "ChatGPT.app: accepted\nsource=Notarized Developer ID"},
	}}

	if err := verifyMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/ChatGPT.app"); err != nil {
		t.Fatalf("verifyMacOSApp() error = %v", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("verification calls = %#v, want codesign verify, codesign details, spctl", runner.calls)
	}
}

func TestVerifyMacOSAppRejectsUnsignedBundleBeforeIdentityChecks(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{ExitCode: 1, Stderr: "code object is not signed at all"}}}

	err := verifyMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/ChatGPT.app")

	if err == nil || !strings.Contains(err.Error(), "code signature") {
		t.Fatalf("verifyMacOSApp() error = %v, want code-signature failure", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("verification continued after signature failure: %#v", runner.calls)
	}
}

func TestApprovedDownloadURLRequiresHTTPSAndAllowedHost(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "official", url: MacDownloadURL, want: true},
		{name: "http", url: "http://persistent.oaistatic.com/codex-app-prod/ChatGPT.dmg", want: false},
		{name: "wrong host", url: "https://example.test/ChatGPT.dmg", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := approvedDownloadURL(test.url, "persistent.oaistatic.com")
			if (err == nil) != test.want {
				t.Fatalf("approvedDownloadURL(%q) error = %v, want allowed=%v", test.url, err, test.want)
			}
		})
	}
}

func TestVerifyMacOSAppRejectsWrongTeamID(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{
			ExitCode: 0,
			Stderr:   "Identifier=com.openai.codex\nAuthority=Developer ID Application: OpenAI OpCo, LLC (2DC432GLL2)\nTeamIdentifier=WRONGTEAM",
		},
	}}

	err := verifyMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/ChatGPT.app")

	if err == nil || !strings.Contains(err.Error(), "TeamIdentifier") {
		t.Fatalf("verifyMacOSApp() error = %v, want TeamIdentifier failure", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("verification continued after identity failure: %#v", runner.calls)
	}
}

func TestVerifyMacOSAppRejectsNonNotarizedDeveloperIDSource(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{
			ExitCode: 0,
			Stderr:   "Identifier=com.openai.codex\nAuthority=Developer ID Application: OpenAI OpCo, LLC (2DC432GLL2)\nTeamIdentifier=2DC432GLL2",
		},
		{ExitCode: 0, Stdout: "ChatGPT.app: accepted\nsource=Developer ID"},
	}}

	err := verifyMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/ChatGPT.app")

	if err == nil || !strings.Contains(err.Error(), "notarized Developer ID") {
		t.Fatalf("verifyMacOSApp() error = %v, want notarization failure", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("verification continued after Gatekeeper failure: %#v", runner.calls)
	}
}
