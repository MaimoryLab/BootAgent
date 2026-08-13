# OTA Updater Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a release-only GitHub Releases OTA flow that asks before downloading, exposes the download in the existing task center, supports cancellation, and restarts into the staged update only when the user requests it.

**Architecture:** Wails' `pkg/updater` owns release comparison, GitHub asset selection, checksum verification, archive extraction, staging, swapping, and relaunch. A narrow `internal/binding.UpdateService` adapts that concrete updater to three cancellable Wails methods. A frontend-only `AppUpdater` checks once at startup, uses `@wailsio/runtime`'s native `Dialogs.Question` for consent, and drives the existing task-center state and progress event pipeline.

**Tech Stack:** Go 1.26, Wails v3.0.0-beta.7 `pkg/updater`, GitHub Releases API, React 19, TypeScript, Vitest, pnpm, GitHub Actions.

---

## File Map

### Go

- Modify `internal/version/version.go`: add the release-version gate and remove the linker tag's leading `v` for Wails.
- Create `internal/version/version_test.go`: table-test development, tagged, and whitespace inputs.
- Create `internal/binding/update.go`: define the updater backend interface, Wails service methods, stable error conversion, and the OTA progress-to-install-output adapter.
- Create `internal/binding/update_test.go`: fake the three updater calls and cover disabled/current/new-release, cancellation, delegation, and progress payloads.
- Modify `internal/binding/services_test.go`: include `UpdateService` in the method allowlist.
- Modify `cmd/bootagent-desktop/main_wails.go`: initialise GitHub updater settings for non-development versions, register the update service, and bridge download progress to `bootagent:install-output`.
- Modify `go.mod` and `go.sum`: accept only the checksum/module changes required by Wails updater/binding generation (`go mod tidy`).

### Frontend

- Modify `frontend/src/backend/wails.ts`: import generated `UpdateService` and expose `checkUpdate`, `downloadUpdate`, and `restartUpdate`; export the stable progress target.
- Modify `frontend/src/backend/wails.test.ts`: mock the generated update service and test forwarding plus cancellation.
- Modify `frontend/src/state/TaskCenterContext.tsx`: add one optional terminal action and the two minimal setters needed to update that action/message after a request settles.
- Modify `frontend/src/components/TaskCenter.tsx`: render a terminal action button without nesting buttons and keep the existing cancel/dismiss controls.
- Modify `frontend/src/components/TaskCenter.test.tsx`: cover terminal action rendering and invocation.
- Modify `frontend/src/styles/app.css`: reserve a compact action row in a task card.
- Modify `frontend/src/i18n.tsx`: add the update-dialog, task-result, restart, and failure strings in the existing Chinese-first dictionary.
- Create `frontend/src/components/AppUpdater.tsx`: StrictMode-safe startup coordinator; native confirmation; task registration, cancellation, progress attribution, completion, and restart retry.
- Create `frontend/src/components/AppUpdater.test.tsx`: test the consent branches, one-check guard, cancellable download, ready action, and restart failure.
- Modify `frontend/src/App.tsx`: mount `AppUpdater` inside `TaskCenterProvider`.
- Regenerate `frontend/bindings/github.com/MaimoryLab/BootAgent/internal/binding/updateservice.ts` and the generated binding index through the existing task; never hand-edit generated files.

### Release and documentation

- Modify `.github/workflows/build-artifacts.yml`: trigger stable `vX.Y.Z` tags, retain the four-platform build matrix, create the four exact OTA zips, generate `SHA256SUMS`, and create/update the GitHub Release.
- Modify `README.md`: replace the stale manual-release description with the tag-triggered OTA asset description.

---

### Task 1: Version Gate

**Files:**
- Modify: `internal/version/version.go`
- Create: `internal/version/version_test.go`

- [ ] **Step 1: Write the failing table test**

~~~go
package version

import "testing"

func TestUpdaterVersion(t *testing.T) {
	original := Version
	defer func() { Version = original }()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "release with v", value: "v1.2.3", want: "1.2.3"},
		{name: "release without v", value: "1.2.3", want: "1.2.3"},
		{name: "development", value: "v0.0.0-dev", want: ""},
		{name: "development without v", value: "1.2.3-dev", want: ""},
		{name: "blank", value: "  ", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Version = test.value
			if got := UpdaterVersion(); got != test.want {
				t.Fatalf("UpdaterVersion() = %q, want %q", got, test.want)
			}
		})
	}
}
~~~

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `go test ./internal/version -run TestUpdaterVersion -count=1`

Expected: FAIL because `UpdaterVersion` is not defined.

- [ ] **Step 3: Implement the smallest gate**

~~~go
package version

import "strings"

// Version is replaced with the release version through Go linker flags.
var Version = "v0.0.0-dev"

// UpdaterVersion returns the Wails semver input. Development builds opt out of
// OTA so local runs never contact the release feed.
func UpdaterVersion() string {
	value := strings.TrimSpace(strings.TrimPrefix(Version, "v"))
	if value == "" || strings.HasSuffix(value, "-dev") {
		return ""
	}
	return value
}
~~~

- [ ] **Step 4: Run the focused test and verify it passes**

Run: `go test ./internal/version -run TestUpdaterVersion -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the isolated version change**

~~~bash
git add internal/version/version.go internal/version/version_test.go
git commit -m "feat: gate OTA on release versions"
~~~

### Task 2: Wails Update Service and Progress Adapter

**Files:**
- Create: `internal/binding/update.go`
- Create: `internal/binding/update_test.go`
- Modify: `internal/binding/services_test.go`

- [ ] **Step 1: Write the failing fake-backed service tests**

Create `internal/binding/update_test.go` with the following fake and tests:

~~~go
package binding

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

type updateBackendFake struct {
	checkCalls     int
	downloadCalls  int
	restartCalls   int
	release        *updater.Release
	checkErr       error
	downloadErr    error
	restartErr     error
	lastContext    context.Context
}

func (f *updateBackendFake) Check(ctx context.Context) (*updater.Release, error) {
	f.checkCalls++
	f.lastContext = ctx
	return f.release, f.checkErr
}

func (f *updateBackendFake) DownloadAndInstall(ctx context.Context) error {
	f.downloadCalls++
	f.lastContext = ctx
	return f.downloadErr
}

func (f *updateBackendFake) Restart(ctx context.Context) error {
	f.restartCalls++
	f.lastContext = ctx
	return f.restartErr
}

func TestUpdateServiceCheckDisabledAndCurrent(t *testing.T) {
	service := NewUpdateService(nil)
	if got, err := service.Check(context.Background()); err != nil || got != "" {
		t.Fatalf("disabled Check() = %q, %v", got, err)
	}

	fake := &updateBackendFake{}
	service = NewUpdateService(fake)
	if got, err := service.Check(context.Background()); err != nil || got != "" || fake.checkCalls != 1 {
		t.Fatalf("current Check() = %q, %v, calls=%d", got, err, fake.checkCalls)
	}
}

func TestUpdateServiceDelegatesReleaseDownloadAndRestart(t *testing.T) {
	fake := &updateBackendFake{release: &updater.Release{Version: "1.4.0"}}
	service := NewUpdateService(fake)
	ctx := context.WithValue(context.Background(), struct{}{}, "caller")

	if got, err := service.Check(ctx); err != nil || got != "1.4.0" {
		t.Fatalf("new release Check() = %q, %v", got, err)
	}
	if err := service.DownloadAndInstall(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.checkCalls != 1 || fake.downloadCalls != 1 || fake.restartCalls != 1 || fake.lastContext != ctx {
		t.Fatalf("backend calls = %#v", fake)
	}
}

func TestUpdateServiceRejectsCancelledRequestsBeforeDelegation(t *testing.T) {
	fake := &updateBackendFake{}
	service := NewUpdateService(fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.Check(ctx); err == nil {
		t.Fatal("cancelled Check() succeeded")
	}
	if err := service.DownloadAndInstall(ctx); err == nil {
		t.Fatal("cancelled DownloadAndInstall() succeeded")
	}
	if err := service.Restart(ctx); err == nil {
		t.Fatal("cancelled Restart() succeeded")
	}
	if fake.checkCalls != 0 || fake.downloadCalls != 0 || fake.restartCalls != 0 {
		t.Fatalf("cancelled request reached backend: %#v", fake)
	}
}

func TestUpdateServiceConvertsFailuresAndProgress(t *testing.T) {
	fake := &updateBackendFake{checkErr: errors.New("network detail")}
	service := NewUpdateService(fake)

	if _, err := service.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "Unable to check for updates") {
		t.Fatalf("check error = %v", err)
	}
	output, ok := UpdateProgressOutput(updater.Progress{Written: 12, Total: 30})
	if !ok || output.Kind != "progress" || output.Target != UpdateProgressTarget || output.Received != 12 || output.Total != 30 {
		t.Fatalf("progress output = %#v, ok=%v", output, ok)
	}
	if _, ok := UpdateProgressOutput(&updater.Progress{Written: 1, Total: 0}); !ok {
		t.Fatal("pointer progress payload was rejected")
	}
	if _, ok := UpdateProgressOutput(struct{}{}); ok {
		t.Fatal("unrelated payload was accepted")
	}
}

func TestUpdateServiceMethodAllowlist(t *testing.T) {
	typeOf := reflect.TypeOf(&UpdateService{})
	got := make([]string, 0, typeOf.NumMethod())
	for method := range typeOf.Methods() {
		got = append(got, method.Name)
	}
	want := []string{"Check", "DownloadAndInstall", "Restart"}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("methods = %v, want %v", got, want)
	}
}
~~~

- [ ] **Step 2: Run the focused tests and verify the expected red state**

Run: `go test ./internal/binding -run 'TestUpdateService|TestUpdateServiceMethodAllowlist' -count=1`

Expected: FAIL with undefined updater-service symbols.

- [ ] **Step 3: Implement the narrow service and event conversion**

~~~go
package binding

import (
	"context"
	"errors"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/process"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

const UpdateProgressTarget = "bootagent-update"

type UpdateBackend interface {
	Check(context.Context) (*updater.Release, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
}

type UpdateService struct {
	backend UpdateBackend
}

func NewUpdateService(backend UpdateBackend) *UpdateService {
	return &UpdateService{backend: backend}
}

func (s *UpdateService) Check(ctx context.Context) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if s == nil || s.backend == nil {
		return "", nil
	}
	release, err := s.backend.Check(ctx)
	if err != nil {
		return "", updateError("Unable to check for updates", err)
	}
	if release == nil {
		return "", nil
	}
	return release.Version, nil
}

func (s *UpdateService) DownloadAndInstall(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.backend == nil {
		return notReady("Update service is not configured")
	}
	if err := s.backend.DownloadAndInstall(ctx); err != nil {
		return updateError("Unable to download the BootAgent update", err)
	}
	return nil
}

func (s *UpdateService) Restart(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.backend == nil {
		return notReady("Update service is not configured")
	}
	if err := s.backend.Restart(ctx); err != nil {
		return updateError("Unable to restart BootAgent for update", err)
	}
	return nil
}

func UpdateProgressOutput(payload any) (process.Output, bool) {
	var progress updater.Progress
	switch value := payload.(type) {
	case updater.Progress:
		progress = value
	case *updater.Progress:
		if value == nil {
			return process.Output{}, false
		}
		progress = *value
	default:
		return process.Output{}, false
	}
	return process.Output{
		Kind: "progress", Target: UpdateProgressTarget,
		Received: progress.Written, Total: progress.Total,
	}, true
}

func updateError(message string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return oneerrors.New(oneerrors.Timeout, message+" was cancelled", oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	return oneerrors.New(oneerrors.InternalError, message, oneerrors.WithStatus(500), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
}
~~~

- [ ] **Step 4: Add the service to the existing allowlist test**

Extend the `tests` slice in `internal/binding/services_test.go` with:

~~~go
{&UpdateService{}, []string{"Check", "DownloadAndInstall", "Restart"}},
~~~

- [ ] **Step 5: Run Go tests and verify green**

Run: `go test ./internal/binding -run 'Test(UpdateService|ServiceMethodAllowlist)' -count=1`

Expected: PASS, including the existing service tests.

- [ ] **Step 6: Commit the backend service**

~~~bash
git add internal/binding/update.go internal/binding/update_test.go internal/binding/services_test.go
git commit -m "feat: expose the Wails updater service"
~~~

### Task 3: Wails Initialisation and Progress Wiring

**Files:**
- Modify: `cmd/bootagent-desktop/main_wails.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the release-only initialisation path**

Import these packages in `main_wails.go`:

~~~go
	"github.com/MaimoryLab/BootAgent/internal/version"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
~~~

Add this helper below `main`:

~~~go
func configureUpdater(appInstance *application.App) binding.UpdateBackend {
	current := version.UpdaterVersion()
	if current == "" {
		return nil
	}
	provider, err := github.New(github.Config{
		Repository:    "MaimoryLab/BootAgent",
		ChecksumAsset: "SHA256SUMS",
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
~~~

`WindowNone` is intentional: `CheckAndInstall` is not used because beta.3 starts downloading immediately. The frontend supplies the confirmation dialog.

- [ ] **Step 2: Register the service and progress listener before `Run`**

Immediately after the `application.New(...)` assignment and before the window creation block, add:

~~~go
	updateBackend := configureUpdater(appInstance)
	appInstance.Event.On(updater.EventDownloadProgress, func(event *application.CustomEvent) {
		if event == nil {
			return
		}
		output, ok := binding.UpdateProgressOutput(event.Data)
		if !ok {
			return
		}
		appInstance.Event.Emit("bootagent:install-output", output)
	})
	appInstance.RegisterService(application.NewServiceWithOptions(
		binding.NewUpdateService(updateBackend),
		application.ServiceOptions{MarshalError: oneerrors.Marshal},
	))
~~~

The existing `InstallOutput` callback stays unchanged; both ordinary downloads and OTA downloads now feed the same frontend event name. `RegisterService` is before `Run`, so the generated binding analyser still discovers the concrete service through `NewServiceWithOptions`.

- [ ] **Step 3: Tidy the module and inspect the dependency diff**

Run: `go mod tidy`

Expected: Wails' missing `golang.org/x/mod` checksum (and only the necessary `go.mod`/`go.sum` entries) is added. Do not add a second updater dependency.

- [ ] **Step 4: Generate bindings and verify the new service exists**

Run: `task generate:bindings`

Expected: generated `frontend/bindings/github.com/MaimoryLab/BootAgent/internal/binding/updateservice.ts` contains `Check`, `DownloadAndInstall`, and `Restart`; the generated binding index exports `UpdateService`. No generated file is edited by hand.

- [ ] **Step 5: Run the Wails-tag compile check**

Run: `go test -tags wails ./cmd/bootagent-desktop ./internal/binding -run '^$'`

Expected: compile succeeds without running native windows/macOS UI tests.

- [ ] **Step 6: Commit the wiring and generated bindings**

~~~bash
git add cmd/bootagent-desktop/main_wails.go go.mod go.sum frontend/bindings
git commit -m "feat: wire GitHub Releases into the desktop updater"
~~~
+

### Task 4: Frontend Wails Adapter

**Files:**
- Modify: frontend/src/backend/wails.ts
- Modify: frontend/src/backend/wails.test.ts
- Generated: frontend/bindings/github.com/MaimoryLab/BootAgent/internal/binding/updateservice.ts

- [ ] **Step 1: Write failing adapter tests**

Add the update bridge functions to the hoisted mock:

~~~ts
  updateCheck: vi.fn(),
  updateDownload: vi.fn(),
  updateRestart: vi.fn(),
~~~

Mock the generated module:

~~~ts
vi.mock("../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/updateservice.js", () => ({
  Check: bridge.updateCheck,
  DownloadAndInstall: bridge.updateDownload,
  Restart: bridge.updateRestart,
}));
~~~

Add this forwarding test:

~~~ts
it("forwards OTA calls", async () => {
  bridge.updateCheck.mockResolvedValue("1.4.0");
  bridge.updateDownload.mockResolvedValue(undefined);
  bridge.updateRestart.mockResolvedValue(undefined);

  await expect(wailsApi.checkUpdate()).resolves.toBe("1.4.0");
  await expect(wailsApi.downloadUpdate()).resolves.toBeUndefined();
  await expect(wailsApi.restartUpdate()).resolves.toBeUndefined();
  expect(bridge.updateCheck).toHaveBeenCalledWith();
  expect(bridge.updateDownload).toHaveBeenCalledWith();
  expect(bridge.updateRestart).toHaveBeenCalledWith();
});
~~~

Add the cancellation assertion using the existing runtime helper:

~~~ts
it("exposes the generated OTA download cancellation", async () => {
  const oncancelled = vi.fn();
  bridge.updateDownload.mockReturnValue(new CancellablePromise<void>(() => {}, oncancelled));
  const request = wailsApi.downloadUpdate();
  expect(typeof request.cancel).toBe("function");
  await request.cancel?.();
  expect(oncancelled).toHaveBeenCalledOnce();
});
~~~

- [ ] **Step 2: Run the focused tests and verify red**

Run: cd frontend && pnpm test -- src/backend/wails.test.ts

Expected: FAIL because the three OTA adapter methods are not defined.

- [ ] **Step 3: Add the adapter methods**

In frontend/src/backend/wails.ts, import UpdateService beside the other generated services and add:

~~~ts
export const OTA_PROGRESS_TARGET = "bootagent-update";

// inside wailsApi:
  checkUpdate: (): Promise<string> => call(() => UpdateService.Check()) as Promise<string>,
  downloadUpdate: (): CancellableRequest<void> =>
    call(() => UpdateService.DownloadAndInstall()) as CancellableRequest<void>,
  restartUpdate: (): Promise<void> => call(() => UpdateService.Restart()).then(() => undefined),
~~~

Keep these calls inside the existing call normalizer so structured Wails errors and generated cancellation errors retain the current frontend contract.

- [ ] **Step 4: Run the focused tests and typecheck**

Run: cd frontend && pnpm test -- src/backend/wails.test.ts && pnpm run typecheck

Expected: PASS and no TypeScript errors.

- [ ] **Step 5: Commit the adapter**

~~~bash
git add frontend/src/backend/wails.ts frontend/src/backend/wails.test.ts frontend/bindings
git commit -m "feat: expose OTA calls to the frontend"
~~~
+

### Task 5: Terminal Task Actions

**Files:**
- Modify: frontend/src/state/TaskCenterContext.tsx
- Modify: frontend/src/components/TaskCenter.tsx
- Modify: frontend/src/components/TaskCenter.test.tsx
- Modify: frontend/src/styles/app.css

- [ ] **Step 1: Write the failing task-action test**

Add this harness to TaskCenter.test.tsx:

~~~tsx
function TaskActionHarness({ action }: { action: () => void }) {
  const { startTask, finishTask, setTaskAction } = useTaskCenter();
  const id = taskKey("update", "bootagent-update");
  return (
    <>
      <button type="button" onClick={() => startTask({
        id, kind: "update", target: "bootagent-update", title: "更新 BootAgent", route: "/overview",
      })}>启动更新</button>
      <button type="button" onClick={() => finishTask(id, { kind: "success", message: "更新已下载" })}>完成更新</button>
      <button type="button" onClick={() => setTaskAction(id, { label: "重启并更新", run: action })}>添加操作</button>
    </>
  );
}
~~~

Add this complete helper beside the existing renderTaskCenter helper:

~~~tsx
import type { ReactNode } from "react";

function renderTaskCenterWith(children: ReactNode, initialEntry = "/overview") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <TaskCenterProvider>
        <TaskCenter />
        {children}
        <Routes>
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </TaskCenterProvider>
    </MemoryRouter>,
  );
}
~~~

Add this assertion:

~~~tsx
it("renders and invokes an optional terminal action", async () => {
  const action = vi.fn();
  const user = userEvent.setup();
  renderTaskCenterWith(<TaskActionHarness action={action} />);
  await user.click(screen.getByRole("button", { name: "启动更新" }));
  await user.click(screen.getByRole("button", { name: "完成更新" }));
  await user.click(screen.getByRole("button", { name: "添加操作" }));
  await user.click(screen.getByRole("button", { name: "重启并更新" }));
  expect(action).toHaveBeenCalledOnce();
});
~~~

- [ ] **Step 2: Run the focused test and verify red**

Run: cd frontend && pnpm test -- src/components/TaskCenter.test.tsx

Expected: FAIL because TaskAction, setTaskAction, and the action button do not exist.

- [ ] **Step 3: Extend the context with the minimum terminal-action state**

Replace the existing task type block with these complete definitions:

~~~tsx
export interface TaskAction {
  label: string;
  run: () => void | PromiseLike<void>;
}

export interface TaskInput {
  id?: string;
  kind: TaskKind;
  target: string;
  title: string;
  route: string;
  progressTarget?: string;
  group?: string;
  action?: TaskAction;
}

export interface TaskRecord extends TaskInput {
  id: string;
  progressTarget: string;
  state: TaskState;
  progress?: TaskProgress;
  message?: string;
  startedAt: number;
}
~~~

Add these methods to TaskCenterValue and the default context:

~~~tsx
setTaskAction: (id: string, action?: TaskAction) => void;
setTaskMessage: (id: string, message: string) => void;
~~~

Implement them beside finishTask:

~~~tsx
const setTaskAction = useCallback((id: string, action?: TaskAction) => {
  updateTasks((current) => current.map((task) => (
    task.id === id || task.target === id ? { ...task, action } : task
  )));
}, [updateTasks]);

const setTaskMessage = useCallback((id: string, message: string) => {
  updateTasks((current) => current.map((task) => (
    task.id === id || task.target === id ? { ...task, message } : task
  )));
}, [updateTasks]);
~~~

Include both callbacks in the provider value and dependency list. The existing finishTask spread preserves action, so a success or failure transition does not lose the restart retry.

- [ ] **Step 4: Render the action as a sibling button**

Import RefreshCw from lucide-react. Add has-action to the card class when the task has a terminal action, then render this after the main card button and before the dismiss button:

~~~tsx
{task.state !== "running" && task.action ? (
  <button
    type="button"
    className="task-card-action"
    onClick={() => { void task.action?.run(); }}
  >
    <RefreshCw size={13} aria-hidden="true" />
    <span>{task.action.label}</span>
  </button>
) : null}
~~~

This keeps the DOM valid: the action is never nested inside the existing main button.

- [ ] **Step 5: Add compact responsive styling**

Append these rules beside the existing task-card rules:

~~~css
.task-card.has-action { display: grid; grid-template-columns: minmax(0, 1fr); }
.task-card-action {
  min-height: 27px;
  margin: 0 30px 7px 10px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: 4px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  color: var(--text-primary);
  background: var(--window-bg);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.task-card-action:hover { background: var(--surface-pressed); }
~~~

- [ ] **Step 6: Run the focused tests and build**

Run: cd frontend && pnpm test -- src/components/TaskCenter.test.tsx && pnpm run typecheck

Expected: PASS; existing progress, cancellation, and dismissal tests stay green.

- [ ] **Step 7: Commit the task-center extension**

~~~bash
git add frontend/src/state/TaskCenterContext.tsx frontend/src/components/TaskCenter.tsx frontend/src/components/TaskCenter.test.tsx frontend/src/styles/app.css
git commit -m "feat: add terminal actions to task cards"
~~~
+

### Task 6: Startup Coordinator and Native Confirmation

**Files:**
- Create: frontend/src/components/AppUpdater.tsx
- Create: frontend/src/components/AppUpdater.test.tsx
- Modify: frontend/src/i18n.tsx
- Modify: frontend/src/App.tsx

- [ ] **Step 1: Write failing coordinator tests and runtime mocks**

Create AppUpdater.test.tsx with these mocks and a deferred promise helper:

~~~tsx
import { StrictMode } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const bridge = vi.hoisted(() => ({
  check: vi.fn(),
  download: vi.fn(),
  restart: vi.fn(),
  question: vi.fn(),
  onInstallOutput: vi.fn(() => vi.fn()),
}));

beforeEach(() => {
  vi.clearAllMocks();
  bridge.onInstallOutput.mockReturnValue(vi.fn());
});

vi.mock("@wailsio/runtime", () => ({
  Dialogs: { Question: bridge.question },
}));
vi.mock("../backend/api", () => ({
  api: {
    checkUpdate: bridge.check,
    downloadUpdate: bridge.download,
    restartUpdate: bridge.restart,
    onInstallOutput: bridge.onInstallOutput,
  },
  describeError: (error: unknown, fallback: string) => ({
    message: error instanceof Error ? error.message : fallback,
  }),
  isCancellationError: (error: unknown) => (
    error && typeof error === "object" && (error as { name?: unknown }).name === "CancelError"
  ),
  taskCanceller: (request: { cancel?: () => void }) => request.cancel ? () => request.cancel?.() : undefined,
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
}
~~~

Add the provider wrapper used by every test:

~~~tsx
import type { PropsWithChildren } from "react";
import { I18nProvider } from "../i18n";
import { TaskCenter } from "./TaskCenter";
import { TaskCenterProvider } from "../state/TaskCenterContext";
import { AppUpdater } from "./AppUpdater";

function TestProviders({ children }: PropsWithChildren) {
  return (
    <I18nProvider>
      <TaskCenterProvider>
        <TaskCenter />
        {children}
      </TaskCenterProvider>
    </I18nProvider>
  );
}
~~~

Render the component with TestProviders. Add cases that assert: StrictMode calls
Check once; Not now calls no download; Update creates a card and attaches/calls
cancel; a resolved request renders Restart and update; and a rejected restart
leaves that action available with the failure message.

The approved-download assertion should look for the versioned task title
Update BootAgent 1.4.0, then click the card's Cancel task control and assert the
fake request's cancel function was called exactly once.

Use these concrete terminal-action assertions:

~~~tsx
it("finishes with a restart action", async () => {
  bridge.check.mockResolvedValue("1.4.0");
  bridge.question.mockResolvedValue("更新");
  bridge.download.mockResolvedValue(undefined);
  bridge.restart.mockResolvedValue(undefined);
  const user = userEvent.setup();
  render(<AppUpdater />, { wrapper: TestProviders });
  await user.click(await screen.findByRole("button", { name: "重启并更新" }));
  expect(bridge.restart).toHaveBeenCalledOnce();
});

it("keeps restart available and reports a restart failure", async () => {
  bridge.check.mockResolvedValue("1.4.0");
  bridge.question.mockResolvedValue("更新");
  bridge.download.mockResolvedValue(undefined);
  bridge.restart.mockRejectedValue(new Error("restart failed"));
  const user = userEvent.setup();
  render(<AppUpdater />, { wrapper: TestProviders });
  const action = await screen.findByRole("button", { name: "重启并更新" });
  await user.click(action);
  expect(await screen.findByText("restart failed")).toBeTruthy();
  expect(screen.getByRole("button", { name: "重启并更新" })).toBeTruthy();
});
~~~

- [ ] **Step 2: Run the focused test and verify red**

Run: cd frontend && pnpm test -- src/components/AppUpdater.test.tsx

Expected: FAIL because AppUpdater and the new backend methods do not exist.

- [ ] **Step 3: Add the update strings**

Add these keys to the english dictionary in frontend/src/i18n.tsx:

~~~ts
  "BootAgent 更新": "BootAgent update",
  "发现 BootAgent 新版本 {version}，现在下载吗？": "BootAgent {version} is available. Download it now?",
  "暂不": "Not now",
  "更新 BootAgent": "Update BootAgent",
  "更新 BootAgent {version}": "Update BootAgent {version}",
  "更新已下载": "Update downloaded",
  "重启并更新": "Restart and update",
  "更新失败": "Update failed",
  "无法重启并更新": "Could not restart and update",
~~~

- [ ] **Step 4: Implement the one-shot coordinator**

Create frontend/src/components/AppUpdater.tsx:

~~~tsx
import { Dialogs } from "@wailsio/runtime";
import { useCallback, useEffect, useRef } from "react";

import { api, describeError, isCancellationError, taskCanceller } from "../backend/api";
import { OTA_PROGRESS_TARGET } from "../backend/wails";
import { useI18n } from "../i18n";
import { taskKey, useTaskCenter } from "../state/TaskCenterContext";

export const OTA_TASK_ID = taskKey("update", OTA_PROGRESS_TARGET);

export function AppUpdater() {
  const { t } = useI18n();
  const {
    startTask, finishTask, setTaskCanceller, setTaskAction, setTaskMessage, isTaskRunning,
  } = useTaskCenter();

  const restart = useCallback(async () => {
    try {
      await api.restartUpdate();
    } catch (error) {
      setTaskMessage(OTA_TASK_ID, describeError(error, t("无法重启并更新")).message);
    }
  }, [setTaskMessage, t]);

  const download = useCallback(async (version: string) => {
    if (isTaskRunning(OTA_TASK_ID) || !startTask({
      id: OTA_TASK_ID,
      kind: "update",
      target: OTA_PROGRESS_TARGET,
      progressTarget: OTA_PROGRESS_TARGET,
      title: t("更新 BootAgent {version}", { version }),
      route: "/overview",
    })) return;

    try {
      const request = api.downloadUpdate();
      setTaskCanceller(OTA_TASK_ID, taskCanceller(request));
      await request;
      finishTask(OTA_TASK_ID, { kind: "success", message: t("更新已下载") });
      setTaskAction(OTA_TASK_ID, { label: t("重启并更新"), run: restart });
    } catch (error) {
      const cancelled = isCancellationError(error);
      finishTask(OTA_TASK_ID, {
        kind: cancelled ? "cancelled" : "failure",
        message: cancelled ? t("已取消") : describeError(error, t("更新失败")).message,
      });
    }
  }, [finishTask, isTaskRunning, restart, setTaskAction, setTaskCanceller, startTask, t]);

  const check = useCallback(async () => {
    if (isTaskRunning(OTA_TASK_ID)) return;
    let version = "";
    try {
      version = await api.checkUpdate();
    } catch {
      return;
    }
    if (!version || isTaskRunning(OTA_TASK_ID)) return;

    try {
      const update = t("更新");
      const choice = await Dialogs.Question({
        Title: t("BootAgent 更新"),
        Message: t("发现 BootAgent 新版本 {version}，现在下载吗？", { version }),
        Buttons: [
          { Label: update, IsDefault: true },
          { Label: t("暂不"), IsCancel: true },
        ],
      });
      if (choice === update) await download(version);
    } catch {
      // A background prompt failure must not interrupt startup.
    }
  }, [download, isTaskRunning, t]);

  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void check();
  }, [check]);

  return null;
}
~~~

The version parameter supplies visible dialog and task text; Wails retains the checked pending release, so the frontend never picks an asset or reimplements version comparison. Not now is not persisted.

- [ ] **Step 5: Mount it inside the existing provider**

Change the provider tree in frontend/src/App.tsx to:

~~~tsx
<TaskCenterProvider>
  <AppUpdater />
  <WizardProvider>
    <WorkspaceRoutes />
  </WizardProvider>
</TaskCenterProvider>
~~~

Add the import from ./components/AppUpdater.

- [ ] **Step 6: Run coordinator and frontend tests**

Run: cd frontend && pnpm test -- src/components/AppUpdater.test.tsx src/components/TaskCenter.test.tsx && pnpm run typecheck

Expected: PASS; ignored updates create no card, approved downloads are cancellable, successful downloads expose the restart action, and restart errors leave that action available.

- [ ] **Step 7: Commit the frontend OTA flow**

~~~bash
git add frontend/src/components/AppUpdater.tsx frontend/src/components/AppUpdater.test.tsx frontend/src/i18n.tsx frontend/src/App.tsx
git commit -m "feat: add consented OTA task flow"
~~~
+

### Task 7: Tag Release Packaging and Checksums

**Files:**
- Modify: .github/workflows/build-artifacts.yml
- Modify: README.md

- [ ] **Step 1: Change the workflow trigger and release permissions**

Replace the manual-only trigger/env block with:

~~~yaml
on:
  push:
    tags:
      - 'v*.*.*'
  workflow_dispatch:
    inputs:
      version:
        description: Release version (for example v0.3.0)
        required: true
        type: string
        default: v0.0.0

permissions:
  contents: read

env:
  RELEASE_VERSION: ${{ github.event_name == 'workflow_dispatch' && inputs.version || github.ref_name }}
~~~

The existing validation command remains the exact gate:

~~~bash
if [[ ! "$RELEASE_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "version must match vX.Y.Z" >&2
  exit 1
fi
~~~

- [ ] **Step 2: Keep the existing build labels and add OTA asset labels**

The license inventory and existing artifact checks use x64 as the amd64 label, so
do not rename that matrix value. Replace the string matrix with an object that
preserves the existing label while adding the Wails-facing archive token:

~~~yaml
        arch:
          - label: x64
            goarch: amd64
            ota: amd64
          - label: arm64
            goarch: arm64
            ota: arm64

    env:
      CGO_ENABLED: ${{ matrix.target.cgo }}
      CGO_CFLAGS: ${{ matrix.target.platform == 'macos' && '-mmacosx-version-min=12.0' || '' }}
      CGO_LDFLAGS: ${{ matrix.target.platform == 'macos' && '-mmacosx-version-min=12.0' || '' }}
      GOARCH: ${{ matrix.arch.goarch }}
      MACOSX_DEPLOYMENT_TARGET: ${{ matrix.target.platform == 'macos' && '12.0' || '' }}
~~~

Change the build job name to use the matrix arch label. Use the label field for
the existing third-party verification and non-OTA artifact names; use the ota
field only for OTA zip names. Keep the existing linker flag exactly:

~~~bash
version_ldflag="-X github.com/MaimoryLab/BootAgent/internal/version.Version=$RELEASE_VERSION"
~~~

The concrete substitutions are:

~~~yaml
    name: ${{ matrix.target.platform }} ${{ matrix.arch.label }}
          --verify-platform "${{ matrix.target.platform }}-${{ matrix.arch.label }}"
          name: BootAgent-${{ matrix.target.platform }}-${{ matrix.arch.label }}
~~~

- [ ] **Step 3: Zip exactly one top-level desktop payload per matrix entry**

After the existing macOS bundle/license verification, add:

~~~yaml
      - name: Package macOS OTA archive
        if: matrix.target.platform == 'macos'
        shell: bash
        run: |
          (cd bin && zip -qry "BootAgent-darwin-${{ matrix.arch.ota }}.zip" BootAgent.app)
          roots=$(unzip -Z1 "bin/BootAgent-darwin-${{ matrix.arch.ota }}.zip" | awk -F/ 'NF {print $1}' | sort -u)
          test "$roots" = "BootAgent.app"

      - name: Package Windows OTA archive
        if: matrix.target.platform == 'windows'
        shell: pwsh
        run: |
          $archive = "bin/BootAgent-windows-${{ matrix.arch.ota }}.zip"
          Compress-Archive -Path bin/bootagent-desktop.exe -DestinationPath $archive -CompressionLevel Optimal
          $roots = @(tar -tf $archive | ForEach-Object { ($_ -split '/')[0] } | Sort-Object -Unique)
          if ($roots.Count -ne 1 -or $roots[0] -ne 'bootagent-desktop.exe') { throw 'OTA archive has unexpected top-level entries' }

      - name: Upload OTA archive
        uses: actions/upload-artifact@v7
        with:
          name: ota-${{ matrix.target.platform }}-${{ matrix.arch.label }}
          path: bin/BootAgent-${{ matrix.target.platform }}-${{ matrix.arch.ota }}.zip
          if-no-files-found: error
~~~

The macOS archive is made from BootAgent.app only; the Windows archive is made
from bootagent-desktop.exe only. Compliance files remain inside the macOS app
bundle as they do in the existing package.

- [ ] **Step 4: Add a release job that generates SHA256SUMS and creates or updates the release**

Append this job:

~~~yaml
  release:
    needs: build
    runs-on: ubuntu-latest
    permissions:
      contents: write
    env:
      GH_TOKEN: ${{ github.token }}
    steps:
      - uses: actions/download-artifact@v8
        with:
          pattern: ota-*
          path: release-assets
          merge-multiple: true

      - name: Validate OTA set and create checksum manifest
        shell: bash
        run: |
          set -euo pipefail
          cd release-assets
          test "$(find . -maxdepth 1 -type f -name 'BootAgent-*.zip' | wc -l)" -eq 4
          printf '%s\n' BootAgent-darwin-amd64.zip BootAgent-darwin-arm64.zip BootAgent-windows-amd64.zip BootAgent-windows-arm64.zip | while read -r name; do
            test -f "$name"
          done
          sha256sum BootAgent-*.zip > SHA256SUMS
          test "$(wc -l < SHA256SUMS)" -eq 4

      - name: Create or update GitHub Release
        shell: bash
        run: |
          set -euo pipefail
          if gh release view "$RELEASE_VERSION" >/dev/null 2>&1; then
            gh release upload "$RELEASE_VERSION" release-assets/* --clobber
          else
            gh release create "$RELEASE_VERSION" release-assets/* --verify-tag --title "$RELEASE_VERSION" --generate-notes
          fi
~~~

The job has write permission only where publication occurs. SHA256SUMS uses the exact archive base names that the Wails GitHub provider requests through ChecksumAsset: SHA256SUMS.

- [ ] **Step 5: Update the release documentation**

Replace the manual-release paragraph in README.md with:

~~~md
Release packages are built and published by .github/workflows/build-artifacts.yml
when a stable vX.Y.Z tag is pushed. It publishes macOS and Windows amd64/arm64
OTA archives plus SHA256SUMS; the macOS archives contain BootAgent.app and the
Windows archives contain bootagent-desktop.exe.
~~~

- [ ] **Step 6: Validate workflow text and commit**

Run:

~~~bash
git diff --check
ruby -e 'require "yaml"; YAML.load_file(".github/workflows/build-artifacts.yml"); puts "workflow yaml parsed"'
python3 scripts/check-docs.py
~~~

Expected: no whitespace errors, YAML parses, and documentation checks pass.

~~~bash
git add .github/workflows/build-artifacts.yml README.md
git commit -m "ci: publish OTA archives on version tags"
~~~

### Task 8: Full Verification and Handoff

**Files:**
- No new files; inspect all changes and generated output.

- [ ] **Step 1: Regenerate bindings from the current source**

Run: task generate:bindings

Expected: the pinned Wails CLI completes and produces no unexpected edits beyond the updater service bindings.

- [ ] **Step 2: Run all Go checks**

Run: go test ./... && go vet ./...

Expected: PASS. If the host lacks native Wails libraries, use the repository's existing non-Wails Go suite and separately run:

~~~bash
go test -tags wails ./cmd/bootagent-desktop ./internal/binding -run '^$'
~~~

- [ ] **Step 3: Run all frontend checks**

Run: cd frontend && pnpm test && pnpm run build

Expected: all Vitest tests pass and typecheck plus Vite production build complete.

- [ ] **Step 4: Validate archive rules with disposable fixtures**

Run the same root-entry checks against temporary fixtures without touching tracked files:

~~~bash
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$tmp_dir/BootAgent.app/Contents/MacOS"
touch "$tmp_dir/BootAgent.app/Contents/MacOS/bootagent-desktop"
(cd "$tmp_dir" && zip -qry BootAgent-darwin-amd64.zip BootAgent.app)
test "$(unzip -Z1 "$tmp_dir/BootAgent-darwin-amd64.zip" | awk -F/ 'NF {print $1}' | sort -u)" = "BootAgent.app"
printf 'binary' > "$tmp_dir/bootagent-desktop.exe"
(cd "$tmp_dir" && zip -qry BootAgent-windows-amd64.zip bootagent-desktop.exe)
test "$(unzip -Z1 "$tmp_dir/BootAgent-windows-amd64.zip" | awk -F/ 'NF {print $1}' | sort -u)" = "bootagent-desktop.exe"
echo "archive checks passed"
~~~

Expected: archive checks passed.

- [ ] **Step 5: Review the diff against the specification**

Run:

~~~bash
git diff --stat
git diff --check
git status --short
~~~

Confirm every spec section has an implementation: release-only startup check, native consent, current-launch ignore, task progress/cancellation/failure, restart retry, four exact archives, checksum manifest, and tag publication. Do not add forced-update, prerelease, skip-version persistence, signing keys, or custom updater-window code; they are explicit non-goals.

- [ ] **Step 6: Commit the verified final state**

~~~bash
git add docs/superpowers/plans/2026-08-06-ota-updater.md internal/version internal/binding cmd/bootagent-desktop frontend .github/workflows/build-artifacts.yml README.md go.mod go.sum
git commit -m "feat: complete GitHub Releases OTA updates"
~~~

## Self-Review Checklist

- Spec coverage: Tasks 1-3 cover version gating, GitHub provider configuration, checksum selection, Wails delegation, and progress translation. Tasks 4-6 cover generated bindings, native confirmation, one-shot startup, task-center cancellation/action/failure behavior. Task 7 covers tag release packaging and publication. Task 8 covers requested verification.
- Placeholder scan: every task step has a concrete snippet and command with an expected result; no deferred implementation is hidden behind a vague instruction.
- Type consistency: OTA_PROGRESS_TARGET is the shared frontend target, UpdateProgressTarget is its Go counterpart, UpdateBackend is the exact interface accepted by NewUpdateService, and the generated methods are Check, DownloadAndInstall, and Restart throughout.
