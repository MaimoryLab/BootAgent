import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

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

function renderPage(agentId = "codex") {
  mockState = { status: status(), statusState: "success" };
  render(
    <MemoryRouter initialEntries={[`/agents/${agentId}`]}>
      <Routes>
        <Route path="/agents/:agentId" element={<AgentDetailPage />} />
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

  it("drops a passing verdict when the key is edited afterwards", async () => {
    // Constraint 2: otherwise a wrong key rides in on the previous verdict.
    const { api } = await import("../api/client");
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
    const { api } = await import("../api/client");
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
});
