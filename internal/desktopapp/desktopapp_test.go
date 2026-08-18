package desktopapp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
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

type cancelAtEOFBody struct {
	cancel  context.CancelFunc
	content []byte
}

func (b *cancelAtEOFBody) Read(buffer []byte) (int, error) {
	if len(b.content) == 0 {
		return 0, io.EOF
	}
	written := copy(buffer, b.content)
	b.content = b.content[written:]
	if len(b.content) == 0 {
		b.cancel()
		return written, io.EOF
	}
	return written, nil
}

func (*cancelAtEOFBody) Close() error { return nil }

type cancellingDownloader struct {
	cancel  context.CancelFunc
	content []byte
}

func (d cancellingDownloader) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK, Body: &cancelAtEOFBody{cancel: d.cancel, content: d.content}, ContentLength: int64(len(d.content)), Request: request,
	}, nil
}

type routeDownloader struct {
	routes map[string][]byte
	hits   []string
}

func (d *routeDownloader) Do(request *http.Request) (*http.Response, error) {
	d.hits = append(d.hits, request.URL.String())
	body, ok := d.routes[request.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request,
	}, nil
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
	maps.Copy(copyEnvironment, environment)
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

func TestDesktopLifecycleRequiresAnExplicitKnownAgent(t *testing.T) {
	options := Options{Platform: platform.For("macos", "arm64")}
	status := Inspect(context.Background(), "", options)
	if status.ID != "" || status.InspectionUnavailable == nil {
		t.Fatalf("empty Agent ID status = %#v", status)
	}
	if _, err := Install(context.Background(), "", options); err == nil {
		t.Fatal("empty Agent ID install unexpectedly succeeded")
	}
	if err := Open(context.Background(), "", options); err == nil {
		t.Fatal("empty Agent ID open unexpectedly succeeded")
	}
}

func TestDSHURLUsesTheNPMMirrorPreference(t *testing.T) {
	mac := Options{Platform: platform.For("macos", "arm64")}
	mac.PreferMirror = true
	got, err := dshURL(context.Background(), mac)
	if err != nil || got != DSHDesktopMacMirrorURL {
		t.Fatalf("mirror dsh URL = %q, %v", got, err)
	}
	win := Options{Platform: platform.For("windows", "amd64"), PreferMirror: true}
	got, err = dshURL(context.Background(), win)
	if err != nil || got != DSHDesktopWinMirrorURL {
		t.Fatalf("windows mirror dsh URL = %q, %v", got, err)
	}
}

func TestDesktopDefinitionsExposeIndependentProducts(t *testing.T) {
	definitions := Definitions()
	// DSH Desktop leads the list because the UI renders it in this order.
	if len(definitions) < 4 || definitions[0].ID != DSHDesktopID || definitions[1].ID != ClaudeDesktopID || definitions[2].ID != ChatGPTDesktopID || definitions[3].ID != WorkBuddyID {
		t.Fatalf("desktop definitions = %#v", definitions)
	}
	// Only the third-party build carries the flag; claiming it for a vendor's own
	// app would put a false disclaimer on the row.
	dsh, ok := DefinitionFor(DSHDesktopID)
	if !ok || !dsh.Unofficial {
		t.Fatalf("DSH definition = %#v, found=%v", dsh, ok)
	}
	if chatGPT, ok := DefinitionFor(ChatGPTDesktopID); !ok || chatGPT.Unofficial {
		t.Fatalf("ChatGPT must not be marked unofficial: %#v, found=%v", chatGPT, ok)
	}
	chatGPT, ok := DefinitionFor(ChatGPTDesktopID)
	if !ok || chatGPT.ProfileAgentID != CodexAgentID || chatGPT.SharedConfigAgentID != CodexAgentID {
		t.Fatalf("ChatGPT definition = %#v, found=%v", chatGPT, ok)
	}
	workBuddy, ok := DefinitionFor(WorkBuddyID)
	if !ok || workBuddy.ProfileAgentID != WorkBuddyID || workBuddy.Protocol != "openai" {
		t.Fatalf("WorkBuddy definition = %#v, found=%v", workBuddy, ok)
	}
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
	status := Inspect(context.Background(), ChatGPTDesktopID, Options{
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
	direct := Inspect(context.Background(), ChatGPTDesktopID, Options{
		Platform:    platform.For("darwin", "arm64"),
		SearchRoots: []string{appPath},
		Runner:      &probeRunner{macValues: runner.macValues},
	})
	if !direct.Installed || direct.Path != appPath {
		t.Fatalf("direct app root status = %#v", direct)
	}
}

func TestInspectWorkBuddyMacOSUsesItsOwnBundleIdentity(t *testing.T) {
	root := t.TempDir()
	appPath := makeBundle(t, root, "WorkBuddy.app")
	if err := os.WriteFile(filepath.Join(appPath, "Contents", "Info.plist"), []byte("plist"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := Inspect(context.Background(), WorkBuddyID, Options{
		Platform: platform.For("darwin", "arm64"), SearchRoots: []string{root},
		Runner: &probeRunner{macValues: map[string]string{
			"CFBundleIdentifier": WorkBuddyBundleID, "CFBundleShortVersionString": "5.3.8",
		}},
	})
	if !status.Installed || status.ID != WorkBuddyID || status.Path != appPath || status.Version == nil || *status.Version != "5.3.8" || status.Source != SourceMacOSZIP {
		t.Fatalf("WorkBuddy status = %#v", status)
	}
}

func TestInspectMacOSDoesNotTraverseApplicationRoots(t *testing.T) {
	root := t.TempDir()
	nested := makeBundle(t, filepath.Join(root, "nested"), "ChatGPT.app")
	if err := os.WriteFile(filepath.Join(nested, "Contents", "Info.plist"), []byte("plist"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &probeRunner{macValues: map[string]string{"CFBundleIdentifier": CodexBundleID}}
	status := Inspect(context.Background(), ChatGPTDesktopID, Options{Platform: platform.For("macos", "x64"), SearchRoots: []string{root}, Runner: runner})
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
	status := Inspect(context.Background(), ChatGPTDesktopID, Options{Platform: platform.For("windows", "amd64"), Runner: runner})
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
		status := Inspect(context.Background(), ChatGPTDesktopID, Options{Platform: platform.For("windows", "amd64"), Runner: runner})
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
	if got := chatGPTMacDownloadURL(Options{Platform: platform.For("macos", "amd64")}); got != ChatGPTMacDownloadURLX64 {
		t.Fatalf("x64 URL = %q", got)
	}
	if got := chatGPTMacDownloadURL(Options{Platform: platform.For("macos", "arm64")}); got != ChatGPTMacDownloadURL {
		t.Fatalf("arm64 URL = %q", got)
	}
	if got := chatGPTMacDownloadURL(Options{Platform: platform.For("macos", "amd64"), DownloadURL: "https://mirror.test/app.dmg"}); got != "https://mirror.test/app.dmg" {
		t.Fatalf("override URL = %q", got)
	}
}

func TestWorkBuddyPlatformMapping(t *testing.T) {
	tests := []struct {
		osID string
		arch string
		want string
	}{
		{"macos", "arm64", "workbuddy-darwin-arm64"},
		{"macos", "x64", "workbuddy-darwin-x64"},
		{"windows", "x64", WorkBuddyWindowsPlatform},
		{"windows", "arm64", WorkBuddyWindowsPlatform},
	}
	for _, test := range tests {
		got, err := workBuddyPlatform(test.osID, test.arch)
		if err != nil || got != test.want {
			t.Fatalf("workBuddyPlatform(%q, %q) = %q, %v", test.osID, test.arch, got, err)
		}
	}
}

func TestWorkBuddyWindowsInstallUsesSharedPlatformManifestAndSignatureVerification(t *testing.T) {
	payload := []byte("signed WorkBuddy installer")
	downloadURL := "https://download.codebuddy.cn/workbuddy/WorkBuddy.exe"
	manifestURL := WorkBuddyUpdateEndpoint + "?platform=" + WorkBuddyWindowsPlatform
	manifest := fmt.Appendf(nil, `{"version":"5.4.0","url":%q,"sha256hash":"stale-vendor-metadata"}`, downloadURL)
	downloader := &routeDownloader{routes: map[string][]byte{manifestURL: manifest, downloadURL: payload}}
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0, Stdout: `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Tencent Technology (Shenzhen) Company Limited","Organization":"Tencent Technology (Shenzhen) Company Limited","Subject":"CN=Tencent Technology (Shenzhen) Company Limited, O=Tencent Technology (Shenzhen) Company Limited","Issuer":"CN=Trusted CA"}`},
	}}
	var outputs []process.Output
	result, err := Install(context.Background(), WorkBuddyID, Options{
		Platform: platform.For("windows", "arm64"), Runner: runner, Downloader: downloader,
		Output: func(output process.Output) { outputs = append(outputs, output) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "installer-started" || len(runner.started) != 1 || len(downloader.hits) != 2 || downloader.hits[0] != manifestURL || downloader.hits[1] != downloadURL {
		t.Fatalf("result=%#v starts=%#v downloads=%#v", result, runner.started, downloader.hits)
	}
	defer os.Remove(runner.started[0][0])
	data, readErr := os.ReadFile(runner.started[0][0])
	if readErr != nil || !bytes.Equal(data, payload) {
		t.Fatalf("downloaded WorkBuddy installer = %q, %v", data, readErr)
	}
	progressFound := false
	for _, output := range outputs {
		progressFound = progressFound || output.Kind == "progress" && output.Target == WorkBuddyID
	}
	if !progressFound {
		t.Fatalf("WorkBuddy progress = %#v", outputs)
	}
}

func TestWorkBuddyUpdateRejectsUnapprovedDownloadHost(t *testing.T) {
	manifestURL := WorkBuddyUpdateEndpoint + "?platform=workbuddy-darwin-arm64"
	downloader := &routeDownloader{routes: map[string][]byte{
		manifestURL: []byte(`{"url":"https://example.test/WorkBuddy.zip"}`),
	}}
	_, err := fetchWorkBuddyUpdate(context.Background(), workBuddyCN, Options{
		Platform: platform.For("macos", "arm64"), Downloader: downloader,
	})
	if err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("fetchWorkBuddyUpdate() error = %v", err)
	}
}

func TestMacInstallStopsOnDownloadHTTPError(t *testing.T) {
	runner := &scriptedRunner{}
	downloader := &fakeDownloader{status: http.StatusNotFound}
	_, err := Install(context.Background(), ChatGPTDesktopID, Options{
		Platform:    platform.For("macos", "arm64"),
		SearchRoots: []string{t.TempDir()},
		Runner:      runner,
		Downloader:  downloader,
	})
	if err == nil || !strings.Contains(err.Error(), "download ChatGPT installer") {
		t.Fatalf("install error = %v", err)
	}
	if len(downloader.hits) != 1 || downloader.hits[0] != ChatGPTMacDownloadURL || len(runner.calls) != 0 {
		t.Fatalf("download hits=%#v installer calls=%#v", downloader.hits, runner.calls)
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
	result, err := Install(context.Background(), ChatGPTDesktopID, Options{
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
	if len(downloader.hits) != 1 || downloader.hits[0] != ChatGPTWindowsInstallerURL {
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
	if last.Target != ChatGPTDesktopID || last.Received != int64(len(payload)) || last.Total != int64(len(payload)) {
		t.Fatalf("final progress = %#v", last)
	}
}

func TestWindowsInstallDoesNotOpenInstallerAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &cancelRunner{cancel: cancel}
	_, err := Install(ctx, ChatGPTDesktopID, Options{Platform: platform.For("windows", "amd64"), Runner: runner})
	if err == nil || runner.started {
		t.Fatalf("install error=%v, installer started=%v", err, runner.started)
	}
}

func TestDownloadFileDeletesTheFileWhenCancellationWinsAtEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	destination := filepath.Join(t.TempDir(), "installer.exe")
	content := []byte("partial desktop installer")
	err := downloadFile(ctx, Options{Downloader: cancellingDownloader{cancel: cancel, content: content}}, "https://example.test/installer", destination, ChatGPTDesktopID)
	if err != context.Canceled {
		t.Fatalf("cancelled download error = %v", err)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled download left %s behind: %v", destination, statErr)
	}
}

func TestVerifyWindowsInstallerRequiresValidMicrosoftAuthenticode(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{
		ExitCode: 0,
		Stdout:   `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Microsoft Corporation","Organization":"Microsoft Corporation","Subject":"CN=Microsoft Corporation, O=Microsoft Corporation","Issuer":"CN=Microsoft Marketplace CA G 024"}`,
	}}}

	if err := verifyChatGPTWindowsInstaller(context.Background(), Options{Runner: runner}, `C:\Users\test\ChatGPT.exe`); err != nil {
		t.Fatalf("verifyChatGPTWindowsInstaller() error = %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "powershell.exe" {
		t.Fatalf("verification calls = %#v", runner.calls)
	}
}

func TestVerifyWindowsInstallerRejectsInvalidAuthenticodeStates(t *testing.T) {
	for _, status := range []string{"NotSigned", "HashMismatch", "NotTrusted", "UnknownError"} {
		t.Run(status, func(t *testing.T) {
			runner := &scriptedRunner{results: []process.Result{{
				ExitCode: 0,
				Stdout:   `{"Status":"` + status + `","StatusMessage":"signature failure","Publisher":"Microsoft Corporation","Organization":"Microsoft Corporation","Subject":"CN=Microsoft Corporation","Issuer":"CN=Microsoft Marketplace CA G 024"}`,
			}}}

			err := verifyChatGPTWindowsInstaller(context.Background(), Options{Runner: runner}, `C:\Users\test\ChatGPT.exe`)
			if err == nil || !strings.Contains(err.Error(), status) {
				t.Fatalf("verifyChatGPTWindowsInstaller() error = %v, want status %q rejection", err, status)
			}
		})
	}
}

func TestVerifyWindowsInstallerRejectsMissingSignerCertificate(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{
		ExitCode: 0,
		Stdout:   `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"","Organization":"","Subject":"","Issuer":""}`,
	}}}

	err := verifyChatGPTWindowsInstaller(context.Background(), Options{Runner: runner}, `C:\Users\test\ChatGPT.exe`)
	if err == nil || !strings.Contains(err.Error(), "signer certificate") {
		t.Fatalf("verifyChatGPTWindowsInstaller() error = %v, want missing signer certificate rejection", err)
	}
}

func TestVerifyWindowsInstallerRejectsUnexpectedPublisher(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{
		ExitCode: 0,
		Stdout:   `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Example Corporation","Organization":"Example Corporation","Subject":"CN=Example Corporation, O=Example Corporation","Issuer":"CN=Example CA"}`,
	}}}

	err := verifyChatGPTWindowsInstaller(context.Background(), Options{Runner: runner}, `C:\Users\test\ChatGPT.exe`)
	if err == nil || !strings.Contains(err.Error(), "publisher") {
		t.Fatalf("verifyChatGPTWindowsInstaller() error = %v, want publisher rejection", err)
	}
}

func TestWindowsInstallDoesNotStartUnsignedInstaller(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0},
		{ExitCode: 0, Stdout: `{"Status":"NotSigned","StatusMessage":"The file is not digitally signed.","Publisher":"","Organization":"","Subject":"","Issuer":""}`},
	}}
	downloader := &fakeDownloader{body: []byte("unsigned installer")}

	_, err := Install(context.Background(), ChatGPTDesktopID, Options{
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

	_, err := Install(context.Background(), ChatGPTDesktopID, Options{
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

	if err := verifyChatGPTMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/ChatGPT.app"); err != nil {
		t.Fatalf("verifyChatGPTMacOSApp() error = %v", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("verification calls = %#v, want codesign verify, codesign details, spctl", runner.calls)
	}
}

func TestVerifyWorkBuddyMacOSAppRequiresExpectedTeamAndNotarization(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0, Stderr: strings.Join([]string{
			"Identifier=" + WorkBuddyBundleID,
			"Authority=Developer ID Application: Tencent Technology (Shenzhen) Company Limited (" + WorkBuddyMacTeamID + ")",
			"TeamIdentifier=" + WorkBuddyMacTeamID,
		}, "\n")},
		{ExitCode: 0, Stdout: "WorkBuddy.app: accepted\nsource=Notarized Developer ID"},
	}}
	if err := verifyWorkBuddyMacOSApp(context.Background(), workBuddyCN, Options{Runner: runner}, "/Applications/WorkBuddy.app"); err != nil {
		t.Fatalf("verifyWorkBuddyMacOSApp() error = %v", err)
	}
}

func TestVerifyMacOSAppRejectsIdentityInjectedByExecutablePath(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0, Stderr: strings.Join([]string{
			"Executable=/Applications/Identifier=" + CodexBundleID + " TeamIdentifier=" + MacExpectedTeamID + " Authority=" + MacExpectedAuthority + ".app/Contents/MacOS/Fake",
			"Identifier=com.example.fake",
			"Authority=Developer ID Application: Example (EXAMPLETEAM)",
			"TeamIdentifier=EXAMPLETEAM",
		}, "\n")},
		{ExitCode: 0, Stdout: "Fake.app: accepted\nsource=Notarized Developer ID"},
	}}

	err := verifyChatGPTMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/Identifier="+CodexBundleID+" TeamIdentifier="+MacExpectedTeamID+".app")

	if err == nil {
		t.Fatal("verifyChatGPTMacOSApp() accepted identity values echoed from the executable path")
	}
	if len(runner.calls) != 2 {
		t.Fatalf("verification continued after identity failure: %#v", runner.calls)
	}
}

func TestVerifyMacOSAppRejectsMatchingNonLeafAuthority(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{
		{ExitCode: 0},
		{ExitCode: 0, Stderr: strings.Join([]string{
			"Executable=/Applications/Fake",
			"Authority=" + MacExpectedAuthority,
			".app/Contents/MacOS/Fake",
			"Identifier=" + CodexBundleID,
			"Authority=Developer ID Application: Example (EXAMPLETEAM)",
			"Authority=" + MacExpectedAuthority,
			"TeamIdentifier=" + MacExpectedTeamID,
		}, "\n")},
		{ExitCode: 0, Stdout: "Fake.app: accepted\nsource=Notarized Developer ID"},
	}}

	err := verifyChatGPTMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/Fake.app")

	if err == nil || !strings.Contains(err.Error(), "Authority") {
		t.Fatalf("verifyChatGPTMacOSApp() error = %v, want leaf Authority failure", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("verification continued after authority failure: %#v", runner.calls)
	}
}

func TestVerifyMacOSAppRejectsUnsignedBundleBeforeIdentityChecks(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{ExitCode: 1, Stderr: "code object is not signed at all"}}}

	err := verifyChatGPTMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/ChatGPT.app")

	if err == nil || !strings.Contains(err.Error(), "code signature") {
		t.Fatalf("verifyChatGPTMacOSApp() error = %v, want code-signature failure", err)
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
		{name: "official", url: ChatGPTMacDownloadURL, want: true},
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

	err := verifyChatGPTMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/ChatGPT.app")

	if err == nil || !strings.Contains(err.Error(), "TeamIdentifier") {
		t.Fatalf("verifyChatGPTMacOSApp() error = %v, want TeamIdentifier failure", err)
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
		{ExitCode: 0, Stdout: "/Applications/source=Notarized Developer ID.app: accepted\nsource=Developer ID"},
	}}

	err := verifyChatGPTMacOSApp(context.Background(), Options{Runner: runner}, "/Applications/ChatGPT.app")

	if err == nil || !strings.Contains(err.Error(), "notarized Developer ID") {
		t.Fatalf("verifyChatGPTMacOSApp() error = %v, want notarization failure", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("verification continued after Gatekeeper failure: %#v", runner.calls)
	}
}

func TestVerifyWindowsInstallerPassesPathViaEnvironment(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{
		ExitCode: 0,
		Stdout:   `{"Status":"Valid","StatusMessage":"Signature verified.","Publisher":"Microsoft Corporation","Organization":"Microsoft Corporation","Subject":"CN=Microsoft Corporation, O=Microsoft Corporation","Issuer":"CN=Microsoft Marketplace CA G 024"}`,
	}}}
	installerPath := `C:\Users\test with spaces\ChatGPT's installer.exe`

	if err := verifyChatGPTWindowsInstaller(context.Background(), Options{Runner: runner}, installerPath); err != nil {
		t.Fatalf("verifyChatGPTWindowsInstaller() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("verification calls = %#v", runner.calls)
	}
	argv := runner.calls[0]
	// powershell -Command treats trailing tokens as command text, not $args, so
	// the installer path must never be appended to argv.
	if argv[len(argv)-1] != argv[4] || len(argv) != 5 {
		t.Fatalf("installer path appended to powershell argv: %#v", argv)
	}
	script := argv[4]
	if !strings.Contains(script, "$env:BOOTAGENT_VERIFY_PATH") {
		t.Fatalf("verification script does not read the path environment variable: %q", script)
	}
	if got := runner.environments[0]["BOOTAGENT_VERIFY_PATH"]; got != installerPath {
		t.Fatalf("BOOTAGENT_VERIFY_PATH = %q, want %q", got, installerPath)
	}
}

func TestWorkBuddyWindowsVersionPassesPathViaEnvironment(t *testing.T) {
	runner := &scriptedRunner{results: []process.Result{{ExitCode: 0, Stdout: "5.3.11\n"}}}
	path := `C:\Users\test\AppData\Local\Programs\WorkBuddy\WorkBuddy.exe`

	version := workBuddyWindowsVersion(context.Background(), Options{Runner: runner}, path)
	if version == nil || *version != "5.3.11" {
		t.Fatalf("workBuddyWindowsVersion() = %v, want 5.3.11", version)
	}
	argv := runner.calls[0]
	if len(argv) != 5 {
		t.Fatalf("version query appended arguments to powershell argv: %#v", argv)
	}
	if !strings.Contains(argv[4], "$env:BOOTAGENT_VERSION_PATH") {
		t.Fatalf("version script does not read the path environment variable: %q", argv[4])
	}
	if got := runner.environments[0]["BOOTAGENT_VERSION_PATH"]; got != path {
		t.Fatalf("BOOTAGENT_VERSION_PATH = %q, want %q", got, path)
	}
}

func TestWorkBuddyStartAppsQueryPassesNameViaEnvironment(t *testing.T) {
	argv, environment := workBuddyStartAppsQuery(workBuddyIntl)
	if len(argv) != 5 {
		t.Fatalf("start-apps query argv = %#v", argv)
	}
	script := argv[4]
	if strings.Contains(script, "WorkBuddy") {
		t.Fatalf("display name interpolated into the script: %q", script)
	}
	if !strings.Contains(script, "$env:BOOTAGENT_APP_NAME") {
		t.Fatalf("start-apps script does not read the name environment variable: %q", script)
	}
	expectedName := strings.TrimSuffix(workBuddyIntl.appName, ".app")
	if got := environment["BOOTAGENT_APP_NAME"]; got != expectedName {
		t.Fatalf("BOOTAGENT_APP_NAME = %q, want %q", got, expectedName)
	}
}
