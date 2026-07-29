import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

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
    capabilities: { canInstall: {}, supportedAgentIds: [] },
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

  it("states that a custom endpoint is the user's responsibility", () => {
    // ADR-003 puts protocol compatibility on the user for Custom; the UI has to
    // say it rather than leave it in a document.
    renderPage({ codex: "ppio" });
    expect(screen.getByText(/自定义/)).toBeTruthy();
  });
});
