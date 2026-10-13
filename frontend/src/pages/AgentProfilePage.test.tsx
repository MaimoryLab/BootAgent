import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { FeedbackViewport } from "../components/FeedbackViewport";
import { FeedbackProvider } from "../state/FeedbackContext";
import type { ProfileSummary, SaveProfileResult, StatusResponse } from "../types/api";
import { AgentProfilePage } from "./AgentProfilePage";

const refreshStatus = vi.fn<() => Promise<void>>();
const dispatch = vi.fn();

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch, refreshStatus }),
}));

vi.mock("../state/confirmDelete", () => ({
  confirmAction: vi.fn(async () => true),
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
  return render(
    <FeedbackProvider>
      <MemoryRouter initialEntries={["/agents/codex"]}>
        <Routes>
          <Route path="/agents/:agentId" element={<AgentProfilePage />} />
          <Route path="/overview" element={<h1>overview</h1>} />
        </Routes>
      </MemoryRouter>
    </FeedbackProvider>,
  );
}

/** The receipt on screen, with 详情 open. */
async function failureReceipt() {
  const receipt = await screen.findByRole("alert");
  fireEvent.click(within(receipt).getByRole("button", { name: "详情" }));
  return receipt;
}

function renderClaudeDesktopPage(profiles: ProfileSummary[]) {
  const status = statusWith(profiles);
  status.desktopAgents = [{
    id: "claude-desktop", name: "Claude Desktop", installed: true, supported: true,
    version: "1.0.0", source: "application", protocol: "anthropic", profileAgentId: "claude-desktop", profileId: null,
  }];
  status.providers = {
    "chat-only": { name: "Chat only", home: "", base_url: "https://chat.example/v1", has_key: true },
  };
  mockState = { status, statusState: "success" };
  render(
    <MemoryRouter initialEntries={["/agents/claude-desktop"]}>
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
  // Only 检测模型 sends a check; a test that expects none asserts on this spy.
  vi.spyOn(api, "checkProfileModel").mockResolvedValue(availableCheck);
});

afterEach(() => vi.restoreAllMocks());

const refusedCheck = {
  profileId: "team-ppio", model: "claude-opus-5-5",
  status: "unavailable" as const, reason: "refused" as const, protocol: "responses", httpStatus: 404,
  detail: "model does not exist", agents: ["codex"],
};
const availableCheck = {
  profileId: "team-ppio", model: "deepseek/deepseek-v3", status: "available" as const, protocol: "responses", agents: ["codex"],
};

/** The page inside the app's feedback layer, with the overview showing receipts. */
function renderWithReceipts(path: string, status: StatusResponse) {
  mockState = { status, statusState: "success" };
  render(
    <FeedbackProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/agents/:agentId" element={<AgentProfilePage />} />
          <Route path="/overview" element={<><h1>overview</h1><FeedbackViewport /></>} />
        </Routes>
      </MemoryRouter>
    </FeedbackProvider>,
  );
}

describe("AgentProfilePage", () => {
  // The overview row changing was the only sign the apply worked; the receipt
  // outlives this page and says it in words.
  it("names the applied Profile on the overview it returns to", async () => {
    vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true, agent: "codex", config: "/c", provider: "ppio",
      model: "deepseek/deepseek-v3", restart: "", next: "",
    });
    renderWithReceipts("/agents/codex", statusWith([profile()]));
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    await screen.findByRole("heading", { name: "overview" });
    expect(screen.getByText("Codex 已改用「团队 PPIO」")).toBeTruthy();
  });

  // The backend reports that a desktop app reads its settings at startup; that
  // used to be dropped, so the running app looked switched when it was not.
  it("keeps a restart reminder after applying to a desktop app", async () => {
    const status = statusWith([profile({ id: "desk", label: "桌面", protocol: "anthropic", model: "claude-sonnet-5-5" })]);
    status.desktopAgents = [{
      id: "claude-desktop", name: "Claude Desktop", installed: true, supported: true,
      version: "1.0.0", source: "application", protocol: "anthropic", profileAgentId: "claude-desktop", profileId: null,
    }];
    status.providers = { ppio: { name: "PPIO", home: "", base_url: "https://api.ppio.com/openai", anthropic_base_url: "https://api.ppio.com/anthropic", has_key: true } };
    const configure = vi.spyOn(api, "configureDesktopAgent").mockResolvedValue({
      agent: "claude-desktop", profileId: "desk", profileAgentId: "claude-desktop",
      message: "Claude Desktop model applied", restart: "Restart Claude Desktop",
    });
    renderWithReceipts("/agents/claude-desktop", status);
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    await waitFor(() => expect(configure).toHaveBeenCalledWith("claude-desktop", "desk"));
    await screen.findByRole("heading", { name: "overview" });
    // A warning, so it waits to be closed rather than retiring itself.
    expect(screen.getByRole("alert")).toHaveTextContent("已应用，重启 Claude Desktop 后生效");
  });

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
    const receipt = await failureReceipt();
    expect(receipt).toHaveTextContent("应用配置模版失败");
    expect(receipt).toHaveTextContent("Endpoint refused the key");
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

  // The editor is a page of its own; the Profile list returns when it closes.
  it("shows only the editor while a Profile is being edited", async () => {
    renderPage([profile()]);
    expect(screen.getByTestId("agent-profile-team-ppio")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "编辑 团队 PPIO" }));
    expect(await screen.findByText("编辑配置模版")).toBeTruthy();
    expect(screen.queryByTestId("agent-profile-team-ppio")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.getByTestId("agent-profile-team-ppio")).toBeTruthy();
  });

  // A check is a billed request, so it is the user's to send: an inline model
  // edit and an Apply send none.
  it("does not check the model on an inline edit or on apply", async () => {
    vi.spyOn(api, "saveProfile").mockResolvedValue({ ...saved({ model: "deepseek/deepseek-r1" }), reapplied: ["codex"] });
    vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true, agent: "codex", config: "/c", provider: "ppio",
      model: "deepseek/deepseek-r1", restart: "restart", next: "next",
    });
    const check = vi.spyOn(api, "checkProfileModel");
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    await userEvent.clear(field);
    await userEvent.type(field, "deepseek/deepseek-r1");
    await userEvent.tab();
    fireEvent.click(screen.getByRole("button", { name: "应用" }));

    expect(await screen.findByRole("heading", { name: "overview" })).toBeTruthy();
    expect(check).not.toHaveBeenCalled();
  });

  // The reported path: picking a model inline saved it and rewrote every Agent
  // on the Profile. 检测模型 then names the refusal, with the model to go back
  // to, checked for this Agent only.
  it("checks on request after an inline edit and switches back", async () => {
    const save = vi.spyOn(api, "saveProfile")
      .mockImplementationOnce(async () => {
        mockState.status!.profiles = [profile({ model: "claude-opus-5-5" })];
        return { ...saved({ model: "claude-opus-5-5" }), reapplied: ["codex"] };
      })
      .mockResolvedValueOnce({ ...saved(), reapplied: ["codex"] });
    const check = vi.spyOn(api, "checkProfileModel").mockResolvedValue(refusedCheck);
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    await userEvent.clear(field);
    await userEvent.type(field, "claude-opus-5-5");
    await userEvent.tab();
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("alert")).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "检测模型" }));
    const notice = await screen.findByRole("alert");
    expect(check).toHaveBeenCalledWith("team-ppio", ["codex"]);
    expect(notice.textContent).toContain("模型「claude-opus-5-5」不可用");
    expect(notice.textContent).toContain("Codex");
    await userEvent.click(screen.getByRole("button", { name: "改回 deepseek/deepseek-v3" }));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
    expect(save.mock.calls[1][0]).toMatchObject({ id: "team-ppio", model: "deepseek/deepseek-v3" });
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(check).toHaveBeenCalledTimes(1);
  });

  // Pressing 检测模型 straight after typing checks what is on screen: the typed
  // model is saved first, once, and the check offers the model it replaced.
  it("saves the typed model once before checking it", async () => {
    const save = vi.spyOn(api, "saveProfile").mockResolvedValue({ ...saved({ model: "claude-opus-5-5" }), reapplied: ["codex"] });
    const check = vi.spyOn(api, "checkProfileModel").mockResolvedValue(refusedCheck);
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    await userEvent.clear(field);
    await userEvent.type(field, "claude-opus-5-5");
    await userEvent.click(screen.getByRole("button", { name: "检测模型" }));

    await screen.findByRole("alert");
    expect(save).toHaveBeenCalledTimes(1);
    expect(save.mock.calls[0][0]).toMatchObject({ model: "claude-opus-5-5" });
    expect(check).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "改回 deepseek/deepseek-v3" })).toBeTruthy();
  });

  // A check the user asked for always ends with an answer, a pass included.
  it("reports a model that answered", async () => {
    renderPage([profile()]);
    await userEvent.click(await screen.findByRole("button", { name: "检测模型" }));
    expect(await screen.findByText("模型「deepseek/deepseek-v3」可用")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("button", { name: /改回/ })).toBeNull();
  });

  // Both inline save paths dropped result.failures, so an Agent that could not
  // be rewritten went unreported here.
  // The model belongs to the Profile, so an edit made for this Agent also
  // moves every other Agent on it. That used to happen with nothing said.
  it("names the other Agents an inline model edit moved", async () => {
    vi.spyOn(api, "saveProfile").mockResolvedValue({ ...saved({ model: "deepseek/deepseek-r1" }), reapplied: ["codex", "opencode"] });
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    await userEvent.clear(field);
    await userEvent.type(field, "deepseek/deepseek-r1");
    await userEvent.tab();
    const receipt = (await screen.findByText("已保存，2 个 Agent 已更新")).closest(".feedback-toast") as HTMLElement;
    fireEvent.click(within(receipt).getByRole("button", { name: "详情" }));
    expect(receipt).toHaveTextContent("Codex、opencode");
  });

  it("reports an Agent an inline model edit could not rewrite", async () => {
    vi.spyOn(api, "saveProfile").mockResolvedValue({ ...saved({ model: "deepseek/deepseek-r1" }), failures: { codex: "写入失败" } });
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    await userEvent.clear(field);
    await userEvent.type(field, "deepseek/deepseek-r1");
    await userEvent.tab();
    const receipt = await failureReceipt();
    expect(receipt).toHaveTextContent("已保存，1 个 Agent 未能更新");
    expect(receipt).toHaveTextContent("Codex: 写入失败");
  });

  // Apply right after typing: the flush saves the new model, and the Apply has
  // to activate with that one. It used to read the model from before the click
  // and write the old model back over the Agent the flush had just moved.
  it("applies the model the flush saved, not the one from before the click", async () => {
    vi.spyOn(api, "saveProfile").mockResolvedValue({ ...saved({ model: "deepseek/deepseek-r1" }), reapplied: ["codex"] });
    const activate = vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true, agent: "codex", config: "/c", provider: "ppio",
      model: "deepseek/deepseek-r1", restart: "restart", next: "next",
    });
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    await userEvent.clear(field);
    await userEvent.type(field, "deepseek/deepseek-r1");
    fireEvent.click(screen.getByRole("button", { name: "应用" }));

    await waitFor(() => expect(activate).toHaveBeenCalled());
    expect(activate.mock.calls[0][1]).toMatchObject({ model: "deepseek/deepseek-r1", profileId: "team-ppio" });
  });

  // A flush whose save failed leaves the Profile on the old model. Applying
  // anyway would apply that while the field showed the new one.
  it("does not apply when flushing the typed model fails", async () => {
    const { BootAgentApiError } = await import("../backend/errors");
    vi.spyOn(api, "saveProfile").mockRejectedValue(new BootAgentApiError("disk full", "CONFIG_WRITE_FAILED", false, 500));
    const activate = vi.spyOn(api, "activateAgent");
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    await userEvent.clear(field);
    await userEvent.type(field, "deepseek/deepseek-r1");
    fireEvent.click(screen.getByRole("button", { name: "应用" }));

    // CONFIG_WRITE_FAILED is shown as its localized copy, not the raw message.
    const receipt = await failureReceipt();
    expect(receipt).toHaveTextContent("无法保存配置模版");
    expect(receipt).toHaveTextContent("无法写入配置文件");
    expect(receipt).not.toHaveTextContent("disk full");
    expect(activate).not.toHaveBeenCalled();
    expect(screen.queryByRole("heading", { name: "overview" })).toBeNull();
  });

  // The review's race: leaving the field starts a save. Clicking 应用 in that
  // window found no draft left to flush and applied the stored model -- the
  // old one, since the save had not finished. 应用 waits for it instead.
  it("does not apply while a model the user just left is still saving", async () => {
    let finishSave: (value: SaveProfileResult) => void = () => undefined;
    vi.spyOn(api, "saveProfile").mockImplementation(() => new Promise((resolve) => { finishSave = resolve; }) as never);
    const activate = vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true, agent: "codex", config: "/c", provider: "ppio",
      model: "deepseek/deepseek-r1", restart: "restart", next: "next",
    });
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    const field = screen.getByLabelText("模型");
    await userEvent.clear(field);
    await userEvent.type(field, "deepseek/deepseek-r1");
    await userEvent.tab();

    const button = screen.getByRole("button", { name: "应用" });
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(activate).not.toHaveBeenCalled();

    finishSave({ ...saved({ model: "deepseek/deepseek-r1" }), reapplied: ["codex"] });
    await waitFor(() => expect(button).not.toBeDisabled());
  });

  it.each(["result", "error"])("discards a late model check %s after another Profile save", async (outcome) => {
    let finish: (value: unknown) => void = () => undefined;
    let reject: (error: Error) => void = () => undefined;
    vi.spyOn(api, "checkProfileModel").mockImplementation(() => new Promise((resolve, fail) => { finish = resolve; reject = fail; }) as never);
    vi.spyOn(api, "saveProfile").mockImplementation((request) => {
      mockState.status!.profiles = [profile({ model: request.model })];
      return Promise.resolve(saved({ model: request.model })) as never;
    });
    renderPage([profile({ model: "model-B" })]);
    fireEvent.click(screen.getByRole("button", { name: "检测模型" }));
    fireEvent.click(screen.getByRole("button", { name: "编辑 团队 PPIO" }));
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "model-C" } });
    fireEvent.click(screen.getByRole("button", { name: "保存配置模版" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "保存配置模版" })).toBeNull());
    await act(async () => {
      if (outcome === "result") finish({ profileId: "team-ppio", model: "model-B", status: "unavailable", reason: "refused", agents: ["codex"] });
      else reject(new Error("late check failure"));
    });
    expect(screen.queryByText("模型「model-B」不可用")).toBeNull();
    expect(screen.queryByText("late check failure")).toBeNull();
    expect(screen.getByRole("button", { name: "检测模型" })).not.toBeDisabled();
  });

  it("does not start a model check after leaving while its inline save is pending", async () => {
    let finish: (value: SaveProfileResult) => void = () => undefined;
    vi.spyOn(api, "saveProfile").mockImplementation(() => new Promise((resolve) => { finish = resolve; }) as never);
    const check = vi.spyOn(api, "checkProfileModel");
    const page = renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "model-B" } });
    fireEvent.click(screen.getByRole("button", { name: "检测模型" }));
    page.unmount();
    await act(async () => finish(saved({ model: "model-B" })));
    expect(check).not.toHaveBeenCalled();
  });

  it("does not revert a Profile changed outside this editor after the check", async () => {
    const save = vi.spyOn(api, "saveProfile").mockImplementation((request) => {
      mockState.status!.profiles = [profile({ model: request.model })];
      return Promise.resolve(saved({ model: request.model })) as never;
    });
    vi.spyOn(api, "checkProfileModel").mockResolvedValue(refusedCheck);
    renderPage([profile()]);
    await waitFor(() => expect(screen.getByLabelText("模型")).toHaveAttribute("placeholder", "搜索模型"));
    fireEvent.change(screen.getByLabelText("模型"), { target: { value: "claude-opus-5-5" } });
    fireEvent.click(screen.getByRole("button", { name: "检测模型" }));
    await screen.findByRole("alert");
    mockState.status!.profiles = [profile({ model: "model-C" })];
    fireEvent.click(screen.getByRole("button", { name: "改回 deepseek/deepseek-v3" }));
    expect(save).toHaveBeenCalledOnce();
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

  // An Agent in compatibility mode is shown on the Profile its requests go
  // to; the adapter's own Profile is an implementation detail.
  it("preselects the model service a compatibility-mode Agent really uses", () => {
    const status = statusWith([
      profile({ id: "bootagent-converter-for-codex", label: "BootAgent codex", provider: "bootagent-converter-for-codex", model: "gpt-5.6-sol" }),
      profile({ id: "team-ppio", label: "团队 PPIO", protocol: "responses" }),
      profile({ id: "chat-only", label: "Chat only", protocol: "openai" }),
    ]);
    status.agents.codex = { ...status.agents.codex, profileId: "bootagent-converter-for-codex" };
    status.conversionRoutes = { codex: "chat-only" };
    mockState = { status, statusState: "success" };
    render(
      <MemoryRouter initialEntries={["/agents/codex"]}>
        <Routes><Route path="/agents/:agentId" element={<AgentProfilePage />} /></Routes>
      </MemoryRouter>,
    );

    expect(screen.queryByText("BootAgent codex")).toBeNull();
    expect(screen.getByRole("radio", { name: "选择 Chat only" })).toBeChecked();
    expect(screen.getByText("Codex 正在通过兼容模式使用这个配置模版，使用期间需要保持 BootAgent 运行")).toBeTruthy();
  });

  it("offers one-confirmation protocol adaptation for a Chat Completions Profile", async () => {
    vi.spyOn(api, "assessDesktopAgentProfile").mockResolvedValue({
      agent: "claude-desktop", profileId: "chat-profile", compatibility: "convertible",
      message: "convertible", upstreamVerified: true,
    });
    const configure = vi.spyOn(api, "configureDesktopAgentWithConversion").mockResolvedValue({
      agent: "claude-desktop", profileId: "bootagent-converter-anthropic", profileAgentId: "claude-desktop",
      message: "configured", compatibility: "convertible", conversionRunning: true,
      localAuthVerified: true, upstreamVerified: true, agentConfigured: true, endToEndVerified: true,
    });
    renderClaudeDesktopPage([profile({
      id: "chat-profile", label: "Chat Profile", provider: "chat-only", model: "model-a", protocol: "openai",
    })]);

    expect(await screen.findByText("Claude Desktop 不能直接使用这个模型服务，应用时会开启兼容模式在本机转换，使用期间需要保持 BootAgent 运行")).toBeTruthy();
    const apply = screen.getByRole("button", { name: "用兼容模式应用" });
    await userEvent.click(apply);
    await waitFor(() => expect(configure).toHaveBeenCalledWith("claude-desktop", "chat-profile"));
    expect(await screen.findByRole("heading", { name: "overview" })).toBeTruthy();
  });

  // A command-line Agent used to have no route to the adapter at all: a Chat
  // Completions Profile was never listed for Codex, and pointing Codex at the
  // adapter meant picking a hidden converter Profile by hand.
  it("lists a Chat Completions Profile for Codex and applies it through compatibility mode", async () => {
    const assess = vi.spyOn(api, "assessDesktopAgentProfile");
    const activate = vi.spyOn(api, "activateAgent");
    const convert = vi.spyOn(api, "activateAgentWithConversion").mockResolvedValue({
      ok: true, agent: "codex", config: "/c", provider: "bootagent-converter-responses", model: "gpt-5.6-sol",
      restart: "", next: "", upstreamVerified: true, endToEndVerified: true,
    });
    renderPage([profile({ id: "chat-only", label: "Chat only", protocol: "openai" })]);

    await userEvent.click(screen.getByRole("radio", { name: "选择 Chat only" }));
    expect(screen.getByText("Codex 不能直接使用这个配置模版，应用时会开启兼容模式在本机转换，使用期间需要保持 BootAgent 运行")).toBeTruthy();
    // Nothing billed is sent on selection: the check belongs to the button.
    expect(assess).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "用兼容模式应用" }));
    await waitFor(() => expect(convert).toHaveBeenCalledWith("codex", "chat-only"));
    expect(activate).not.toHaveBeenCalled();
    expect(await screen.findByRole("heading", { name: "overview" })).toBeTruthy();
  });

  it("does not offer Chat Completions Profiles to an Agent that already speaks it", () => {
    const status = statusWith([
      profile({ id: "chat-only", label: "Chat only", protocol: "openai" }),
      profile({ id: "responses-only", label: "Responses only", protocol: "responses" }),
    ]);
    status.catalog = [{ ...status.catalog[0], id: "opencode", name: "OpenCode", protocol: "openai" }];
    status.agents = { opencode: { ...status.agents.codex, profileId: null } };
    mockState = { status, statusState: "success" };
    render(
      <MemoryRouter initialEntries={["/agents/opencode"]}>
        <Routes><Route path="/agents/:agentId" element={<AgentProfilePage />} /></Routes>
      </MemoryRouter>,
    );

    expect(screen.getByRole("radio", { name: "选择 Chat only" })).toBeTruthy();
    expect(screen.queryByRole("radio", { name: "选择 Responses only" })).toBeNull();
    expect(screen.queryByRole("button", { name: "用兼容模式应用" })).toBeNull();
  });

  it("turns a refused check into compatibility mode on the same Provider and model", async () => {
    vi.mocked(api.checkProfileModel).mockResolvedValue(refusedCheck);
    const saveProfile = vi.spyOn(api, "saveProfile").mockResolvedValue(saved({ id: "team-ppio-compat", protocol: "openai" }));
    const convert = vi.spyOn(api, "activateAgentWithConversion").mockResolvedValue({
      ok: true, agent: "codex", config: "/c", provider: "bootagent-converter-responses", model: "gpt-5.6-sol",
      restart: "", next: "", upstreamVerified: true, endToEndVerified: true,
    });
    renderPage([profile()]);

    await userEvent.click(screen.getByRole("button", { name: /检测模型/ }));
    await userEvent.click(await screen.findByRole("button", { name: "使用兼容模式" }));
    await waitFor(() => expect(convert).toHaveBeenCalledWith("codex", "team-ppio-compat"));
    expect(saveProfile).toHaveBeenCalledWith(expect.objectContaining({
      id: "team-ppio-compat", provider: "ppio", model: "deepseek/deepseek-v3", protocol: "openai",
    }));
    expect(await screen.findByRole("heading", { name: "overview" })).toBeTruthy();
  });
});
