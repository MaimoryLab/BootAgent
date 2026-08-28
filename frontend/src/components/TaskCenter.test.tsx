import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";

import { taskKey, TaskCenterProvider, type TaskInput, useTaskCenter } from "../state/TaskCenterContext";
import type { InstallOutput } from "../types/api";
import { DownloadProgress } from "./DownloadProgress";
import { TaskCenter } from "./TaskCenter";

let emit: ((output: InstallOutput) => void) | null = null;
const unsubscribe = vi.fn();
const cancelRequest = vi.fn();
const terminalAction = vi.fn();

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: {
      onInstallOutput: (listener: (output: InstallOutput) => void) => {
        emit = listener;
        return unsubscribe;
      },
    },
    describeError: errors.describeError,
    describeFailure: errors.describeFailure,
  };
});

function send(output: InstallOutput) {
  act(() => {
    emit?.(output);
  });
}

const installTask: TaskInput = {
  id: taskKey("install", "codex"),
  kind: "install",
  target: "codex",
  title: "安装 Codex",
  route: "/setup/activation",
  version: "1.2.3",
  source: "npmjs.org",
};

function TaskHarness() {
  const { startTask, finishTask, setTaskCanceller, setTaskAction } = useTaskCenter();
  return (
    <div>
      <button type="button" onClick={() => {
        if (startTask(installTask)) setTaskCanceller(installTask.id!, cancelRequest);
      }}>启动安装</button>
      <button type="button" onClick={() => finishTask(installTask.id!, { kind: "success", message: "安装完成" })}>完成安装</button>
      <button type="button" onClick={() => setTaskAction(installTask.id!, { label: "重试安装", run: terminalAction })}>设置终端动作</button>
      <button type="button" onClick={() => { startTask(installTask); }}>再次启动</button>
      <button type="button">外部区域</button>
      <button type="button" onClick={() => { startTask({ kind: "update", target: "codex", title: "更新 Codex", route: "/overview" }); }}>更新同一 Agent</button>
      <button type="button" onClick={() => {
        for (const target of ["node", "uv"]) {
          const id = taskKey("download", target);
          startTask({ id, kind: "download", target, title: `下载 ${target}`, route: "/overview", group: "runtime-batch" });
          setTaskCanceller(id, cancelRequest);
        }
      }}>启动批量下载</button>
      <button type="button" onClick={() => {
        startTask({ id: taskKey("migration", "codex-conversations"), kind: "migration", target: "codex-conversations", title: "迁移对话", route: "", openable: false, cancellable: false });
      }}>启动迁移</button>
    </div>
  );
}

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="location">{location.pathname}</output>;
}

function renderTaskCenter(initialEntry = "/overview") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <TaskCenterProvider>
        <TaskCenter />
        <TaskHarness />
        <Routes>
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </TaskCenterProvider>
    </MemoryRouter>,
  );
}

describe("TaskCenter", () => {
  beforeEach(() => {
    emit = null;
    unsubscribe.mockReset();
    cancelRequest.mockReset();
    terminalAction.mockReset();
  });

  it("shows task cards and never renders command output", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动安装" }));
    expect(await screen.findByText("安装 Codex")).toBeTruthy();
    expect(screen.getByText("进行中")).toBeTruthy();
    expect(screen.getByText(/1\.2\.3/)).toBeTruthy();
    expect(screen.getByText(/npmjs\.org/)).toBeTruthy();

    send({ kind: "command", args: ["npm", "install", "-g", "@openai/codex"] });
    send({ kind: "output", stream: "stdout", text: "added 1 package\n" });
    expect(screen.queryByText(/npm install/)).toBeNull();
    expect(screen.queryByText(/added 1 package/)).toBeNull();
    expect(screen.queryByText(/暂无任务日志|清空|完整日志/)).toBeNull();
  });

  it("moves a task into the download phase and reports a rate after two samples", async () => {
    const user = userEvent.setup();
    const now = vi.spyOn(Date, "now").mockReturnValueOnce(1_000).mockReturnValueOnce(2_000);
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动安装" }));
    send({ kind: "progress", target: "codex", received: 1 * 1024 * 1024, total: 5 * 1024 * 1024 });
    send({ kind: "progress", target: "codex", received: 2 * 1024 * 1024, total: 5 * 1024 * 1024 });
    expect(screen.getByText(/下载中/)).toBeTruthy();
    expect(screen.getByText(/MB\/s/)).toBeTruthy();
    expect(screen.getByText(/剩余约/)).toBeTruthy();
    now.mockRestore();
  });

  it("renders migration as a status-only task", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动迁移" }));
    expect(await screen.findByText("迁移对话")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /返回任务页面：迁移对话/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "取消任务" })).toBeNull();
    send({ kind: "progress", target: "codex-conversations", received: 1, total: 2 });
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(screen.queryByText(/MB/)).toBeNull();
  });

  it("closes when the user clicks outside the task center", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "任务中心" }));
    expect(screen.getByText("暂无任务")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "外部区域" }));
    expect(screen.queryByText("暂无任务")).toBeNull();
  });

  it("returns to the route recorded by a card after navigation", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动安装" }));
    await user.click(screen.getByRole("button", { name: /返回任务页面：安装 Codex/ }));
    expect(screen.getByTestId("location")).toHaveTextContent("/setup/activation");
  });

  it("keeps a terminal card until the user dismisses it", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动安装" }));
    await user.click(screen.getByRole("button", { name: "完成安装" }));
    await user.click(screen.getByRole("button", { name: "任务中心" }));
    expect(await screen.findByText(/已完成/)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "关闭任务" }));
    expect(screen.queryByText("安装 Codex")).toBeNull();
  });

  it("renders and invokes a terminal action after the task finishes", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动安装" }));
    await user.click(screen.getByRole("button", { name: "设置终端动作" }));
    expect(screen.queryByRole("button", { name: "重试安装" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "完成安装" }));
    await user.click(screen.getByRole("button", { name: "任务中心" }));
    await user.click(screen.getByRole("button", { name: "重试安装" }));
    expect(terminalAction).toHaveBeenCalledTimes(1);
  });

  it("tracks byte progress on a download card", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动安装" }));
    // The install card is enough to prove task rendering; progress events are
    // attributed only to a matching progress target.
    send({ kind: "progress", target: "codex", received: 5 * 1024 * 1024, total: 20 * 1024 * 1024 });
    expect(await screen.findByText(/5\.0 MB \/ 20\.0 MB/)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "完成安装" }));
    expect(screen.queryByText(/5\.0 MB \/ 20\.0 MB/)).toBeNull();
  });

  it("locks duplicate and conflicting work on the same Agent", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动安装" }));
    await user.click(screen.getByRole("button", { name: "再次启动" }));
    await user.click(screen.getByRole("button", { name: "更新同一 Agent" }));
    await user.click(screen.getByRole("button", { name: "任务中心" }));
    expect(screen.getAllByText("安装 Codex")).toHaveLength(1);
    expect(screen.queryByText("更新 Codex")).toBeNull();
  });

  it("cancels the request once, keeps the terminal state, and releases the lock", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动安装" }));
    await user.click(screen.getByRole("button", { name: "取消任务" }));
    expect(cancelRequest).toHaveBeenCalledTimes(1);
    expect(screen.getByText("已取消")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "完成安装" }));
    expect(screen.queryByText("已完成")).toBeNull();
    await user.click(screen.getByRole("button", { name: "更新同一 Agent" }));
    expect(screen.getByText("更新 Codex")).toBeTruthy();
  });

  it("cancels every card backed by one request", async () => {
    const user = userEvent.setup();
    renderTaskCenter();
    await user.click(screen.getByRole("button", { name: "启动批量下载" }));
    await user.click(screen.getAllByRole("button", { name: "取消任务" })[0]);
    expect(cancelRequest).toHaveBeenCalledTimes(1);
    expect(screen.getAllByText("已取消")).toHaveLength(2);
  });
});

describe("DownloadProgress", () => {
  beforeEach(() => {
    emit = null;
  });

  it("can show an indeterminate bar before the first byte event", () => {
    render(
      <TaskCenterProvider>
        <DownloadProgress target="node" pending />
      </TaskCenterProvider>,
    );
    const bar = screen.getByRole("progressbar");
    expect(bar.className).toContain("is-indeterminate");
    expect(screen.getByText(/已下载 0\.0 MB/)).toBeTruthy();
  });

  it("renders nothing until the download reports bytes, then tracks the percentage", () => {
    render(
      <TaskCenterProvider>
        <DownloadProgress target="node" />
      </TaskCenterProvider>,
    );
    expect(screen.queryByRole("progressbar")).toBeNull();

    send({ kind: "progress", target: "node", received: 5 * 1024 * 1024, total: 20 * 1024 * 1024 });
    const bar = screen.getByRole("progressbar");
    expect(bar.getAttribute("aria-valuenow")).toBe("25");
    expect(screen.getByText(/5\.0 MB \/ 20\.0 MB/)).toBeTruthy();
  });

  it("ignores a progress event for another runtime", () => {
    render(
      <TaskCenterProvider>
        <DownloadProgress target="node" />
      </TaskCenterProvider>,
    );
    send({ kind: "progress", target: "uv", received: 1024, total: 2048 });
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  it("stays indeterminate when the server sent no length", () => {
    render(
      <TaskCenterProvider>
        <DownloadProgress target="node" />
      </TaskCenterProvider>,
    );
    // A percentage computed against a zero total would read as 0% forever.
    send({ kind: "progress", target: "node", received: 3 * 1024 * 1024, total: 0 });
    const bar = screen.getByRole("progressbar");
    expect(bar.getAttribute("aria-valuenow")).toBeNull();
    expect(bar.className).toContain("is-indeterminate");
    expect(screen.getByText(/已下载 3\.0 MB/)).toBeTruthy();
  });
});
