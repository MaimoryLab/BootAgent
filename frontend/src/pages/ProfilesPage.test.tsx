import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { ProfileSummary, StatusResponse } from "../types/api";
import { ProfilesPage } from "./ProfilesPage";

const refreshStatus = vi.fn<() => Promise<void>>();

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch: vi.fn(), refreshStatus }),
}));

let mockState: { status: StatusResponse | null; statusState: string };

function statusWith(profiles: ProfileSummary[]): StatusResponse {
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "bash" },
    runtimes: [],
    capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
    agents: {
      codex: {
        installed: true,
        configured: true,
        guideOnly: false,
        config: "/c",
        version: "1.0.0",
        lockedVersion: "1.0.0",
        canInstall: true,
        provider: "ppio",
        profileId: "team-ppio",
        model: "deepseek/deepseek-v3",
        baseUrl: null,
        updatedAt: null,
        detected: null,
      },
    },
    catalog: [
      {
        id: "codex",
        name: "Codex",
        group: "auto",
        configMode: "auto",
        guideOnly: false,
        lockedVersion: "1.0.0",
        protocol: "responses",
        platforms: ["macos"],
        platformNote: "",
        rank: 1,
      },
    ],
    groups: [],
    providers: { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" } },
    mirrors: [],
    paths: {},
    backups: {},
    environment: null,
    environmentError: null,
    profiles,
    activeProfile: null,
  };
}

function profile(over: Partial<ProfileSummary> = {}): ProfileSummary {
  return {
    id: "team-ppio",
    label: "团队 PPIO",
    provider: "ppio",
    model: "deepseek/deepseek-v3",
    baseUrl: null,
    agentIds: ["codex"],
    hasKey: true,
    activatedAt: null,
    ...over,
  };
}

function renderPage(profiles: ProfileSummary[]) {
  mockState = { status: statusWith(profiles), statusState: "success" };
  render(
    <MemoryRouter>
      <ProfilesPage />
    </MemoryRouter>,
  );
}

describe("ProfilesPage", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    refreshStatus.mockReset();
    refreshStatus.mockResolvedValue();
  });

  it("lists public Profile details without returning the key", () => {
    renderPage([profile()]);
    expect(screen.getByText("团队 PPIO")).toBeTruthy();
    expect(screen.getByText(/deepseek\/deepseek-v3/)).toBeTruthy();
    expect(screen.getByText(/已保存密钥/)).toBeTruthy();
    expect(document.body.innerHTML).not.toMatch(/sk-[A-Za-z0-9]/);
  });

  it("starts Profile creation from the template manager", () => {
    renderPage([]);
    expect(screen.getByText(/还没有 Profile/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "新增 Profile" }));
    expect(screen.getByRole("heading", { name: "配置模板" })).toBeTruthy();
    expect(screen.getByLabelText("Profile ID")).toBeTruthy();
  });

  it("creates a Profile with Provider, model, key, and Agents", async () => {
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue(profile());
    renderPage([]);
    fireEvent.click(screen.getByRole("button", { name: "新增 Profile" }));
    fireEvent.change(screen.getByLabelText("Profile ID"), { target: { value: "team-ppio" } });
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "团队 PPIO" } });
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "deepseek/deepseek-v3" } });
    fireEvent.change(screen.getByLabelText("API Key"), { target: { value: "sk-new" } });
    fireEvent.click(screen.getByLabelText("选择 Codex"));
    fireEvent.click(screen.getByRole("button", { name: "保存 Profile" }));

    await waitFor(() => expect(save).toHaveBeenCalledWith({
      id: "team-ppio",
      label: "团队 PPIO",
      provider: "ppio",
      apiBaseUrl: "",
      apiKey: "sk-new",
      model: "deepseek/deepseek-v3",
      configMode: "provider",
      agentIds: ["codex"],
    }));
  });

  it("edits metadata without asking the backend to replace a saved key", async () => {
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue(profile({ label: "团队默认" }));
    renderPage([profile()]);
    fireEvent.click(screen.getByRole("button", { name: "编辑 团队 PPIO" }));
    expect(screen.getByLabelText("Profile ID").hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/留空将保留/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "团队默认" } });
    fireEvent.click(screen.getByRole("button", { name: "保存 Profile" }));

    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({
      id: "team-ppio",
      label: "团队默认",
      apiKey: "",
    })));
  });

  it("applies one Profile to all of its Agents", async () => {
    const install = vi.spyOn(api, "install").mockResolvedValue({
      ok: true,
      code: 0,
      results: [{ agent: "codex", status: "configured", retryable: false }],
      log: "",
      next: "",
      probe: null,
      probes: {},
    });
    renderPage([profile()]);
    fireEvent.click(screen.getByRole("button", { name: "应用到 Agent" }));

    await waitFor(() => expect(install).toHaveBeenCalledWith(expect.objectContaining({
      agents: ["codex"],
      profile_id: "team-ppio",
      provider: "ppio",
      api_key: "",
      model: "deepseek/deepseek-v3",
      install_agent: true,
    })));
    await waitFor(() => expect(screen.getByText(/已应用到 1 个 Agent/)).toBeTruthy());
  });
});
