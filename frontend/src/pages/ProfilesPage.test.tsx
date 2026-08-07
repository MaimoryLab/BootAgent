import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { ProfileSummary, StatusResponse } from "../types/api";
import { ProfilesPage } from "./ProfilesPage";

const refreshStatus = vi.fn<() => Promise<void>>();
const dispatch = vi.fn();

// Hoisted so the module factory below can close over it: vi.mock is lifted above
// the imports, so a plain const declared here would not exist yet when it runs.
const { question } = vi.hoisted(() => ({ question: vi.fn<(options: { Message: string }) => Promise<string>>() }));
vi.mock("@wailsio/runtime", () => ({ Dialogs: { Question: question } }));

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch, refreshStatus }),
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
        latestVersion: null,
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
    desktopAgents: [],
    profiles,
    activeProfile: null,
    firstRun: false,
  };
}

function profile(over: Partial<ProfileSummary> = {}): ProfileSummary {
  return {
    id: "team-ppio",
    label: "团队 PPIO",
    provider: "ppio",
    model: "deepseek/deepseek-v3",
    baseUrl: null,
    protocol: "responses",
    activatedAt: null,
    ...over,
  };
}

function renderPage(profiles: ProfileSummary[]) {
  mockState = { status: statusWith(profiles), statusState: "success" };
  dispatch.mockClear();
  render(
    <MemoryRouter initialEntries={["/profiles"]}>
      <Routes>
        <Route path="/profiles" element={<ProfilesPage />} />
        <Route path="/setup/agents" element={<h1>onboarding</h1>} />
        <Route path="/overview" element={<h1>overview</h1>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("ProfilesPage", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    refreshStatus.mockReset();
    refreshStatus.mockResolvedValue();
    question.mockReset();
  });

  it("lists public Profile details and reports the Provider's key state", () => {
    mockState = { status: statusWith([profile()]), statusState: "success" };
    if (!mockState.status) throw new Error("missing status");
    mockState.status.providers.ppio.has_key = true;
    render(
      <MemoryRouter>
        <ProfilesPage />
      </MemoryRouter>,
    );
    expect(screen.getByText("团队 PPIO")).toBeTruthy();
    expect(screen.getByText(/deepseek\/deepseek-v3/)).toBeTruthy();
    expect(screen.getByText(/Provider 已有 Key/)).toBeTruthy();
    expect(document.body.innerHTML).not.toMatch(/sk-[A-Za-z0-9]/);
  });

  it("renders the Profile API mode", () => {
    renderPage([profile()]);
    expect(screen.getByText("团队 PPIO")).toBeTruthy();
    expect(screen.getByText("API mode: responses")).toBeTruthy();
  });

  it("shows which Agents use each Profile", () => {
    renderPage([profile(), profile({ id: "unused", label: "未使用" })]);
    expect(screen.getByTestId("profile-team-ppio").textContent).toContain("Codex");
    expect(screen.getByTestId("profile-unused").textContent).toContain("暂无 Agent 使用");
  });

  // These two replace a test that stubbed window.confirm and asserted the delete
  // went through. The page never called window.confirm, so the stub did nothing
  // and the test passed against a delete button with no confirmation at all --
  // exactly the bug it was named for. Asserting the cancel path is what makes the
  // confirmation load-bearing: without it, deleting unconditionally still passes.
  it("does not delete a Profile when the confirmation is declined", async () => {
    question.mockResolvedValue("取消");
    const remove = vi.spyOn(api, "deleteProfile").mockResolvedValue();
    renderPage([profile({ id: "unused", label: "未使用" })]);

    fireEvent.click(screen.getByRole("button", { name: "删除 未使用" }));
    await waitFor(() => expect(question).toHaveBeenCalled());
    expect(remove).not.toHaveBeenCalled();
    expect(refreshStatus).not.toHaveBeenCalled();
  });

  it("deletes a Profile once the confirmation is accepted", async () => {
    question.mockResolvedValue("删除");
    const remove = vi.spyOn(api, "deleteProfile").mockResolvedValue();
    renderPage([profile({ id: "unused", label: "未使用" })]);

    fireEvent.click(screen.getByRole("button", { name: "删除 未使用" }));
    await waitFor(() => expect(remove).toHaveBeenCalledWith("unused"));
    expect(refreshStatus).toHaveBeenCalled();
    // The prompt names the Profile: "delete this?" with no subject is how a user
    // confirms the wrong row.
    expect(question.mock.calls[0][0].Message).toContain("未使用");
  });

  it("blocks a second delete while the first is still running", async () => {
    question.mockResolvedValue("删除");
    let release = () => {};
    const remove = vi.spyOn(api, "deleteProfile").mockReturnValue(new Promise<void>((resolve) => { release = resolve; }));
    renderPage([profile({ id: "unused", label: "未使用" })]);

    const button = screen.getByRole("button", { name: "删除 未使用" });
    fireEvent.click(button);
    // Double-clicking sent two deletes, and the second one reported the Profile
    // it had just removed as unknown.
    await waitFor(() => expect(button).toBeDisabled());
    fireEvent.click(button);
    expect(remove).toHaveBeenCalledTimes(1);
    release();
  });

  it("explains why an in-use Profile cannot be deleted", () => {
    renderPage([profile()]);
    fireEvent.click(screen.getByRole("button", { name: "删除 团队 PPIO" }));
    expect(screen.getByText(/Profile 正在被.*使用，无法删除/)).toBeTruthy();
  });

  it("creates a Profile inline without entering onboarding", async () => {
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue(profile({
      id: "profile-ppio",
      label: "Codex Profile",
    }));
    renderPage([]);
    expect(screen.getByText(/还没有 Profile/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "新增 Profile" }));

    expect(screen.queryByRole("heading", { name: "onboarding" })).toBeNull();
    expect(screen.getByLabelText("Profile ID")).toHaveValue("profile-ppio");
    expect(screen.getByRole("combobox", { name: "API 类型" })).toHaveTextContent("请选择 API 类型");
    expect(dispatch).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "model-a" } });
    fireEvent.click(screen.getByRole("combobox", { name: "API 类型" }));
    fireEvent.click(screen.getByRole("option", { name: "OpenAI Responses" }));
    fireEvent.click(screen.getByRole("button", { name: "保存 Profile" }));

    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({
      id: "profile-ppio",
      provider: "ppio",
      model: "model-a",
      protocol: "responses",
      apiKey: "",
    })));
  });

  it("chooses the next available Profile ID", () => {
    renderPage([profile({ id: "codex-ppio" })]);
    fireEvent.click(screen.getByRole("button", { name: "新增 Profile" }));

    expect(screen.getByLabelText("Profile ID")).toHaveValue("profile-ppio");
  });

  it("requires a manually selected API type", async () => {
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue(profile({ id: "profile-ppio" }));
    renderPage([]);
    fireEvent.click(screen.getByRole("button", { name: "新增 Profile" }));
    expect(screen.getByRole("button", { name: "保存 Profile" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "model-a" } });
    expect(save).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("combobox", { name: "API 类型" }));
    fireEvent.click(screen.getByRole("option", { name: "OpenAI Chat Completions" }));
    expect(screen.getByRole("button", { name: "保存 Profile" })).not.toBeDisabled();
  });

  it("points at the Provider page when its key is missing", async () => {
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue(profile({ label: "团队默认" }));
    renderPage([profile()]);
    fireEvent.click(screen.getByRole("button", { name: "编辑 团队 PPIO" }));
    expect(screen.getByLabelText("Profile ID").hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/这个 Provider 还没有 Key/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("名称"), { target: { value: "团队默认" } });
    fireEvent.click(screen.getByRole("button", { name: "保存 Profile" }));

    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({
      id: "team-ppio",
      label: "团队默认",
      apiKey: "",
    })));
  });

  it("saves a Profile whose name was left blank", async () => {
    // The backend fills an empty label in from the existing value or the ID
    // (internal/profile/write.go:71-74), so requiring one here was stricter than
    // the write path and blocked a rename-to-nothing edit.
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue(profile());
    renderPage([profile()]);
    fireEvent.click(screen.getByRole("button", { name: "编辑 团队 PPIO" }));
    const label = screen.getByLabelText("名称");
    expect(label.hasAttribute("required")).toBe(false);
    fireEvent.change(label, { target: { value: "" } });

    const button = screen.getByRole("button", { name: "保存 Profile" });
    expect(button.hasAttribute("disabled")).toBe(false);
    fireEvent.click(button);
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ id: "team-ppio", label: "" })));
  });

  it("still refuses to save without a model", async () => {
    // model has no backend fallback (write.go:50-53), so this one stays required.
    const save = vi.spyOn(api, "saveProfile");
    renderPage([profile()]);
    fireEvent.click(screen.getByRole("button", { name: "编辑 团队 PPIO" }));
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "" } });

    expect(screen.getByRole("button", { name: "保存 Profile" }).hasAttribute("disabled")).toBe(true);
    expect(save).not.toHaveBeenCalled();
  });

  it("pre-fills the model from the Provider and leaves it editable", () => {
    // The whole point of the field being pre-filled is that a first-time user
    // never has to invent a model ID, so this asserts the value is present
    // before any typing -- and that typing still replaces it.
    mockState = {
      status: {
        ...statusWith([]),
        providers: { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai", default_model: "ppio/default-model" } },
      },
      statusState: "success",
    };
    dispatch.mockClear();
    render(
      <MemoryRouter initialEntries={["/profiles"]}>
        <Routes>
          <Route path="/profiles" element={<ProfilesPage />} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "新增 Profile" }));
    expect(screen.getByLabelText("模型")).toHaveValue("ppio/default-model");
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "mine" } });
    expect(screen.getByLabelText("模型")).toHaveValue("mine");
  });

  it("leaves the model empty for a Provider with no default", () => {
    // A user-added Provider is an endpoint we know nothing about; guessing a
    // model for it would write a config that fails on the first request.
    renderPage([]);
    fireEvent.click(screen.getByRole("button", { name: "新增 Profile" }));
    expect(screen.getByLabelText("模型")).toHaveValue("");
  });

  it("discovers and searches the selected Provider's models", async () => {
    const models = vi.spyOn(api, "models").mockResolvedValue({
      ok: true,
      reachable: true,
      status: 200,
      message: "Found 2 models.",
      error_code: null,
      retryable: false,
      models: ["model-alpha", "model-beta"],
    });
    mockState = { status: statusWith([]), statusState: "success" };
    if (!mockState.status) throw new Error("missing status");
    mockState.status.providers.ppio.has_key = true;
    render(<MemoryRouter><ProfilesPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "新增 Profile" }));
    fireEvent.click(screen.getByRole("combobox", { name: "API 类型" }));
    fireEvent.click(screen.getByRole("option", { name: "OpenAI Responses" }));
    await waitFor(() => expect(models).toHaveBeenCalledWith({ provider: "ppio", apiBaseUrl: "", apiKey: "" }));
    expect(screen.getByText("model-alpha")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "beta" } });
    expect(screen.queryByText("model-alpha")).toBeNull();
    expect(screen.getByText("model-beta")).toBeTruthy();
  });

  it("applies one Profile to all of its Agents", async () => {
    const activate = vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true,
      agent: "codex",
      config: "/c",
      provider: "ppio",
      model: "deepseek/deepseek-v3",
      restart: "restart",
      next: "next",
    });
    mockState = { status: statusWith([profile()]), statusState: "success" };
    if (!mockState.status) throw new Error("missing status");
    mockState.status.providers.ppio.has_key = true;
    render(
      <MemoryRouter initialEntries={["/profiles"]}>
        <Routes><Route path="/profiles" element={<ProfilesPage />} /><Route path="/overview" element={<h1>overview</h1>} /></Routes>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "应用到 Agent" }));

    await waitFor(() => expect(activate).toHaveBeenCalledWith("codex", expect.objectContaining({
      provider: "ppio",
      model: "deepseek/deepseek-v3",
      profileId: "team-ppio",
    })));
    expect(await screen.findByRole("heading", { name: "overview" })).toBeTruthy();
  });
});
