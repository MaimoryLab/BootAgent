import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { StatusResponse } from "../types/api";
import { EnvironmentOverviewPage } from "./EnvironmentOverviewPage";

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, refreshStatus: vi.fn() }),
}));

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
    capabilities: { canInstall: {}, supportedAgentIds: [] },
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
    environment: null,
    environmentError: null,
  };
}

describe("EnvironmentOverviewPage", () => {
  it("shows only installed Agents with their Provider and Profile", () => {
    mockState = { status: status(), statusState: "success", statusError: "" };
    render(<EnvironmentOverviewPage />);
    expect(screen.getByText("Codex")).toBeTruthy();
    expect(screen.queryByText("OpenCode")).toBeNull();
    expect(screen.getByText("PPIO")).toBeTruthy();
    expect(screen.getByText("团队默认")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /配置|安装/ })).toBeNull();
  });

  it("keeps an empty environment informational", () => {
    const empty = status();
    empty.agents.codex.installed = false;
    mockState = { status: empty, statusState: "success", statusError: "" };
    render(<EnvironmentOverviewPage />);
    expect(screen.getByText("尚未安装任何 Agent")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /开始|新建/ })).toBeNull();
  });
});
