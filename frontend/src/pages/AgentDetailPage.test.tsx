import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { StatusResponse } from "../types/api";
import { AgentDetailPage } from "./AgentDetailPage";

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch: vi.fn(), refreshStatus: vi.fn() }),
}));

let mockState: { status: StatusResponse | null; statusState: string };

function status(): StatusResponse {
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "bash" },
    capabilities: { canInstall: { codex: true }, supportedAgentIds: ["codex"] },
    agents: {
      codex: {
        installed: true,
        configured: true,
        guideOnly: false,
        config: "/home/u/.codex/config.toml",
        version: "0.144.4",
        lockedVersion: "0.145.0",
        canInstall: true,
        provider: "ppio",
        model: "deepseek/deepseek-v3",
        baseUrl: "https://api.ppio.com/openai",
        updatedAt: "2026-07-27T00:00:00Z",
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
        lockedVersion: "0.145.0",
        protocol: "responses",
        platforms: ["macos"],
        platformNote: "",
        rank: 1,
      },
    ],
    groups: [],
    providers: { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" } },
    mirrors: [],
    paths: { codex_config: "/home/u/.codex/config.toml" },
    backups: { codex: true },
    environment: null,
    environmentError: null,
    profiles: [],
    activeProfile: null,
  };
}

function claudeStatus(): StatusResponse {
  const base = status();
  return {
    ...base,
    capabilities: { canInstall: { "claude-code": true }, supportedAgentIds: ["claude-code"] },
    agents: {
      "claude-code": {
        installed: true,
        configured: true,
        guideOnly: false,
        config: "/home/u/.claude/settings.json",
        version: "2.1.217",
        lockedVersion: "2.1.217",
        canInstall: true,
        provider: "ppio",
        model: "model-a",
        baseUrl: "https://api.ppio.com/anthropic",
        updatedAt: "2026-07-27T00:00:00Z",
        detected: null,
      },
    },
    catalog: [
      {
        id: "claude-code",
        name: "Claude Code",
        group: "auto",
        configMode: "auto",
        guideOnly: false,
        lockedVersion: "2.1.217",
        protocol: "anthropic",
        platforms: ["macos"],
        platformNote: "",
        rank: 2,
      },
    ],
    paths: { "claude-code_config": "/home/u/.claude/settings.json" },
    backups: { "claude-code": false },
  };
}

function renderPage(agentId = "codex", override?: StatusResponse) {
  mockState = { status: override ?? status(), statusState: "success" };
  render(
    <MemoryRouter initialEntries={[`/agents/${agentId}`]}>
      <Routes>
        <Route path="/agents/:agentId" element={<AgentDetailPage />} />
        <Route path="/providers/new" element={<div>新增页占位</div>} />
        <Route path="/overview" element={<div>总览占位</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

function passingProbe() {
  return { ok: true, reachable: true, status: 200, message: "ok", error_code: null, retryable: false };
}

describe("AgentDetailPage", () => {
  it("shows what the Agent is pointed at and where its files live", () => {
    renderPage();
    expect(screen.getByRole("heading", { name: /Codex/ })).toBeTruthy();
    expect(screen.getByText(/deepseek\/deepseek-v3/)).toBeTruthy();
    // Detail view has the room for this; a list row did not.
    expect(screen.getByText("/home/u/.codex/config.toml")).toBeTruthy();
  });

  it("keeps apply disabled until a probe succeeds", () => {
    // Constraint 1: a rejected key must not reach a config file.
    renderPage();
    expect(screen.getByRole("button", { name: /^应用/ }).hasAttribute("disabled")).toBe(true);
  });

  it("offers user Providers in the configuration menu", () => {
    const withUserProvider = status();
    withUserProvider.providers.acme = { name: "Acme", home: "", base_url: "https://api.acme.test", custom: true };
    renderPage("codex", withUserProvider);
    expect(screen.getByRole("option", { name: "Acme" })).toBeTruthy();
    expect(screen.queryByRole("option", { name: "自定义端点" })).toBeNull();
  });

  it("opens the Provider creation page from the picker", () => {
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "新增 Provider" }));
    expect(screen.getByText("新增页占位")).toBeTruthy();
  });

  it("drops a passing verdict when the key is edited afterwards", async () => {
    // Constraint 2: otherwise a wrong key rides in on the previous verdict.
    vi.spyOn(api, "probe").mockResolvedValue(passingProbe());
    renderPage();
    fireEvent.change(screen.getByLabelText(/API Key/i), { target: { value: "sk-good" } });
    fireEvent.click(screen.getByRole("button", { name: /测试连接/ }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^应用/ }).hasAttribute("disabled")).toBe(false),
    );

    fireEvent.change(screen.getByLabelText(/API Key/i), { target: { value: "sk-other" } });
    expect(screen.getByRole("button", { name: /^应用/ }).hasAttribute("disabled")).toBe(true);
  });

  it("reports the restart instruction and clears the key after applying", async () => {
    // Constraints 3 and 4: an Agent reads its config at startup, so silence
    // reads as failure; and a key left in a visible field outlives its request.
    vi.spyOn(api, "probe").mockResolvedValue(passingProbe());
    vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true,
      agent: "codex",
      config: "/home/u/.codex/config.toml",
      provider: "ppio",
      model: "deepseek/deepseek-v3",
      restart: "Quit any running codex process, then start it again",
      next: "source ~/.oneagent/agents/codex.env && codex",
    });

    renderPage();
    const field = screen.getByLabelText(/API Key/i) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "sk-secret" } });
    fireEvent.click(screen.getByRole("button", { name: /测试连接/ }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^应用/ }).hasAttribute("disabled")).toBe(false),
    );

    fireEvent.click(screen.getByRole("button", { name: /^应用/ }));
    await waitFor(() => expect(screen.getByText(/Quit any running codex/)).toBeTruthy());
    await waitFor(() =>
      expect((screen.getByLabelText(/API Key/i) as HTMLInputElement).value).toBe(""),
    );
  });

  it("masks the key and keeps it out of browser storage", () => {
    renderPage();
    const field = screen.getByLabelText(/API Key/i) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "sk-secret-value" } });
    expect(field.type).toBe("password");
    expect(JSON.stringify(localStorage)).not.toContain("sk-secret-value");
    expect(JSON.stringify(sessionStorage)).not.toContain("sk-secret-value");
    expect(document.cookie).not.toContain("sk-secret-value");
  });

  it("keeps the optional model field out of the common path", () => {
    // Leaving the model blank lets the endpoint's own list decide, so it is a
    // choice rather than a step. The main form is Provider, key, test.
    renderPage();
    expect(screen.queryByLabelText("模型")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /高级选项/ }));
    expect(screen.getByLabelText("模型")).toBeTruthy();
  });

  it("refuses an Agent that has no managed configuration", () => {
    renderPage("no-such-agent");
    expect(screen.getByText(/找不到/)).toBeTruthy();
  });

  it("offers Claude Code a fast small-model field and sends it on activate", async () => {
    // The one user-facing difference between adapters: Claude Code runs its
    // background work on a second, optionally cheaper model.
    vi.spyOn(api, "probe").mockResolvedValue(passingProbe());
    const activate = vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true,
      agent: "claude-code",
      config: "/home/u/.claude/settings.json",
      provider: "ppio",
      model: "model-a",
      restart: "Quit any running claude process, then start it again",
      next: "source ~/.oneagent/agents/claude-code.env && claude",
    });
    // The spy persists across tests in this file; drop earlier calls so calls[0]
    // is this test's activation.
    activate.mockClear();

    renderPage("claude-code", claudeStatus());
    fireEvent.click(screen.getByRole("button", { name: /高级选项/ }));
    fireEvent.change(screen.getByLabelText("快速小模型"), { target: { value: "model-fast" } });
    fireEvent.change(screen.getByLabelText(/API Key/i), { target: { value: "sk-secret" } });
    fireEvent.click(screen.getByRole("button", { name: /测试连接/ }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^应用/ }).hasAttribute("disabled")).toBe(false),
    );

    fireEvent.click(screen.getByRole("button", { name: /^应用/ }));
    await waitFor(() => expect(activate.mock.calls[0][1].smallFastModel).toBe("model-fast"));
  });

  it("does not show the small-model field for non-Claude Agents", () => {
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: /高级选项/ }));
    expect(screen.getByLabelText("模型")).toBeTruthy();
    expect(screen.queryByLabelText("快速小模型")).toBeNull();
  });
  it("warns before replacing a configuration OneAgent did not write", () => {
    // A backup is taken either way, but a user who configured this Agent by hand
    // has no way to know Apply will replace it unless the page says so.
    const external = status();
    external.agents.codex.provider = null;
    external.agents.codex.model = null;
    external.agents.codex.detected = {
      baseUrl: "https://api.other-vendor.com/v1",
      model: "gpt-5-mini",
      managedByOneAgent: false,
      unreadable: null,
    };
    renderPage("codex", external);
    const warning = screen.getByText(/不是 OneAgent 写入/).closest(".notice-warning");
    expect(warning).not.toBeNull();
    // The endpoint appears in the facts row too; what matters is that the
    // warning itself names what is about to be replaced, and the backup.
    expect(warning?.textContent).toContain("api.other-vendor.com");
    expect(warning?.textContent).toContain("备份");
  });

  it("does not warn about a configuration it wrote itself", () => {
    const ours = status();
    ours.agents.codex.detected = {
      baseUrl: "https://api.ppio.com/openai",
      model: "deepseek/deepseek-v3",
      managedByOneAgent: true,
      unreadable: null,
    };
    renderPage("codex", ours);
    expect(screen.queryByText(/不是 OneAgent 写入/)).toBeNull();
  });

  it("shows the Provider key again when the page is reopened", async () => {
    const saved = status();
    saved.providers.ppio.has_key = true;
    vi.spyOn(api, "getProvider").mockResolvedValue({
      id: "ppio", name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai",
      anthropic_base_url: "https://api.ppio.com/anthropic", api_key: "sk-persisted", built_in: true,
    });
    renderPage("codex", saved);
    await waitFor(() => expect(screen.getByLabelText(/API Key/i)).toHaveValue("sk-persisted"));
  });
});
