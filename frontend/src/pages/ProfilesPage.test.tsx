import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import type { ProfileSummary, StatusResponse } from "../types/api";
import { ProfilesPage } from "./ProfilesPage";

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch: vi.fn(), refreshStatus: vi.fn() }),
}));

let mockState: { status: StatusResponse | null; statusState: string };

function statusWith(profiles: ProfileSummary[]): StatusResponse {
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "bash" },
    capabilities: { canInstall: {}, supportedAgentIds: [] },
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
        model: "deepseek/deepseek-v3",
        baseUrl: null,
        updatedAt: null,
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
  it("lists a template with its provider and model", () => {
    renderPage([profile()]);
    expect(screen.getByText("团队 PPIO")).toBeTruthy();
    expect(screen.getByText(/deepseek\/deepseek-v3/)).toBeTruthy();
  });

  it("reports only whether a key is held, never the key", () => {
    // A template can carry a credential; the page may say it exists and
    // nothing more. There is no API that would return it and none should be
    // added for display.
    renderPage([profile({ hasKey: true })]);
    expect(screen.getByText(/已保存密钥/)).toBeTruthy();
    const markup = document.body.innerHTML;
    expect(markup).not.toMatch(/sk-[A-Za-z0-9]/);
    expect(JSON.stringify(localStorage)).not.toMatch(/sk-/);
  });

  it("distinguishes a template with no key", () => {
    renderPage([profile({ hasKey: false })]);
    expect(screen.getByText(/未保存密钥/)).toBeTruthy();
  });

  it("offers to build a template from a configured Agent when empty", () => {
    // Matching real order of use: people configure an Agent first and only then
    // want to reuse that combination.
    renderPage([]);
    expect(screen.getByText(/还没有配置模板/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /从现有 Agent/ })).toBeTruthy();
  });

  it("lets a template be applied to a specific Agent", () => {
    // Applying is the whole point of a template. Deletion needs a backend
    // endpoint that does not exist yet, so it is deliberately absent rather
    // than present and broken.
    renderPage([profile()]);
    expect(screen.getByRole("button", { name: /应用到/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /删除/ })).toBeNull();
  });
});
