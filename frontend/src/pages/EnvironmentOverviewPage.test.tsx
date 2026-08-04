import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import type { StatusResponse } from "../types/api";
import { EnvironmentOverviewPage } from "./EnvironmentOverviewPage";

const dispatch = vi.fn();

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch, refreshStatus: vi.fn() }),
}));

function renderPage() {
  dispatch.mockClear();
  render(
    <MemoryRouter initialEntries={["/overview"]}>
      <Routes>
        <Route path="/overview" element={<EnvironmentOverviewPage />} />
        <Route path="/setup/agents" element={<h1>onboarding</h1>} />
      </Routes>
    </MemoryRouter>,
  );
}

let mockState: { status: StatusResponse | null; statusState: string; statusError: string };

function status(): StatusResponse {
  const agent = (installed: boolean, profileId: string | null) => ({
    installed,
    configured: installed,
    guideOnly: false,
    config: "/config",
    version: installed ? "1.0.0" : null,
    lockedVersion: "1.0.0",
    canInstall: true,
    provider: installed ? "ppio" : null,
    profileId,
    model: installed ? "model-a" : null,
    baseUrl: installed ? "https://api.ppio.com/openai" : null,
    updatedAt: null,
    detected: null,
  });
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "zsh" },
    runtimes: [],
    capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
    agents: { codex: agent(true, "team"), opencode: agent(false, null) },
    catalog: [
      { id: "opencode", name: "OpenCode", group: "auto", configMode: "auto", guideOnly: false, lockedVersion: "1.0.0", protocol: "openai", platforms: ["macos"], platformNote: "", rank: 4 },
      { id: "codex", name: "Codex", group: "auto", configMode: "auto", guideOnly: false, lockedVersion: "1.0.0", protocol: "responses", platforms: ["macos"], platformNote: "", rank: 1 },
    ],
    groups: [],
    providers: { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" } },
    mirrors: [],
    paths: {},
    backups: {},
    profiles: [{ id: "team", label: "团队默认", provider: "ppio", baseUrl: null, model: "model-a", agentIds: ["codex"], activatedAt: null, hasKey: true }],
    activeProfile: "team",
    firstRun: false,
    environment: null,
    environmentError: null,
    chatgptApp: { id: "chatgpt-desktop", name: "ChatGPT Desktop", installed: false, supported: false, version: null, source: "unknown" },
  };
}

describe("EnvironmentOverviewPage", () => {
  it("shows only installed Agents with their Provider and Profile", () => {
    mockState = { status: status(), statusState: "success", statusError: "" };
    renderPage();
    expect(screen.getByText("Codex")).toBeTruthy();
    expect(screen.queryByText("OpenCode")).toBeNull();
    expect(screen.getByText("PPIO")).toBeTruthy();
    expect(screen.getByText("团队默认")).toBeTruthy();
  });

  it("offers onboarding as the way out of an empty environment", async () => {
    // With nothing installed the page has nothing to manage, so the empty state
    // has to lead somewhere instead of just describing the problem.
    const empty = status();
    empty.agents.codex.installed = false;
    mockState = { status: empty, statusState: "success", statusError: "" };
    renderPage();
    expect(screen.getByText("尚未安装任何 Agent")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "安装 Agent" }));
    expect(await screen.findByRole("heading", { name: "onboarding" })).toBeTruthy();
    // A second run must not inherit the previous Agent, model or log.
    expect(dispatch).toHaveBeenCalledWith({ type: "START_SETUP" });
  });
});
