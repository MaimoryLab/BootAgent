import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import { FeedbackProvider } from "../state/FeedbackContext";
import { initialWizardState, wizardReducer, type WizardAction, type WizardState } from "../state/wizardReducer";
import type { StatusResponse } from "../types/api";
import { ProviderKeyPage } from "./ProviderKeyPage";

let state: WizardState;
const keyRef = { current: "test-key" };
const clearApiKey = vi.fn(() => {
  keyRef.current = "";
  dispatch({ type: "SET_HAS_API_KEY", value: false });
});
const setApiKey = vi.fn((value: string) => {
  keyRef.current = value;
  dispatch({ type: "SET_HAS_API_KEY", value: Boolean(value) });
});
const refreshStatus = vi.fn(async () => {});
const dispatch = vi.fn((action: WizardAction) => {
  state = wizardReducer(state, action);
});

const status = {
  providers: { ppio: { name: "PPIO", home: "", base_url: "https://api.ppio.com/openai", has_key: true } },
  catalog: [],
} as unknown as StatusResponse;

// Built-in Providers as the protocol table describes them: Chat Completions is
// confirmed on both, OpenAI Responses (what Codex speaks) only on the second.
const tabledStatus = {
  providers: {
    jiekou: {
      name: "JieKou.AI", home: "", base_url: "https://api.jiekou.test/openai", has_key: true,
      protocols: { openai: "implementation-supported", anthropic: "implementation-supported", responses: "route-present-unverified" },
    },
    deepseek: {
      name: "DeepSeek", home: "", base_url: "https://api.deepseek.test", has_key: true,
      protocols: { openai: "implementation-supported", anthropic: "implementation-supported", responses: "implementation-supported" },
    },
  },
  catalog: [{ id: "codex", name: "Codex", protocol: "responses", configMode: "auto", selectsModel: true }],
} as unknown as StatusResponse;

const codexOn = (provider: string, patch: Partial<WizardState> = {}): WizardState => ({
  ...initialWizardState, status: tabledStatus, statusState: "success", setupKind: "cli", selectedAgentIds: ["codex"], provider, ...patch,
});

const SUGGESTION = "尚未确认 JieKou.AI 支持 Codex 的请求格式（OpenAI Responses），可以改用兼容模式，由 BootAgent 在本机转换";

/** Pins the selected Provider to the one this fixture defines. Inheriting the
 *  wizard's initial Provider instead coupled these cases to whichever Provider
 *  ships first in providers.lock.json: when that changed, every case here looked
 *  up a Provider the fixture did not contain and rendered nothing. */
const baseState: WizardState = { ...initialWizardState, provider: "ppio" };

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({
    state,
    dispatch,
    secret: { keyRef, setApiKey, clearApiKey },
    refreshStatus,
  }),
}));

describe("ProviderKeyPage", () => {
  afterEach(() => vi.restoreAllMocks());

  // A Chat Completions-only service used to leave Codex users with a failure
  // that pointed at "another API type" the wizard has no control for, and a
  // Continue button that installed an Agent which then failed on first use.
  it("offers compatibility mode when Codex's format fails but Chat Completions works", async () => {
    const codexStatus = {
      ...status,
      catalog: [{ id: "codex", name: "Codex", protocol: "responses", configMode: "auto", selectsModel: true }],
    } as unknown as StatusResponse;
    state = { ...baseState, status: codexStatus, statusState: "success", setupKind: "cli", selectedAgentIds: ["codex"] };
    keyRef.current = "";
    vi.spyOn(api, "probe").mockResolvedValue({
      ok: false, reachable: true, status: 404, message: "Model does not support OpenAI Responses.",
      error_code: "PROTOCOL_UNSUPPORTED", retryable: false, convertible: ["responses"],
    });
    const page = render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));
    await waitFor(() => expect(state.connectionState).toBe("error"));
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByText("Codex 不能直接使用这个模型服务，BootAgent 可以在本机转换后使用")).toBeTruthy();
    // The plain failure, which tells the user to switch API type, is replaced.
    expect(screen.queryByText("这个模型不支持所选的 API 类型")).toBeNull();
    expect(screen.getByRole("button", { name: "继续选择模型" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "使用兼容模式" }));
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);
    expect(state.conversion).toBe(true);
    expect(screen.getByText("已选择兼容模式，使用 Codex 时需要保持 BootAgent 运行")).toBeTruthy();
    expect(screen.getByRole("button", { name: "继续选择模型" })).toBeEnabled();
  });

  it("keeps the plain failure when compatibility mode cannot help", async () => {
    const codexStatus = {
      ...status,
      catalog: [{ id: "codex", name: "Codex", protocol: "responses", configMode: "auto", selectsModel: true }],
    } as unknown as StatusResponse;
    state = { ...baseState, status: codexStatus, statusState: "success", setupKind: "cli", selectedAgentIds: ["codex"] };
    keyRef.current = "";
    vi.spyOn(api, "probe").mockResolvedValue({
      ok: false, reachable: true, status: 401, message: "API key was rejected (401).",
      error_code: "API_KEY_REJECTED", retryable: true, convertible: null,
    });
    const page = render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));
    await waitFor(() => expect(state.connectionState).toBe("error"));
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.queryByRole("button", { name: "使用兼容模式" })).toBeNull();
    expect(screen.getByRole("button", { name: "继续选择模型" })).toBeEnabled();
  });

  // The connection test is optional, so a first-time user who skipped it got no
  // word about compatibility mode and installed a Codex that failed on its first
  // request. The protocol table answers the same question without a request.
  it("suggests compatibility mode from the protocol table, without a connection test", () => {
    state = codexOn("jiekou");
    keyRef.current = "";
    const probe = vi.spyOn(api, "probe");
    const models = vi.spyOn(api, "models").mockResolvedValue({ ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false, models: [] });
    const page = render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByText(SUGGESTION)).toBeTruthy();
    // "Not confirmed" is all the table can say; it has not shown a failure.
    expect(screen.queryByText(/不能直接使用/)).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "使用兼容模式" }));
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);
    expect(state.conversion).toBe(true);
    expect(screen.getByText("已选择兼容模式，使用 Codex 时需要保持 BootAgent 运行")).toBeTruthy();
    expect(screen.queryByText(SUGGESTION)).toBeNull();
    expect(screen.getByRole("button", { name: "继续选择模型" })).toBeEnabled();

    // Billed requests start from a button the user pressed, never from a guess.
    expect(probe).not.toHaveBeenCalled();
    // Listing models is the page's existing unbilled lookup, not a probe.
    expect(models.mock.calls.length).toBeLessThanOrEqual(1);
  });

  it("lets the user continue without taking the untested suggestion", async () => {
    state = codexOn("jiekou");
    keyRef.current = "";
    const probe = vi.spyOn(api, "probe");
    vi.spyOn(api, "models").mockResolvedValue({ ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false, models: [] });
    render(
      <MemoryRouter initialEntries={["/setup/provider"]}>
        <Routes>
          <Route path="/setup/provider" element={<ProviderKeyPage />} />
          <Route path="/setup/model" element={<h1>model step</h1>} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByText(SUGGESTION)).toBeTruthy();
    const next = screen.getByRole("button", { name: "继续选择模型" });
    expect(next).toBeEnabled();
    fireEvent.click(next);

    expect(await screen.findByRole("heading", { name: "model step" })).toBeTruthy();
    expect(state.conversion).toBe(false);
    expect(probe).not.toHaveBeenCalled();
  });

  it("makes no suggestion when the Provider has confirmed the Agent's format", () => {
    state = codexOn("deepseek");
    keyRef.current = "";
    vi.spyOn(api, "models").mockResolvedValue({ ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false, models: [] });
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.queryByText(/尚未确认/)).toBeNull();
    expect(screen.queryByRole("button", { name: "使用兼容模式" })).toBeNull();
    expect(screen.getByRole("button", { name: "继续选择模型" })).toBeEnabled();
  });

  it("makes no suggestion for a custom Provider, which has no protocol table", () => {
    const custom = {
      ...tabledStatus,
      providers: { mine: { name: "Mine", home: "", base_url: "https://api.mine.test/v1", has_key: true, custom: true } },
    } as unknown as StatusResponse;
    state = codexOn("mine", { status: custom });
    keyRef.current = "";
    vi.spyOn(api, "models").mockResolvedValue({ ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false, models: [] });
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.queryByText(/尚未确认/)).toBeNull();
    expect(screen.queryByRole("button", { name: "使用兼容模式" })).toBeNull();
  });

  // Only the untested suggestion is optional. Once a test has shown the Agent's
  // format failing while Chat Completions works, the choice is required again.
  it("requires the choice once a connection test has actually failed", async () => {
    state = codexOn("jiekou");
    keyRef.current = "";
    vi.spyOn(api, "models").mockResolvedValue({ ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false, models: [] });
    vi.spyOn(api, "probe").mockResolvedValue({
      ok: false, reachable: true, status: 404, message: "Model does not support OpenAI Responses.",
      error_code: "PROTOCOL_UNSUPPORTED", retryable: false, convertible: ["responses"],
    });
    const page = render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);
    expect(screen.getByRole("button", { name: "继续选择模型" })).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));
    await waitFor(() => expect(state.connectionState).toBe("error"));
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.queryByText(SUGGESTION)).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("Codex 不能直接使用这个模型服务，BootAgent 可以在本机转换后使用");
    expect(screen.getAllByRole("button", { name: "使用兼容模式" })).toHaveLength(1);
    expect(screen.getByRole("button", { name: "继续选择模型" })).toBeDisabled();
  });

  it("drops the suggestion once a connection test passes", async () => {
    state = codexOn("jiekou");
    keyRef.current = "";
    vi.spyOn(api, "models").mockResolvedValue({ ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false, models: [] });
    vi.spyOn(api, "probe").mockResolvedValue({ ok: true, reachable: true, status: 200, message: "ok", error_code: null, retryable: false });
    const page = render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));
    await waitFor(() => expect(state.connectionState).toBe("success"));
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.queryByText(SUGGESTION)).toBeNull();
    expect(screen.queryByRole("button", { name: "使用兼容模式" })).toBeNull();
  });

  // A first-time user has no key saved, so the key is typed on this same step
  // and cleared again when Continue saves it. Both used to reset the choice.
  it("keeps an untested choice while the key is typed and saved", async () => {
    const noKey = {
      ...tabledStatus,
      providers: { jiekou: { ...tabledStatus.providers.jiekou, has_key: false } },
    } as unknown as StatusResponse;
    state = codexOn("jiekou", { status: noKey });
    keyRef.current = "";
    const save = vi.spyOn(api, "saveProvider").mockResolvedValue({
      entry: { id: "jiekou", name: "JieKou.AI", home: "", base_url: "https://api.jiekou.test/openai", anthropic_base_url: "", api_key: "", built_in: true },
      reapplied: [],
      failures: {},
    });
    const page = render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    fireEvent.click(screen.getByRole("button", { name: "使用兼容模式" }));
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);
    fireEvent.change(screen.getByLabelText("API Key"), { target: { value: "new-key" } });
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);
    expect(state.conversion).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "继续选择模型" }));
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ id: "jiekou", api_key: "new-key" })));
    await waitFor(() => expect(keyRef.current).toBe(""));
    expect(state.conversion).toBe(true);
  });

  it("uses a custom model name for the connection test", async () => {
    state = { ...baseState, status, statusState: "success", hasApiKey: false };
    keyRef.current = "test-key";
    dispatch.mockClear();
    const probe = vi.spyOn(api, "probe").mockResolvedValue({
      ok: true,
      reachable: true,
      status: 200,
      message: "ok",
      error_code: null,
      retryable: false,
    });
    const page = render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    fireEvent.change(screen.getByLabelText("测试用模型（可选）"), { target: { value: "vendor/custom-model" } });
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);
    expect(screen.queryByLabelText("API Key")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));

    await waitFor(() =>
      expect(probe).toHaveBeenCalledWith(expect.objectContaining({ model: "vendor/custom-model", apiKey: "" })),
    );
  });

  it("saves an inline key for a built-in Provider and continues", async () => {
    state = {
      ...baseState,
      status: { ...status, providers: { ppio: { ...status.providers.ppio, has_key: false } } },
      statusState: "success",
    };
    keyRef.current = "";
    const save = vi.spyOn(api, "saveProvider").mockResolvedValue({
      entry: { id: "ppio", name: "PPIO", home: "", base_url: "https://api.ppio.com/openai", anthropic_base_url: "", api_key: "", built_in: true },
      reapplied: [],
      failures: {},
    });
    const page = render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByRole("button", { name: "测试连接" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("API Key"), { target: { value: "new-key" } });
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "继续选择模型" }));

    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ id: "ppio", api_key: "new-key", create: false })));
    expect(refreshStatus).toHaveBeenCalled();
    expect(keyRef.current).toBe("");
  });

  // The field was a bare text input, so the models this Key can actually reach
  // were invisible and had to be typed from memory.
  it("offers the discovered models from the probe field", async () => {
    // A selected Agent is what gives the step a protocol, and without one
    // discovery does not run at all.
    state = {
      ...baseState,
      status: { ...status, catalog: [{ id: "codex", name: "Codex", protocol: "openai" }] } as unknown as StatusResponse,
      statusState: "success",
      selectedAgentIds: ["codex"],
      hasApiKey: true,
    };
    keyRef.current = "";
    vi.spyOn(api, "models").mockResolvedValue({
      ok: true, reachable: true, status: 200, message: "Found 2 models.",
      error_code: null, retryable: false, models: ["deepseek/deepseek-v4-pro", "qwen/qwen3-coder"],
    });
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    await waitFor(() => expect(screen.getByRole("button", { name: "展开模型列表" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "展开模型列表" }));
    fireEvent.click(screen.getByRole("radio", { name: /qwen\/qwen3-coder/ }));
    expect(dispatch).toHaveBeenCalledWith({ type: "SET_PROBE_MODEL", value: "qwen/qwen3-coder" });
  });

  it("keeps the probe model field free of duplicate guidance", () => {
    state = { ...baseState, status, statusState: "success", hasApiKey: true };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.queryByText(/不会写入任何配置/)).toBeNull();
    expect(screen.queryByText(/真正使用的模型在下一步选择/)).toBeNull();
  });

  it("leaves the probe model optional", () => {
    // Empty is the common path: the backend picks a model for the probe.
    state = { ...baseState, status, statusState: "success", hasApiKey: true };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByLabelText("测试用模型（可选）").hasAttribute("required")).toBe(false);
  });

  it("keeps custom Providers on the management-page path", () => {
    state = {
      ...baseState,
      status: { ...status, providers: { custom: { ...status.providers.ppio, custom: true, has_key: false } } },
      provider: "custom",
      statusState: "success",
    };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByRole("button", { name: "前往模型服务" })).toBeTruthy();
    expect(screen.queryByLabelText("API Key")).toBeNull();
  });

  it("allows continuing without a connection test", () => {
    state = { ...baseState, status, statusState: "success", hasApiKey: true, keyVerified: false };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByRole("button", { name: "继续选择模型" })).not.toBeDisabled();
  });

  // Continuing used to return early on providerHasKey alone, before ever reading
  // the ref -- so a key typed in this session was dropped with no save call and no
  // message, while the page advanced as though it had worked.
  it("saves a key typed in this session even when one is already stored", async () => {
    state = { ...baseState, status, statusState: "success", hasApiKey: true };
    keyRef.current = "sk-rotated";
    const save = vi.spyOn(api, "saveProvider").mockResolvedValue({
      entry: {
        id: "ppio", name: "PPIO", home: "", base_url: "https://api.ppio.com/openai",
        anthropic_base_url: "", api_key: "sk-rotated", built_in: true,
      },
      reapplied: null,
      failures: null,
    });
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    fireEvent.click(screen.getByRole("button", { name: "继续选择模型" }));
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ api_key: "sk-rotated" })));
  });

  it("reports a failed key save as a receipt and stays on the page", async () => {
    state = { ...baseState, status, statusState: "success", hasApiKey: true };
    keyRef.current = "sk-rotated";
    vi.spyOn(api, "saveProvider").mockRejectedValue(new BootAgentApiError("disk full", "CONFIG_WRITE_FAILED", false, 500));
    render(<FeedbackProvider><MemoryRouter><ProviderKeyPage /></MemoryRouter></FeedbackProvider>);

    fireEvent.click(screen.getByRole("button", { name: "继续选择模型" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("无法保存模型服务");
    expect(document.querySelector(".provider-form p")).toBeNull();
  });

  it("continues without a save when nothing was typed", async () => {
    state = { ...baseState, status, statusState: "success", hasApiKey: true };
    keyRef.current = "";
    const save = vi.spyOn(api, "saveProvider");
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    fireEvent.click(screen.getByRole("button", { name: "继续选择模型" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "继续选择模型" })).not.toBeDisabled());
    expect(save).not.toHaveBeenCalled();
  });

  it("offers the key button for a Provider with only a key page", () => {
    // No home URL, so the pre-change guard would have hidden the button. The
    // backend picks the URL; the page only decides whether to offer the action.
    state = {
      ...baseState,
      status: {
        ...status,
        providers: { ppio: { ...status.providers.ppio, home: "", key_management_url: "https://ppio.com/settings/key-management" } },
      } as unknown as StatusResponse,
      statusState: "success",
      hasApiKey: true,
    };
    const open = vi.spyOn(api, "openRegister").mockResolvedValue({ ok: true, url: "", message: "" });
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    fireEvent.click(screen.getByRole("button", { name: "获取 API Key" }));
    // No URL is passed: the backend re-resolves it, so a tampered frontend
    // cannot choose what gets opened.
    expect(open).toHaveBeenCalledWith("ppio", expect.anything());
  });

  it("hides the key button when the Provider publishes neither URL", () => {
    state = { ...baseState, status, statusState: "success", hasApiKey: true };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.queryByRole("button", { name: "获取 API Key" })).toBeNull();
  });

  describe("what a key is, for someone who has never had one", () => {
    const withoutKey = (provider: Record<string, unknown> = {}) => ({
      ...baseState,
      status: {
        ...status,
        providers: { ppio: { ...status.providers.ppio, has_key: false, key_management_url: "https://ppio.com/settings/key-management", ...provider } },
      } as unknown as StatusResponse,
      statusState: "success" as const,
    });
    const explainer = () => screen.queryByRole("list", { name: "关于 API Key" });

    it("says what it is, where it comes from and who charges for it", () => {
      state = withoutKey();
      render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

      const lines = [...(explainer()?.children ?? [])].map((line) => line.textContent);
      expect(lines).toEqual([
        "API Key 是 PPIO 发给你的访问凭证，Agent 用它调用模型",
        "还没有的话，点「获取 API Key」打开 PPIO 的官方页面，在那里注册并创建",
        // No amount and no promise: the price is the Provider's to state.
        "连通测试和 Agent 的每次调用都是真实请求，由 PPIO 按它的价格计费；BootAgent 不收费，也不预估费用",
      ]);
    });

    // The second line names a button. Without a published page there is no
    // button, and the sentence would send the user looking for one.
    it("does not point at a button that is not there", () => {
      state = withoutKey({ key_management_url: "", home: "" });
      render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

      expect(explainer()?.children).toHaveLength(2);
      expect(explainer()?.textContent).not.toContain("获取 API Key");
    });

    it("stays out of the way once a key is saved", () => {
      state = { ...baseState, status, statusState: "success", hasApiKey: true };
      render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

      expect(explainer()).toBeNull();
    });
  });
});
