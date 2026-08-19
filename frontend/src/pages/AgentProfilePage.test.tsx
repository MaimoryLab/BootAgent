import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { ProfileSummary, SaveProfileResult, StatusResponse } from "../types/api";
import { AgentProfilePage } from "./AgentProfilePage";

const refreshStatus = vi.fn<() => Promise<void>>();
const dispatch = vi.fn();

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch, refreshStatus }),
}));

let mockState: { status: StatusResponse | null; statusState: string };

function profile(over: Partial<ProfileSummary> = {}): ProfileSummary {
  return {
    id: "team-ppio",
    label: "团队 PPIO",
    provider: "ppio",
    model: "deepseek/deepseek-v3",
    baseUrl: null,
    context1M: false,
    protocol: "responses",
    activatedAt: null,
    ...over,
  };
}

function saved(over: Partial<ProfileSummary> = {}): SaveProfileResult {
  return { profile: profile(over), reapplied: null, failures: null };
}

function statusWith(profiles: ProfileSummary[]): StatusResponse {
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "bash" },
    runtimes: [],
    capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
    agents: {
      codex: {
        installed: true, configured: true, guideOnly: false, config: "/c",
        version: "1.0.0", lockedVersion: "1.0.0", latestVersion: null, canInstall: true,
        provider: "ppio", profileId: "team-ppio", model: "deepseek/deepseek-v3",
        baseUrl: null, updatedAt: null, detected: null,
      },
    },
    catalog: [{
      id: "codex", name: "Codex", group: "auto", configMode: "auto", selectsModel: true, webApp: false,
      guideOnly: false, lockedVersion: "1.0.0", protocol: "responses",
      platforms: ["macos"], platformNote: "", rank: 1,
    }],
    groups: [],
    providers: { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai", has_key: true } },
    mirrors: [], paths: {}, backups: {},
    environment: null, environmentError: null, desktopAgents: [],
    profiles, activeProfile: null, firstRun: false,
  };
}

function renderPage(profiles: ProfileSummary[]) {
  mockState = { status: statusWith(profiles), statusState: "success" };
  render(
    <MemoryRouter initialEntries={["/agents/codex"]}>
      <Routes>
        <Route path="/agents/:agentId" element={<AgentProfilePage />} />
        <Route path="/overview" element={<h1>overview</h1>} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  refreshStatus.mockReset();
  refreshStatus.mockResolvedValue(undefined);
  vi.spyOn(api, "models").mockResolvedValue({
    ok: true, reachable: true, status: 200, error_code: "", retryable: false,
    models: ["deepseek/deepseek-v3", "deepseek/deepseek-r1"], message: "",
  });
});

afterEach(() => vi.restoreAllMocks());

describe("AgentProfilePage", () => {
  // Requirement: after applying, land where the result is visible instead of
  // staying on a screen whose job is done.
  it("returns to the overview after a successful apply", async () => {
    const activate = vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true, agent: "codex", config: "/c", provider: "ppio",
      model: "deepseek/deepseek-v3", restart: "restart", next: "next",
    });
    renderPage([profile()]);
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    await waitFor(() => expect(activate).toHaveBeenCalled());
    expect(await screen.findByRole("heading", { name: "overview" })).toBeTruthy();
  });

  it("stays put and reports the failure when applying fails", async () => {
    const { BootAgentApiError } = await import("../backend/errors");
    vi.spyOn(api, "activateAgent").mockRejectedValue(
      new BootAgentApiError("Endpoint refused the key", "PROVIDER_REJECTED", false, 400),
    );
    renderPage([profile()]);
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    expect(await screen.findByText("Endpoint refused the key")).toBeTruthy();
    // Not navigated away: the user needs to see the error next to the choice.
    expect(screen.queryByRole("heading", { name: "overview" })).toBeNull();
  });

  // Requirement: change the model without going through the edit button.
  it("edits the selected Profile's model in place and saves on blur", async () => {
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue(saved({ model: "deepseek/deepseek-r1" }));
    renderPage([profile()]);
    // ModelPicker replaces the plain field with a search field once the model
    // list arrives, so a reference taken before that is detached by the swap.
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    expect(field).toHaveValue("deepseek/deepseek-v3");

    await userEvent.clear(field);
    await userEvent.type(field, "deepseek/deepseek-r1");
    // Nothing written while typing: one save per keystroke would write a Profile
    // per letter.
    expect(save).not.toHaveBeenCalled();

    await userEvent.tab();
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({
      id: "team-ppio", model: "deepseek/deepseek-r1",
    })));
    // The rest of the Profile is passed through, so an inline model edit cannot
    // quietly reset a field only the editor shows.
    expect(save).toHaveBeenCalledWith(expect.objectContaining({
      label: "团队 PPIO", provider: "ppio", protocol: "responses",
    }));
    await waitFor(() => expect(refreshStatus).toHaveBeenCalled());
  });

  it("does not write when the model is unchanged or blank", async () => {
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue(saved());
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");

    await userEvent.click(field);
    await userEvent.tab();
    expect(save).not.toHaveBeenCalled();

    await userEvent.clear(field);
    await userEvent.tab();
    // A blank model would make the Profile unusable, so it is ignored rather
    // than saved.
    expect(save).not.toHaveBeenCalled();
  });

  // The inline field belongs to the selected card only; a picker on every card
  // would be a wall of inputs.
  it("shows the inline model field only on the selected Profile", async () => {
    renderPage([profile(), profile({ id: "solo", label: "个人", model: "gpt-5" })]);
    await waitFor(() => expect(screen.getAllByRole("radio")).toHaveLength(2));
    expect(screen.getAllByLabelText("模型")).toHaveLength(1);

    // Switching selection moves the field, which is also the Profile switch this
    // page has always offered.
    await userEvent.click(screen.getByRole("radio", { name: "选择 个人" }));
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveValue("gpt-5"));
    expect(screen.getAllByLabelText("模型")).toHaveLength(1);
  });

  it("keeps the edit button for everything the inline field does not cover", async () => {
    renderPage([profile()]);
    // Reasoning depth and the 1M context switch still live behind it.
    fireEvent.click(screen.getByRole("button", { name: /编辑/ }));
    expect(await screen.findByLabelText("思考深度")).toBeTruthy();
  });
});
