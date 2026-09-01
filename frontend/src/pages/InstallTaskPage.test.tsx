import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

// No I18nProvider, so translate() returns the Chinese keys unchanged.
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
  it("says where the result actually is", () => {
    render(
      <MemoryRouter initialEntries={["/tasks/install/codex"]}>
        <Routes>
          <Route path="/tasks/install/:agentId" element={<InstallTaskPage />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText("暂无任务")).toBeTruthy();
    expect(screen.getByText(/安装结果请在环境总览中查看/)).toBeTruthy();
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
