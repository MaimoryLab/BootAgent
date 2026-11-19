import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import type { FirstTaskResult } from "../types/api";
import { FirstTaskSection } from "./FirstTaskSection";

const DIRECTORY = "/tmp/bootagent-first-task-123";

function result(overrides: Partial<FirstTaskResult> = {}): FirstTaskResult {
  return { agent_id: "codex", ok: true, directory: DIRECTORY, files: ["index.html"], log: "Created index.html", ...overrides };
}

async function renderSection(agentId = "codex") {
  vi.spyOn(api, "firstTaskAgents").mockResolvedValue([{ id: "codex", name: "Codex" }]);
  const page = render(<MemoryRouter><FirstTaskSection agentId={agentId} /></MemoryRouter>);
  await waitFor(() => expect(api.firstTaskAgents).toHaveBeenCalled());
  return page;
}

describe("FirstTaskSection", () => {
  afterEach(() => vi.restoreAllMocks());

  it("is absent for an Agent that cannot run unattended", async () => {
    const run = vi.spyOn(api, "runFirstTask");
    const { container } = await renderSection("opencode");

    expect(container.innerHTML).toBe("");
    expect(run).not.toHaveBeenCalled();
  });

  // The run is a billed request to the user's own Provider. It has to be said
  // before the button, and nothing may be sent until the button is pressed.
  it("says the run is billed and waits to be asked", async () => {
    const run = vi.spyOn(api, "runFirstTask").mockResolvedValue(result());
    await renderSection();

    const start = await screen.findByRole("button", { name: "开始" });
    expect(screen.getByText(/真实的模型调用，由模型服务按它的价格计费/)).toBeTruthy();
    expect(screen.getByText(/只会写入这个目录/)).toBeTruthy();
    expect(run).not.toHaveBeenCalled();

    fireEvent.click(start);
    expect(run).toHaveBeenCalledTimes(1);
    expect(run).toHaveBeenCalledWith("codex", "zh-CN");
  });

  it("shows where the page is and what was written", async () => {
    vi.spyOn(api, "runFirstTask").mockResolvedValue(result({ files: ["assets/style.css", "index.html"] }));
    await renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "开始" }));

    expect((await screen.findByRole("status")).textContent).toContain("Codex 写好了");
    expect(screen.getByText(DIRECTORY)).toBeTruthy();
    expect(screen.getAllByRole("listitem").map((item) => item.textContent)).toEqual(["assets/style.css", "index.html"]);
    // The record is there, behind a disclosure rather than poured onto the page.
    expect(screen.queryByText("Created index.html")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /查看运行记录/ }));
    expect(screen.getByText("Created index.html")).toBeTruthy();
  });

  it("offers a next step when the Agent wrote nothing", async () => {
    const run = vi.spyOn(api, "runFirstTask").mockResolvedValue(result({ ok: false, directory: "", files: [], log: "401 Unauthorized" }));
    await renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "开始" }));

    expect((await screen.findByRole("alert")).textContent).toContain("Codex 没有写出任何文件");
    expect(screen.getByRole("button", { name: "检查模型服务" })).toBeTruthy();
    // The reason is in the record, so a failure opens it without being asked.
    expect(screen.getByText("401 Unauthorized")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(run).toHaveBeenCalledTimes(2);
  });

  // Files exist, so they are shown, but the Agent did not finish cleanly and the
  // page may be half written. Calling that a success would misreport it.
  it("keeps the files of a run that ended badly without calling it done", async () => {
    vi.spyOn(api, "runFirstTask").mockResolvedValue(result({ ok: false }));
    await renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "开始" }));

    expect((await screen.findByRole("alert")).textContent).toContain("Codex 没有正常结束");
    expect(screen.getByText(DIRECTORY)).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("reports a run that could not start", async () => {
    vi.spyOn(api, "runFirstTask").mockRejectedValue(new BootAgentApiError("The selected Agent is not installed", "PREREQUISITE_MISSING", false, 400));
    await renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "开始" }));

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByRole("button", { name: "重试" })).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
  });
});
