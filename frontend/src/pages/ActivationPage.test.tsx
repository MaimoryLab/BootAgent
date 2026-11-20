import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { initialWizardState, type WizardState } from "../state/wizardReducer";
import type { AgentInstallResult, StatusResponse } from "../types/api";
import { ActivationPage } from "./ActivationPage";

// No I18nProvider, so translate() returns the Chinese keys unchanged.
let state: WizardState;
const refreshStatus = vi.fn(async () => {});
/** Task ids the mocked task centre should report as cancelled. */
const cancelledTargets = new Set<string>();
/** Makes startTask refuse, as it does when a task with the same key is running. */
let refuseStart = false;

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state, dispatch: vi.fn(), refreshStatus }),
}));

vi.mock("../state/TaskCenterContext", async (importOriginal) => ({
  ...await importOriginal<typeof import("../state/TaskCenterContext")>(),
  useTaskCenter: () => ({
    tasks: [], startTask: () => !refuseStart, finishTask: vi.fn(),
    setTaskCanceller: vi.fn(),
    taskFor: (id: string) => (cancelledTargets.has(id) ? { state: "cancelled" } : undefined),
  }),
  useTaskRoute: () => "/setup/activation",
}));

const status = {
  providers: {}, catalog: [
    { id: "codex", name: "Codex" },
    { id: "aider", name: "Aider" },
  ],
  capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
  runtimes: [], agents: {}, profiles: [],
} as unknown as StatusResponse;

const result = (agent: string, over: Partial<AgentInstallResult> = {}): AgentInstallResult =>
  ({ agent, status: "configured", retryable: false, ...over });

function show(results: AgentInstallResult[], next = "") {
  state = {
    ...initialWizardState,
    status,
    statusState: "success",
    selectedAgentIds: ["codex", "aider"],
    // Not "success": a run with a failed row lands on the error state, which is
    // exactly the case that had no way forward.
    activationState: "error",
    activationRequested: true,
    activationResults: results,
    activationNext: next,
  };
  render(<MemoryRouter><ActivationPage /></MemoryRouter>);
}

describe("ActivationPage partial results", () => {
  beforeEach(() => { vi.restoreAllMocks(); refreshStatus.mockClear(); cancelledTargets.clear(); refuseStart = false; });

  // One Agent configured and another failed unretryably left no forward button
  // and no retry button -- the only exit was back to the review step, even though
  // the configured Agent is usable.
  it("offers the overview when at least one Agent was configured", () => {
    show([result("codex"), result("aider", { status: "failed", retryable: false })]);
    expect(screen.getByRole("button", { name: "进入总览" })).toBeTruthy();
  });

  it("offers no way forward when every Agent failed", () => {
    // Nothing was configured, so the overview would show no new state; the back
    // link remains the honest exit.
    show([
      result("codex", { status: "failed", retryable: false }),
      result("aider", { status: "failed", retryable: false }),
    ]);
    expect(screen.queryByRole("button", { name: "进入总览" })).toBeNull();
  });

  // The commands belong to the Agents that succeeded. Hiding them because a
  // different Agent failed withheld the next step for work that was done -- and
  // for OpenClaw that command is the one that makes it usable at all.
  it("still shows the next-step command on a partial run", () => {
    show([result("codex"), result("aider", { status: "failed", retryable: false })], "openclaw onboard");
    expect(screen.getByText("openclaw onboard")).toBeTruthy();
  });

  // A cancel can land between steps -- after a runtime tree is published but
  // before the Agent package installs, or after config is written. The row is
  // synthesised as skipped with retryable: false, so there is no retry button
  // either; saying only "已取消" left the user unable to tell whether re-running
  // was safe.
  // A mid-loop collision rolls back the tasks it already started. Reusing the
  // collision message for those made an unrelated Agent report "this task is
  // already running", pointing the user at the wrong row and at a task that was
  // never theirs.
  it("names where to look when another install is already running", async () => {
    // startTask refuses the first target, so activate() reports the collision
    // instead of starting a run. The old copy said only that a task existed.
    refuseStart = true;
    state = {
      ...initialWizardState,
      status,
      statusState: "success",
      selectedAgentIds: ["codex", "aider"],
      activationState: "idle",
      activationRequested: true,
      activationResults: [],
    };
    render(<MemoryRouter><ActivationPage /></MemoryRouter>);
    // activate() runs from an effect, so the notice appears after a tick.
    await waitFor(() => expect(screen.getByText(/任务中心/)).toBeTruthy());
  });

  it("says what a cancelled run left behind", () => {
    // activationCancelled is derived from the task centre, not wizard state.
    cancelledTargets.add("install:codex");
    state = {
      ...initialWizardState,
      status,
      statusState: "success",
      selectedAgentIds: ["codex"],
      activationState: "error",
      activationRequested: true,
      activationResults: [],
    };
    render(<MemoryRouter><ActivationPage /></MemoryRouter>);
    expect(screen.getByText(/重新运行是安全的/)).toBeTruthy();
  });

  it("hides the next-step command when nothing succeeded", () => {
    show([result("codex", { status: "failed", retryable: false })], "openclaw onboard");
    expect(screen.queryByText("openclaw onboard")).toBeNull();
  });

  it("installs without configuring, then points the Agent at the adapter in compatibility mode", async () => {
    const install = vi.spyOn(api, "install").mockResolvedValue({
      ok: true, results: [result("codex", { status: "skipped" })], log: "", probe: null, next: "",
    } as never);
    const saveProfile = vi.spyOn(api, "saveProfile").mockResolvedValue({
      profile: { id: "codex-ppio-compat", label: "Codex · PPIO", provider: "ppio", baseUrl: null, model: "deepseek/deepseek-v3", protocol: "openai", activatedAt: null },
      reapplied: null,
      failures: null,
    });
    const activate = vi.spyOn(api, "activateAgentWithConversion").mockResolvedValue({
      ok: true, agent: "codex", config: "/c", provider: "bootagent-converter-responses", model: "gpt-5.6-sol",
      restart: "", next: "codex", upstreamVerified: true, endToEndVerified: true,
    });
    state = {
      ...initialWizardState,
      status,
      statusState: "success",
      setupKind: "cli",
      selectedAgentIds: ["codex"],
      provider: "ppio",
      model: "deepseek/deepseek-v3",
      profileId: "codex-ppio-compat",
      profileLabel: "Codex · PPIO",
      conversion: true,
      activationState: "idle",
      activationRequested: true,
    };

    render(<MemoryRouter><ActivationPage /></MemoryRouter>);
    await waitFor(() => expect(activate).toHaveBeenCalledWith("codex", "codex-ppio-compat"));
    expect(install).toHaveBeenCalledWith(expect.objectContaining({ agents: ["codex"], configure: false, install_agent: true }));
    expect(saveProfile).toHaveBeenCalledWith(expect.objectContaining({ id: "codex-ppio-compat", provider: "ppio", protocol: "openai" }));
  });

  it("refreshes after a failed compatibility mode step, so the saved Profile is visible", async () => {
    vi.spyOn(api, "install").mockResolvedValue({
      ok: true, results: [result("codex", { status: "skipped" })], log: "", probe: null, next: "",
    } as never);
    vi.spyOn(api, "saveProfile").mockResolvedValue({
      profile: { id: "codex-ppio-compat", label: "Codex · PPIO", provider: "ppio", baseUrl: null, model: "m", protocol: "openai", activatedAt: null },
      reapplied: null,
      failures: null,
    });
    const { BootAgentApiError } = await import("../backend/errors");
    vi.spyOn(api, "activateAgentWithConversion").mockRejectedValue(
      new BootAgentApiError("port in use", "CONVERSION_PORT_UNAVAILABLE", true, 409),
    );
    state = {
      ...initialWizardState,
      status, statusState: "success", setupKind: "cli", selectedAgentIds: ["codex"],
      provider: "ppio", model: "m", profileId: "codex-ppio-compat", profileLabel: "Codex · PPIO",
      conversion: true, activationState: "idle", activationRequested: true,
    };

    render(<MemoryRouter><ActivationPage /></MemoryRouter>);
    await waitFor(() => expect(refreshStatus).toHaveBeenCalled());
  });

  it("configures a manual desktop app without requesting an install", async () => {
    vi.spyOn(api, "saveProfile").mockResolvedValue({
      profile: { id: "claude-desktop-jiekou", label: "Claude Desktop", provider: "jiekou", baseUrl: null, model: "claude-sonnet-4-5", protocol: "anthropic", activatedAt: null },
      reapplied: null,
      failures: null,
    });
    const install = vi.spyOn(api, "installDesktopAgent");
    const configure = vi.spyOn(api, "configureDesktopAgent").mockResolvedValue({
      agent: "claude-desktop",
      profileId: "claude-desktop-jiekou",
      profileAgentId: "claude-desktop",
      config: "/tmp/Claude-3p/claude_desktop_config.json",
      message: "configured",
    });
    state = {
      ...initialWizardState,
      status: {
        ...status,
        desktopAgents: [{
          id: "claude-desktop", name: "Claude Desktop", installed: false, supported: true,
          version: null, source: "manual", protocol: "anthropic", profileAgentId: "claude-desktop",
          profileId: null, manualInstall: true, home: "https://claude.com/download",
        }],
      },
      statusState: "success",
      selectedAgentIds: ["claude-desktop"],
      model: "claude-sonnet-4-5",
      activationState: "idle",
      activationRequested: true,
    };

    render(<MemoryRouter><ActivationPage /></MemoryRouter>);
    await waitFor(() => expect(configure).toHaveBeenCalledWith("claude-desktop", "claude-desktop-jiekou"));
    expect(install).not.toHaveBeenCalled();
  });

  it("opens the official download page after configuring Claude Desktop", async () => {
    const install = vi.spyOn(api, "installDesktopAgent").mockResolvedValue({
      status: "installer-opened", message: "opened", refreshNeeded: false,
      app: { id: "claude-desktop", name: "Claude Desktop", installed: false, supported: true, version: null, source: "manual", protocol: "anthropic", profileAgentId: "claude-desktop", profileId: null },
    });
    state = {
      ...initialWizardState,
      status: {
        ...status,
        desktopAgents: [{
          id: "claude-desktop", name: "Claude Desktop", installed: false, supported: true,
          version: null, source: "manual", protocol: "anthropic", profileAgentId: "claude-desktop",
          profileId: null, manualInstall: true, home: "https://claude.com/download",
        }],
      },
      statusState: "success",
      selectedAgentIds: ["claude-desktop"],
      activationState: "success",
      activationResults: [result("claude-desktop")],
    };

    render(<MemoryRouter><ActivationPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "前往 Claude Desktop 下载" }));
    await waitFor(() => expect(install).toHaveBeenCalledWith("claude-desktop"));
  });
});
