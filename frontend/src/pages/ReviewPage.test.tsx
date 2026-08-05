import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { initialWizardState, type WizardState } from "../state/wizardReducer";
import type { StatusResponse } from "../types/api";
import { ReviewPage } from "./ReviewPage";

const dispatch = vi.fn();
let state: WizardState;

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state, dispatch }),
}));

const status = {
  apiVersion: 1,
  platform: { os: "macos", arch: "arm64", shell: "bash" },
  runtimes: [],
  capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
  agents: { codex: { installed: false, guideOnly: false } },
  catalog: [{ id: "codex", name: "Codex", group: "auto", configMode: "auto", guideOnly: false, platforms: ["macos"] }],
  groups: [],
  providers: { ppio: { name: "PPIO", base_url: "https://api.ppinfra.com/openai", has_key: true } },
  mirrors: [],
  paths: { codex_config: "~/.codex/config.toml", profile: "~/.oneagent/profile.json" },
  backups: {},
  profiles: [],
  activeProfile: null,
  environment: null,
  environmentError: null,
  desktopAgents: [],
  firstRun: true,
} as unknown as StatusResponse;

function renderPage(over: Partial<WizardState> = {}) {
  dispatch.mockClear();
  state = {
    ...initialWizardState,
    status,
    statusState: "success",
    selectedAgentIds: ["codex"],
    provider: "ppio",
    model: "deepseek/deepseek-v3",
    hasApiKey: false,
    keyVerified: true,
    ...over,
  };
  render(
    <MemoryRouter initialEntries={["/setup/review"]}>
      <Routes>
        <Route path="/setup/review" element={<ReviewPage />} />
        <Route path="/setup/provider" element={<h1>provider step</h1>} />
        <Route path="/setup/activation" element={<h1>installing</h1>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("ReviewPage", () => {
  it("names the Profile after the Agent and Provider, and lets the user override it", () => {
    renderPage();
    const field = screen.getByLabelText("配置模板名称") as HTMLInputElement;
    expect(field.value).toBe("Codex · PPIO");
    fireEvent.change(field, { target: { value: "团队 PPIO" } });
    expect(dispatch).toHaveBeenCalledWith({ type: "SET_PROFILE_LABEL", value: "团队 PPIO" });
  });

  it("commits a derived id and label before starting the install", () => {
    // The install writes the Profile, so the id has to be settled here; leaving
    // it empty would let the backend fall back to "default" and overwrite the
    // Profile from an earlier run.
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: /开始安装/ }));
    expect(dispatch).toHaveBeenCalledWith({ type: "SET_PROFILE_ID", value: "codex-ppio" });
    expect(dispatch).toHaveBeenCalledWith({ type: "SET_PROFILE_LABEL", value: "Codex · PPIO" });
    expect(dispatch).toHaveBeenCalledWith({ type: "REQUEST_ACTIVATION" });
  });

  it("sends the user back for a key instead of installing without one", async () => {
    // The wizard reuses Provider credentials, so the missing-key check comes
    // from the Provider status rather than a frontend secret ref.
    renderPage({
      status: { ...status, providers: { ppio: { ...status.providers.ppio, has_key: false } } },
    });
    fireEvent.click(screen.getByRole("button", { name: /开始安装/ }));
    expect(await screen.findByRole("heading", { name: "provider step" })).toBeTruthy();
    expect(dispatch).not.toHaveBeenCalledWith({ type: "REQUEST_ACTIVATION" });
  });

  it("keeps the API key off the review page", () => {
    renderPage();
    expect(document.body.innerHTML).not.toMatch(/sk-|api_key/);
  });
});
