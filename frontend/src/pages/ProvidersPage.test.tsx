import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { StatusResponse } from "../types/api";
import { ProvidersPage } from "./ProvidersPage";

// Hoisted so the module factory can close over it: vi.mock is lifted above the
// imports, so a plain const here would not exist yet when the factory runs.
const { question } = vi.hoisted(() => ({ question: vi.fn<(options: { Message: string }) => Promise<string>>() }));
vi.mock("@wailsio/runtime", () => ({ Dialogs: { Question: question } }));

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
          latestVersion: null,
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
      latestVersion: null,
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
    desktopAgents: [],
    profiles: [],
    activeProfile: null,
    firstRun: false,
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
  afterEach(() => {
    vi.restoreAllMocks();
    question.mockReset();
  });

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

  it("explains why an in-use Provider cannot be deleted", () => {
    mockState = { status: statusWith({ codex: "acme" }), statusState: "success" };
    if (!mockState.status) throw new Error("missing status");
    mockState.status.providers.acme = { name: "Acme", home: "", base_url: "https://api.acme.test", custom: true };
    const remove = vi.spyOn(api, "deleteProvider");
    render(<MemoryRouter><ProvidersPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "删除 Acme" }));
    expect(screen.getByText(/模型服务正在被.*使用，无法删除/)).toBeTruthy();
    expect(remove).not.toHaveBeenCalled();
  });

  it("says so when no Agent uses a Provider", () => {
    renderPage({ codex: "ppio" });
    expect(screen.getByTestId("provider-novita").textContent).toMatch(/暂无/);
  });

  it("states that a user Provider is the user's responsibility", () => {
    // ADR-003 puts protocol compatibility on the user; the UI has to
    // say it rather than leave it in a document.
    renderPage({ codex: "ppio" });
    expect(screen.getByText(/用户模型服务/)).toBeTruthy();
  });

  it("loads the saved API key when editing and sends updates", async () => {
    // This page owns the key, so the editor has to show and round-trip it.
    const entry = {
      id: "ppio", name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai",
      anthropic_base_url: "https://api.ppio.com/anthropic", api_key: "sk-saved", built_in: true,
    };
    vi.spyOn(api, "getProvider").mockResolvedValue(entry);
    const save = vi.spyOn(api, "saveProvider").mockResolvedValue({ entry, reapplied: null, failures: null });
    renderPage({ codex: "ppio" });

    fireEvent.click(screen.getByRole("button", { name: "编辑 PPIO" }));
    await waitFor(() => expect(screen.getByLabelText("API Key")).toHaveValue("sk-saved"));
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "PPIO Cloud" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));

    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ id: "ppio", name: "PPIO Cloud", api_key: "sk-saved" })));
  });

  it("opens the requested Provider editor", async () => {
    const entry = {
      id: "ppio", name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai",
      anthropic_base_url: "", api_key: "sk-saved", built_in: true,
    };
    const get = vi.spyOn(api, "getProvider").mockResolvedValue(entry);
    mockState = { status: statusWith({ codex: "ppio" }), statusState: "success" };

    render(
      <MemoryRouter initialEntries={["/providers?provider=ppio"]}>
        <ProvidersPage />
      </MemoryRouter>,
    );

    await waitFor(() => expect(get).toHaveBeenCalledWith("ppio"));
    expect(await screen.findByLabelText("API Key")).toHaveValue("sk-saved");
  });

  it("names the Agents rewritten after an endpoint or key edit", async () => {
    // A Provider edit re-applies to every Agent bound to it, so the page has to
    // say which ones changed rather than leaving it invisible.
    const entry = {
      id: "ppio", name: "PPIO", home: "", base_url: "https://api.ppio.com/openai",
      anthropic_base_url: "", api_key: "sk-rotated", built_in: true,
    };
    vi.spyOn(api, "getProvider").mockResolvedValue(entry);
    vi.spyOn(api, "saveProvider").mockResolvedValue({ entry, reapplied: ["codex", "claude-code"], failures: null });
    renderPage({ codex: "ppio", "claude-code": "ppio" });

    fireEvent.click(screen.getByRole("button", { name: "编辑 PPIO" }));
    await waitFor(() => expect(screen.getByLabelText("API Key")).toBeTruthy());
    fireEvent.change(screen.getByLabelText("API Key"), { target: { value: "sk-rotated" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));

    await waitFor(() => expect(screen.getByText(/已重新应用到.*Claude Code/)).toBeTruthy());
  });

  it("reports an Agent that could not be rewritten", async () => {
    const entry = {
      id: "ppio", name: "PPIO", home: "", base_url: "https://api.ppio.com/openai",
      anthropic_base_url: "", api_key: "sk-rotated", built_in: true,
    };
    vi.spyOn(api, "getProvider").mockResolvedValue(entry);
    vi.spyOn(api, "saveProvider").mockResolvedValue({
      entry, reapplied: null, failures: { "claude-code": "model is required" },
    });
    renderPage({ "claude-code": "ppio" });

    fireEvent.click(screen.getByRole("button", { name: "编辑 PPIO" }));
    await waitFor(() => expect(screen.getByLabelText("API Key")).toBeTruthy());
    fireEvent.change(screen.getByLabelText("API Key"), { target: { value: "sk-rotated" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));

    await waitFor(() => expect(screen.getByText(/Claude Code 重新应用失败：model is required/)).toBeTruthy());
  });

  it("adds a Provider from the management page", async () => {
    const save = vi.spyOn(api, "saveProvider").mockResolvedValue({
      entry: {
        id: "acme", name: "Acme", home: "", base_url: "https://api.acme.test",
        anthropic_base_url: "", api_key: "sk-acme", built_in: false,
      },
      reapplied: null,
      failures: null,
    });
    renderPage({ codex: null });
    fireEvent.click(screen.getByRole("button", { name: "新增模型服务" }));
    fireEvent.change(screen.getByLabelText("模型服务 ID"), { target: { value: "acme" } });
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "Acme" } });
    fireEvent.change(screen.getByLabelText("OpenAI 兼容 Base URL"), { target: { value: "https://api.acme.test" } });
    fireEvent.change(screen.getByLabelText("API Key"), { target: { value: "sk-acme" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ id: "acme", base_url: "https://api.acme.test", api_key: "sk-acme", create: true })));
  });

  // The ID is a storage key the user should not have to invent, but a collision is
  // now refused rather than silently overwriting -- so the suggested value has to
  // be one that is actually free.
  it("prefills a free Provider ID and states the rule", () => {
    renderPage({ codex: null });
    fireEvent.click(screen.getByRole("button", { name: "新增模型服务" }));
    const id = screen.getByLabelText("模型服务 ID") as HTMLInputElement;
    expect(id.value).toMatch(/^[a-z0-9][a-z0-9-]*$/);
    expect(Object.keys(mockState.status?.providers ?? {})).not.toContain(id.value);
    expect(screen.getByText(/小写字母、数字或连字符/)).toBeTruthy();
  });

  // create separates the two intents. An edit must keep overwriting, or saving a
  // Provider you opened from the list would be refused as a duplicate of itself.
  it("saves an edited Provider without the create flag", async () => {
    const save = vi.spyOn(api, "saveProvider").mockResolvedValue({
      entry: {
        id: "ppio", name: "PPIO Cloud", home: "", base_url: "https://api.ppio.com/openai",
        anthropic_base_url: "", api_key: "", built_in: true,
      },
      reapplied: null,
      failures: null,
    });
    vi.spyOn(api, "getProvider").mockResolvedValue({
      id: "ppio", name: "PPIO", home: "", base_url: "https://api.ppio.com/openai",
      anthropic_base_url: "", api_key: "", built_in: true,
    });
    renderPage({ codex: "ppio" });
    fireEvent.click(screen.getByRole("button", { name: "编辑 PPIO" }));
    await waitFor(() => expect(screen.getByLabelText("名称")).toBeTruthy());
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "PPIO Cloud" } });
    fireEvent.click(screen.getByRole("button", { name: /^保存$/ }));
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ id: "ppio", create: false })));
  });

  // As on the Profiles page, this used to stub window.confirm, which the page
  // never called -- so it passed against an unconfirmed delete. The declined case
  // is what holds the confirmation in place.
  const renderWithUserProvider = () => {
    renderPage({ codex: null });
    if (!mockState.status) throw new Error("missing status");
    mockState.status.providers.acme = { name: "Acme", home: "", base_url: "https://api.acme.test", custom: true };
    render(
      <MemoryRouter>
        <ProvidersPage />
      </MemoryRouter>,
    );
  };

  it("does not delete a Provider when the confirmation is declined", async () => {
    question.mockResolvedValue("取消");
    const remove = vi.spyOn(api, "deleteProvider").mockResolvedValue();
    renderWithUserProvider();
    fireEvent.click(screen.getByRole("button", { name: "删除 Acme" }));
    await waitFor(() => expect(question).toHaveBeenCalled());
    expect(remove).not.toHaveBeenCalled();
  });

  it("deletes a user Provider once the confirmation is accepted", async () => {
    question.mockResolvedValue("删除");
    const remove = vi.spyOn(api, "deleteProvider").mockResolvedValue();
    renderWithUserProvider();
    fireEvent.click(screen.getByRole("button", { name: "删除 Acme" }));
    await waitFor(() => expect(remove).toHaveBeenCalledWith("acme"));
    // The saved key going too is the part worth warning about.
    expect(question.mock.calls[0][0].Message).toContain("API Key");
  });
});
