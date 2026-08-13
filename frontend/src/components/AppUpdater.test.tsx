import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode, useEffect } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { BootAgentApiError } from "../backend/errors";
import { OTA_PROGRESS_TARGET } from "../backend/wails";
import { I18nProvider, LOCALE_STORAGE_KEY } from "../i18n";
import { taskKey, TaskCenterProvider, useTaskCenter } from "../state/TaskCenterContext";
import { AppUpdater } from "./AppUpdater";
import { TaskCenter } from "./TaskCenter";

const mocks = vi.hoisted(() => ({
  question: vi.fn(),
  checkUpdate: vi.fn(),
  downloadUpdate: vi.fn(),
  restartUpdate: vi.fn(),
}));

vi.mock("@wailsio/runtime", () => ({ Dialogs: { Question: mocks.question } }));
vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: {
      onInstallOutput: () => () => {},
      checkUpdate: mocks.checkUpdate,
      downloadUpdate: mocks.downloadUpdate,
      restartUpdate: mocks.restartUpdate,
    },
    describeError: errors.describeError,
    describeFailure: errors.describeFailure,
    failureLine: errors.failureLine,
    isCancellationError: errors.isCancellationError,
  };
});

function deferredDownload() {
  let resolve!: () => void;
  let reject!: (error: unknown) => void;
  const request = new Promise<void>((onResolve, onReject) => {
    resolve = onResolve;
    reject = onReject;
  }) as Promise<void> & { cancel: ReturnType<typeof vi.fn> };
  request.cancel = vi.fn();
  return { request, resolve, reject };
}

function ExistingUpdate() {
  const { isTaskRunning, startTask } = useTaskCenter();
  const id = taskKey("update", OTA_PROGRESS_TARGET);
  useEffect(() => {
    startTask({
      id,
      kind: "update",
      target: OTA_PROGRESS_TARGET,
      title: "Existing update",
      route: "/overview",
    });
  }, [id, startTask]);
  return isTaskRunning(id) ? <AppUpdater /> : null;
}

function mount({ strict = false, existing = false } = {}) {
  const content = (
    <I18nProvider>
      <TaskCenterProvider>
        {existing ? <ExistingUpdate /> : null}
        {existing ? null : <AppUpdater />}
        <TaskCenter />
      </TaskCenterProvider>
    </I18nProvider>
  );
  return render(strict ? <StrictMode>{content}</StrictMode> : content);
}

describe("AppUpdater", () => {
  beforeEach(() => {
    localStorage.setItem(LOCALE_STORAGE_KEY, "en");
    mocks.question.mockReset().mockResolvedValue("Not now");
    mocks.checkUpdate.mockReset().mockResolvedValue("");
    mocks.downloadUpdate.mockReset();
    mocks.restartUpdate.mockReset();
  });

  it("checks once under StrictMode", async () => {
    mount({ strict: true });
    await waitFor(() => expect(mocks.checkUpdate).toHaveBeenCalledTimes(1));
  });

  it.each([
    ["a failed check", () => Promise.reject(new Error("offline"))],
    ["the current version", () => Promise.resolve("")],
  ])("silently ignores %s", async (_name, result) => {
    mocks.checkUpdate.mockImplementation(result);
    mount();
    await waitFor(() => expect(mocks.checkUpdate).toHaveBeenCalledTimes(1));
    expect(mocks.question).not.toHaveBeenCalled();
    expect(mocks.downloadUpdate).not.toHaveBeenCalled();
    expect(screen.queryByText("Update BootAgent")).toBeNull();
  });

  it("does nothing when the OTA task is already running", async () => {
    mocks.checkUpdate.mockResolvedValue("v2.0.0");
    mount({ existing: true });
    expect(await screen.findByText("Existing update")).toBeTruthy();
    expect(mocks.checkUpdate).not.toHaveBeenCalled();
    expect(mocks.question).not.toHaveBeenCalled();
    expect(mocks.downloadUpdate).not.toHaveBeenCalled();
  });

  it("skips the download when Not now is chosen", async () => {
    mocks.checkUpdate.mockResolvedValue("v2.0.0");
    mount();
    await waitFor(() => expect(mocks.question).toHaveBeenCalledWith({
      Title: "BootAgent update",
      Message: "BootAgent v2.0.0 is available. Download it now?",
      Buttons: [
        { Label: "Update", IsDefault: true },
        { Label: "Not now", IsCancel: true },
      ],
    }));
    expect(mocks.downloadUpdate).not.toHaveBeenCalled();
    expect(screen.queryByText("Update BootAgent v2.0.0")).toBeNull();
  });

  it("starts a versioned cancellable task after approval", async () => {
    const user = userEvent.setup();
    const download = deferredDownload();
    mocks.checkUpdate.mockResolvedValue("v2.0.0");
    mocks.question.mockResolvedValue("Update");
    mocks.downloadUpdate.mockReturnValue(download.request);
    mount();

    expect(await screen.findByText("Update BootAgent v2.0.0")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Cancel task" }));
    expect(download.request.cancel).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Cancelled")).toBeTruthy();
    download.reject(Object.assign(new Error("cancelled"), { name: "CancelError" }));
  });

  it("offers restart after a successful download", async () => {
    const user = userEvent.setup();
    mocks.checkUpdate.mockResolvedValue("v2.0.0");
    mocks.question.mockResolvedValue("Update");
    mocks.downloadUpdate.mockResolvedValue(undefined);
    mocks.restartUpdate.mockResolvedValue(undefined);
    mount();

    await waitFor(() => expect(mocks.downloadUpdate).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole("button", { name: "Task center" }));
    expect(await screen.findByText(/Completed.*Update downloaded/)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Restart and update" }));
    expect(mocks.restartUpdate).toHaveBeenCalledTimes(1);
  });

  it("shows download failures and preserves cancellation", async () => {
    const user = userEvent.setup();
    const failed = deferredDownload();
    mocks.checkUpdate.mockResolvedValue("v2.0.0");
    mocks.question.mockResolvedValue("Update");
    mocks.downloadUpdate.mockReturnValue(failed.request);
    const view = mount();
    expect(await screen.findByText("Update BootAgent v2.0.0")).toBeTruthy();
    failed.reject(new Error("download failed"));
    expect(await screen.findByText(/Failed.*download failed/)).toBeTruthy();

    view.unmount();
    const cancelled = deferredDownload();
    mocks.downloadUpdate.mockReturnValue(cancelled.request);
    mount();
    expect(await screen.findByText("Update BootAgent v2.0.0")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Cancel task" }));
    cancelled.reject(Object.assign(new Error("cancelled"), { name: "CancelledRejectionError" }));
    expect(await screen.findByText("Cancelled")).toBeTruthy();
  });

  // The updater picked BootAgent-darwin-arm64.dmg over the sibling .zip, and a
  // .dmg is not a format it unpacks -- so the disk image replaced the installed
  // BootAgent.app. The backend now refuses that artifact, and the task centre has
  // to carry the hint: the message alone names no way forward, and retrying
  // downloads the same asset.
  it("tells the user to download manually when the update is not installable", async () => {
    const failed = deferredDownload();
    mocks.checkUpdate.mockResolvedValue("v2.0.0");
    mocks.question.mockResolvedValue("Update");
    mocks.downloadUpdate.mockReturnValue(failed.request);
    mount();
    expect(await screen.findByText("Update BootAgent v2.0.0")).toBeTruthy();

    failed.reject(new BootAgentApiError(
      "The downloaded BootAgent update is not installable",
      "UPDATE_NOT_INSTALLABLE",
      false,
      500,
    ));

    expect(await screen.findByText(/cannot be installed/)).toBeTruthy();
    expect(screen.getByText(/manually from the releases page/)).toBeTruthy();
  });

  it("keeps the restart action after restart fails", async () => {
    const user = userEvent.setup();
    mocks.checkUpdate.mockResolvedValue("v2.0.0");
    mocks.question.mockResolvedValue("Update");
    mocks.downloadUpdate.mockResolvedValue(undefined);
    mocks.restartUpdate.mockRejectedValue(new Error("restart failed"));
    mount();

    await waitFor(() => expect(mocks.downloadUpdate).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole("button", { name: "Task center" }));
    const restart = await screen.findByRole("button", { name: "Restart and update" });
    await user.click(restart);
    expect(await screen.findByText(/restart failed/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Restart and update" })).toBeTruthy();
  });

  it("serializes restart attempts and allows retry after failure", async () => {
    const user = userEvent.setup();
    let rejectRestart!: (error: unknown) => void;
    const restart = new Promise<void>((_resolve, reject) => { rejectRestart = reject; });
    mocks.checkUpdate.mockResolvedValue("v2.0.0");
    mocks.question.mockResolvedValue("Update");
    mocks.downloadUpdate.mockResolvedValue(undefined);
    mocks.restartUpdate.mockReturnValueOnce(restart).mockResolvedValueOnce(undefined);
    mount();

    await waitFor(() => expect(mocks.downloadUpdate).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole("button", { name: "Task center" }));
    const restartButton = await screen.findByRole("button", { name: "Restart and update" });
    await user.click(restartButton);
    await user.click(restartButton);
    expect(mocks.restartUpdate).toHaveBeenCalledTimes(1);

    rejectRestart(new Error("restart failed"));
    expect(await screen.findByText(/restart failed/)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Restart and update" }));
    expect(mocks.restartUpdate).toHaveBeenCalledTimes(2);
  });
});
