import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

// No I18nProvider, so translate() returns the Chinese keys unchanged.
import { InstallTaskPage } from "./InstallTaskPage";

vi.mock("../state/TaskCenterContext", async (importOriginal) => ({
  ...await importOriginal<typeof import("../state/TaskCenterContext")>(),
  useTaskCenter: () => ({ tasks: [], cancelTask: vi.fn(), dismissTask: vi.fn(), progress: {}, running: false }),
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
