import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import type { DesktopAgentStatus, StatusResponse } from "../types/api";
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
    selectsModel: true, guideOnly: false,
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
    latestVersion: null,
  });
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "zsh" },
    runtimes: [],
    capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
    agents: { codex: agent(true, "team"), opencode: agent(false, null) },
    catalog: [
      { id: "opencode", name: "OpenCode", group: "auto", configMode: "auto", selectsModel: true, guideOnly: false, lockedVersion: "1.0.0", protocol: "openai", platforms: ["macos"], platformNote: "", rank: 4 },
      { id: "codex", name: "Codex", group: "auto", configMode: "auto", selectsModel: true, guideOnly: false, lockedVersion: "1.0.0", protocol: "responses", platforms: ["macos"], platformNote: "", rank: 1 },
    ],
    groups: [],
    providers: { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" } },
    mirrors: [],
    paths: {},
    backups: {},
    profiles: [{ id: "team", label: "团队默认", provider: "ppio", baseUrl: null, model: "model-a", protocol: "responses", activatedAt: null }],
    activeProfile: "team",
    firstRun: false,
    environment: null,
    environmentError: null,
    desktopAgents: [],
  };
}

function desktopApp(overrides: Partial<DesktopAgentStatus> = {}): DesktopAgentStatus {
  return {
    id: "chatgpt-desktop",
    name: "ChatGPT Desktop",
    installed: false,
    supported: true,
    version: null,
    source: "macos-dmg",
    protocol: "responses",
    profileAgentId: "codex",
    profileId: null,
    ...overrides,
  };
}

describe("EnvironmentOverviewPage", () => {
  it("shows only installed Agents with their Provider and Profile", () => {
    mockState = { status: status(), statusState: "success", statusError: "" };
    renderPage();
    expect(screen.getByRole("heading", { name: "命令行 Agent" })).toBeTruthy();
    expect(screen.getByText("Codex")).toBeTruthy();
    expect(screen.queryByText("OpenCode")).toBeNull();
    const row = within(screen.getByTestId("agent-codex"));
    expect(row.getByText("PPIO", { selector: ".agent-manage-pill" })).toBeTruthy();
    expect(row.getByText("团队默认", { selector: ".agent-manage-pill" })).toBeTruthy();
  });

  it("uses a compatible active Profile when an older install has no binding file", () => {
    const legacy = status();
    legacy.agents.codex.profileId = null;
    mockState = { status: legacy, statusState: "success", statusError: "" };
    renderPage();

    const row = within(screen.getByTestId("agent-codex"));
    expect(row.getByText("团队默认", { selector: ".agent-manage-pill" })).toBeTruthy();
    expect(row.getByText("PPIO", { selector: ".agent-manage-pill" })).toBeTruthy();
  });

  it("offers onboarding as the way out of an empty environment", async () => {
    // With nothing installed the page has nothing to manage, so the empty state
    // has to lead somewhere instead of just describing the problem.
    const empty = status();
    empty.agents.codex.installed = false;
    mockState = { status: empty, statusState: "success", statusError: "" };
    renderPage();
    expect(screen.getByText("尚未安装任何命令行 Agent")).toBeTruthy();
    // With nothing installed the empty state owns the single call to action.
    expect(screen.getAllByRole("button", { name: "安装 Agent" })).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "安装 Agent" }));
    expect(await screen.findByRole("heading", { name: "onboarding" })).toBeTruthy();
    // A second run must not inherit the previous Agent, model or log.
    expect(dispatch).toHaveBeenCalledWith({ type: "START_DESKTOP_SETUP" });
  });

  it("counts and shows only installed desktop Agents", async () => {
    const current = status();
    current.desktopAgents = [
      desktopApp({ installed: true, version: "26.730.61309", profileId: "team" }),
      desktopApp({ id: "workbuddy", name: "WorkBuddy", protocol: "openai", profileAgentId: "workbuddy" }),
    ];
    mockState = { status: current, statusState: "success", statusError: "" };
    renderPage();

    const desktopSection = screen.getByRole("heading", { name: "桌面 Agent" }).closest("section");
    expect(desktopSection).toBeTruthy();
    expect(within(desktopSection!).getByText("共 1 个")).toBeTruthy();
    expect(within(desktopSection!).getByText("ChatGPT Desktop")).toBeTruthy();
    expect(screen.queryByText("按引导安装桌面 Agent")).toBeNull();
    const footer = within(screen.getByRole("contentinfo"));
    expect(footer.getByRole("button", { name: "安装 Agent" })).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "安装 Agent" })).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "安装 Agent" }));
    expect(dispatch).toHaveBeenCalledWith({ type: "START_DESKTOP_SETUP" });
    expect(dispatch).not.toHaveBeenCalledWith(expect.objectContaining({ type: "SELECT_AGENT" }));
    expect(await screen.findByRole("heading", { name: "onboarding" })).toBeTruthy();
  });

  it("shows desktop onboarding in the empty desktop section", () => {
    const current = status();
    current.agents.codex.installed = false;
    current.desktopAgents = [
      desktopApp(),
      desktopApp({ id: "workbuddy", name: "WorkBuddy", protocol: "openai", profileAgentId: "workbuddy" }),
    ];
    mockState = { status: current, statusState: "success", statusError: "" };
    renderPage();

    const desktopSection = screen.getByRole("heading", { name: "桌面 Agent" }).closest("section");
    expect(desktopSection).toBeTruthy();
    expect(within(desktopSection!).getByText("共 0 个")).toBeTruthy();
    expect(within(desktopSection!).getByText("按引导安装桌面 Agent")).toBeTruthy();
    const footer = within(screen.getByRole("contentinfo"));
    expect(footer.getByRole("button", { name: "安装 Agent" })).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "安装 Agent" })).toHaveLength(1);
  });

  // Reading order, and the only thing that fixes it: both sections render
  // unconditionally in the installed case, so their order is whatever the JSX
  // says and nothing else would catch a future edit that swaps them back.
  it("puts desktop Agents above command-line Agents", () => {
    const current = status();
    current.desktopAgents = [desktopApp({ installed: true, version: "26.730.61309", profileId: "team" })];
    mockState = { status: current, statusState: "success", statusError: "" };
    renderPage();

    const headings = screen.getAllByRole("heading", { name: /桌面 Agent|命令行 Agent/ });
    expect(headings.map((heading) => heading.textContent)).toEqual(["桌面 Agent", "命令行 Agent"]);
  });

  // The desktop section is the one that can be absent -- on a platform with no
  // supported desktop app it renders nothing -- so the empty-state pair has to
  // hold the order too, not just the populated one.
  it("keeps that order when both sections are empty", () => {
    const current = status();
    current.agents.codex.installed = false;
    current.desktopAgents = [desktopApp()];
    mockState = { status: current, statusState: "success", statusError: "" };
    renderPage();

    const headings = screen.getAllByRole("heading", { name: /桌面 Agent|命令行 Agent/ });
    expect(headings.map((heading) => heading.textContent)).toEqual(["桌面 Agent", "命令行 Agent"]);
  });
});
