import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { initialWizardState, type WizardState } from "../state/wizardReducer";
import type { AgentInstallResult, StatusResponse } from "../types/api";
import { ActivationPage } from "./ActivationPage";

// No I18nProvider, so translate() returns the Chinese keys unchanged.
let state: WizardState;
const refreshStatus = vi.fn(async () => {});

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state, dispatch: vi.fn(), refreshStatus }),
}));

vi.mock("../state/TaskCenterContext", async (importOriginal) => ({
  ...await importOriginal<typeof import("../state/TaskCenterContext")>(),
  useTaskCenter: () => ({
    tasks: [], startTask: vi.fn(() => true), finishTask: vi.fn(),
    setTaskCanceller: vi.fn(), taskFor: vi.fn(() => undefined),
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
  beforeEach(() => refreshStatus.mockClear());

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

  it("hides the next-step command when nothing succeeded", () => {
    show([result("codex", { status: "failed", retryable: false })], "openclaw onboard");
    expect(screen.queryByText("openclaw onboard")).toBeNull();
  });
});
