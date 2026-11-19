import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

// No I18nProvider, so translate() returns the Chinese keys unchanged.
import { api } from "../backend/api";
import { InstallTaskPage } from "./InstallTaskPage";

const mockUseTaskCenter = vi.hoisted(() => vi.fn(() => ({ tasks: [], cancelTask: vi.fn(), dismissTask: vi.fn(), progress: {}, running: {} })));

vi.mock("../state/TaskCenterContext", async (importOriginal) => ({
  ...await importOriginal<typeof import("../state/TaskCenterContext")>(),
  useTaskCenter: mockUseTaskCenter,
}));

describe("InstallTaskPage without a matching task", () => {
  // Reached by reloading the route after tasks were cleared, or by following a
  // link to a dismissed one. A bare "暂无任务" left the user unsure whether the
  // install had been lost. ActivationPage already has a recovery path for the
  // analogous case, so the two pages disagreed about the same situation.
  it("keeps the empty task state focused", () => {
    render(
      <MemoryRouter initialEntries={["/tasks/install/codex"]}>
        <Routes>
          <Route path="/tasks/install/:agentId" element={<InstallTaskPage />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText("暂无任务")).toBeTruthy();
    expect(screen.queryByText(/安装结果请在环境总览中查看/)).toBeNull();
    expect(screen.getByRole("button", { name: "进入总览" })).toBeTruthy();
  });
});

describe("InstallTaskPage update route", () => {
  it("renders a completed update task and its log", async () => {
    mockUseTaskCenter.mockReturnValue({
      tasks: [{
        id: "update:openclaw",
        kind: "update",
        target: "openclaw",
        title: "更新 OpenClaw",
        route: "/tasks/update/openclaw",
        progressTarget: "openclaw",
        state: "success",
        message: "更新完成",
        log: "$ npm update -g openclaw\nupdated\n",
        startedAt: 1,
        events: [
          { at: 1, kind: "phase", phase: "preparing", message: "task started" },
          { at: 2, kind: "result", phase: "completed", message: "更新完成" },
        ],
      }],
      cancelTask: vi.fn(),
      dismissTask: vi.fn(),
      progress: {},
      running: {},
    } as never);
    render(
      <MemoryRouter initialEntries={["/tasks/update/openclaw"]}>
        <Routes><Route path="/tasks/update/:agentId" element={<InstallTaskPage />} /></Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText("更新完成 · 更新 OpenClaw")).toBeTruthy();
    expect(screen.queryByText(/已下载/)).toBeNull();
    expect(screen.queryByText(/npm update -g openclaw/)).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /查看安装日志/ }));
    expect(screen.getByText(/npm update -g openclaw/)).toBeTruthy();
    expect(screen.getByRole("region", { name: "任务时间线" })).toBeTruthy();
  });
});

// An install ends on this page, so it is where the first task has to be offered.
describe("InstallTaskPage first task", () => {
  function renderTask(kind: "install" | "update", state: "success" | "failure") {
    mockUseTaskCenter.mockReturnValue({
      tasks: [{
        id: `${kind}:codex`, kind, target: "codex", title: "安装 Codex", route: `/tasks/${kind}/codex`,
        progressTarget: "", state, message: "", startedAt: 1, events: [],
      }],
      cancelTask: vi.fn(), dismissTask: vi.fn(), progress: {}, running: {},
    } as never);
    render(
      <MemoryRouter initialEntries={[`/tasks/${kind}/codex`]}>
        <Routes><Route path={`/tasks/${kind}/:agentId`} element={<InstallTaskPage />} /></Routes>
      </MemoryRouter>,
    );
  }

  it("is offered once an Agent has installed", async () => {
    const agents = vi.spyOn(api, "firstTaskAgents").mockResolvedValue([{ id: "codex", name: "Codex" }]);
    renderTask("install", "success");

    expect(await screen.findByRole("heading", { name: "跑一个小任务试试" })).toBeTruthy();
    agents.mockRestore();
  });

  // An update leaves an Agent the user already knows, and a failed install
  // leaves nothing to run. Neither asks the backend at all.
  it("is not offered after an update or a failed install", () => {
    const agents = vi.spyOn(api, "firstTaskAgents").mockResolvedValue([{ id: "codex", name: "Codex" }]);
    renderTask("update", "success");
    renderTask("install", "failure");

    expect(agents).not.toHaveBeenCalled();
    agents.mockRestore();
  });
});
