import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
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

  it("uses a custom model name for the connection test", async () => {
    state = { ...initialWizardState, status, statusState: "success", hasApiKey: false };
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
      ...initialWizardState,
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
      ...initialWizardState,
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

  // A model entered here is not configured anywhere, and a user who assumes it is
  // will not understand why the next step asks again.
  it("says the probe model configures nothing", () => {
    state = { ...initialWizardState, status, statusState: "success", hasApiKey: true };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByText(/不会写入任何配置/)).toBeTruthy();
    expect(screen.getByText(/真正使用的模型在下一步选择/)).toBeTruthy();
  });

  it("leaves the probe model optional", () => {
    // Empty is the common path: the backend picks a model for the probe.
    state = { ...initialWizardState, status, statusState: "success", hasApiKey: true };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByLabelText("测试用模型（可选）").hasAttribute("required")).toBe(false);
  });

  it("keeps custom Providers on the management-page path", () => {
    state = {
      ...initialWizardState,
      status: { ...status, providers: { custom: { ...status.providers.ppio, custom: true, has_key: false } } },
      provider: "custom",
      statusState: "success",
    };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByRole("button", { name: "前往模型服务" })).toBeTruthy();
    expect(screen.queryByLabelText("API Key")).toBeNull();
  });

  it("allows continuing without a connection test", () => {
    state = { ...initialWizardState, status, statusState: "success", hasApiKey: true, keyVerified: false };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByRole("button", { name: "继续选择模型" })).not.toBeDisabled();
  });

  // Continuing used to return early on providerHasKey alone, before ever reading
  // the ref -- so a key typed in this session was dropped with no save call and no
  // message, while the page advanced as though it had worked.
  it("saves a key typed in this session even when one is already stored", async () => {
    state = { ...initialWizardState, status, statusState: "success", hasApiKey: true };
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

  it("continues without a save when nothing was typed", async () => {
    state = { ...initialWizardState, status, statusState: "success", hasApiKey: true };
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
      ...initialWizardState,
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
    state = { ...initialWizardState, status, statusState: "success", hasApiKey: true };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.queryByRole("button", { name: "获取 API Key" })).toBeNull();
  });
});
