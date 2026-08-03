import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { StatusResponse } from "../types/api";
import { ProvidersPage } from "./ProvidersPage";

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch: vi.fn(), refreshStatus: vi.fn() }),
}));

let mockState: { status: StatusResponse | null; statusState: string };

function statusWith(agents: Record<string, string | null>): StatusResponse {
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "bash" },
    runtimes: [],
    capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
    agents: Object.fromEntries(
      Object.entries(agents).map(([id, provider]) => [
        id,
        {
          installed: true,
          configured: Boolean(provider),
          guideOnly: false,
          config: "/c",
          version: "1.0.0",
          lockedVersion: "1.0.0",
          canInstall: true,
          provider,
          profileId: null,
          model: provider ? "some-model" : null,
          baseUrl: null,
          updatedAt: null,
          detected: null,
        },
      ]),
    ),
    catalog: Object.keys(agents).map((id) => ({
      id,
      name: id === "claude-code" ? "Claude Code" : id,
      group: "auto" as const,
      configMode: "auto" as const,
      guideOnly: false,
      lockedVersion: "1.0.0",
      protocol: "openai" as const,
      platforms: ["macos" as const],
      platformNote: "",
      rank: 1,
    })),
    groups: [],
    providers: {
      ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai", anthropic_base_url: "https://api.ppio.com/anthropic" },
      novita: { name: "Novita", home: "https://novita.ai/", base_url: "https://api.novita.ai/openai" },
    },
    mirrors: [],
    paths: {},
    backups: {},
    environment: null,
    environmentError: null,
    profiles: [],
    activeProfile: null,
  };
}

function renderPage(agents: Record<string, string | null>) {
  mockState = { status: statusWith(agents), statusState: "success" };
  render(
    <MemoryRouter>
      <ProvidersPage />
    </MemoryRouter>,
  );
}

describe("ProvidersPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists each Provider with its endpoint", () => {
    renderPage({ codex: "ppio" });
    expect(screen.getByText("PPIO")).toBeTruthy();
    expect(screen.getByText("Novita")).toBeTruthy();
    expect(screen.getByText("https://api.ppio.com/openai")).toBeTruthy();
  });

  it("shows which Agents point at a Provider", () => {
    // Only meaningful once each Agent has its own binding: this is the reverse
    // lookup that answers "what breaks if this Provider goes down".
    renderPage({ codex: "ppio", "claude-code": "novita", opencode: "ppio" });
    const ppio = screen.getByTestId("provider-ppio");
    expect(ppio.textContent).toContain("codex");
    expect(ppio.textContent).toContain("opencode");
    expect(ppio.textContent).not.toContain("claude-code");
  });

  it("says so when no Agent uses a Provider", () => {
    renderPage({ codex: "ppio" });
    expect(screen.getByTestId("provider-novita").textContent).toMatch(/暂无/);
  });

  it("states that a user Provider is the user's responsibility", () => {
    // ADR-003 puts protocol compatibility on the user; the UI has to
    // say it rather than leave it in a document.
    renderPage({ codex: "ppio" });
    expect(screen.getByText(/用户 Provider/)).toBeTruthy();
  });

  it("loads the saved API key when editing and sends updates", async () => {
    // This page owns the key, so the editor has to show and round-trip it.
    const entry = {
      id: "ppio", name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai",
      anthropic_base_url: "https://api.ppio.com/anthropic", api_key: "sk-saved", built_in: true,
    };
    vi.spyOn(api, "getProvider").mockResolvedValue(entry);
    const save = vi.spyOn(api, "saveProvider").mockResolvedValue(entry);
    renderPage({ codex: "ppio" });

    fireEvent.click(screen.getByRole("button", { name: "编辑 PPIO" }));
    await waitFor(() => expect(screen.getByLabelText("API Key")).toHaveValue("sk-saved"));
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "PPIO Cloud" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));

    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ id: "ppio", name: "PPIO Cloud", api_key: "sk-saved" })));
  });

  it("adds a Provider from the management page", async () => {
    const save = vi.spyOn(api, "saveProvider").mockResolvedValue({
      id: "acme", name: "Acme", home: "", base_url: "https://api.acme.test",
      anthropic_base_url: "", api_key: "sk-acme", built_in: false,
    });
    renderPage({ codex: null });
    fireEvent.click(screen.getByRole("button", { name: "新增 Provider" }));
    fireEvent.change(screen.getByLabelText("Provider ID"), { target: { value: "acme" } });
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "Acme" } });
    fireEvent.change(screen.getByLabelText("OpenAI 兼容 Base URL"), { target: { value: "https://api.acme.test" } });
    fireEvent.change(screen.getByLabelText("API Key"), { target: { value: "sk-acme" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ id: "acme", base_url: "https://api.acme.test", api_key: "sk-acme" })));
  });

  it("deletes a user Provider", async () => {
    renderPage({ codex: null });
    if (!mockState.status) throw new Error("missing status");
    mockState.status.providers.acme = { name: "Acme", home: "", base_url: "https://api.acme.test", custom: true };
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const remove = vi.spyOn(api, "deleteProvider").mockResolvedValue();
    render(
      <MemoryRouter>
        <ProvidersPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "删除 Acme" }));
    await waitFor(() => expect(remove).toHaveBeenCalledWith("acme"));
  });
});
