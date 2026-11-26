import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import * as confirm from "../state/confirmDelete";
import { FeedbackProvider } from "../state/FeedbackContext";
import type { ConversionConfig, StatusResponse } from "../types/api";
import { CompatibilitySetting } from "./CompatibilitySetting";
import { FeedbackViewport } from "./FeedbackViewport";

const nobody = { providers: {}, catalog: [], agents: {}, desktopAgents: [], profiles: [], conversionRoutes: {} } as unknown as StatusResponse;
let mockStatus: StatusResponse | null = nobody;

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: { status: mockStatus }, refreshStatus: vi.fn() }),
}));

const stopped: ConversionConfig = {
  enabled: false,
  listen: "127.0.0.1:8787",
  has_api_key: false,
  target_profile: "",
  routes: {},
  anthropic_model: "claude-sonnet-5",
  responses_model: "gpt-5.6-sol",
  chat_model: "gpt-5.6-sol",
};

const codexInCompatibilityMode = {
  providers: { ppio: { name: "PPIO" } },
  catalog: [{ id: "codex", name: "Codex" }, { id: "claude-code", name: "Claude Code" }],
  agents: { codex: { profileId: "bootagent-converter-for-codex" }, "claude-code": { profileId: "bootagent-converter-for-claude-code" } },
  desktopAgents: [],
  profiles: [{ id: "ppio-chat", label: "PPIO", provider: "ppio", model: "deepseek-v3", protocol: "openai" }],
  conversionRoutes: { codex: "ppio-chat", "claude-code": "ppio-chat" },
} as unknown as StatusResponse;

beforeEach(() => {
  localStorage.setItem("bootagent.locale", "zh-CN");
  vi.spyOn(api, "getConversion").mockResolvedValue(stopped);
});

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  mockStatus = nobody;
});

// PageScaffold normally draws the receipts; a bare component needs the viewport.
const renderSetting = () => render(<FeedbackProvider><FeedbackViewport /><CompatibilitySetting /></FeedbackProvider>);

describe("CompatibilitySetting", () => {
  // Nothing to turn on by hand: an Agent switches it on when it needs it.
  it("is off and cannot be switched on while no Agent uses it", async () => {
    renderSetting();
    expect(await screen.findByText("Agent 需要时会自动开启")).toBeTruthy();
    const toggle = screen.getByRole("switch", { name: "兼容模式" });
    expect(toggle).not.toBeChecked();
    expect(toggle).toBeDisabled();
  });

  // Before the status has loaded it is not known who uses it, so the row does
  // not claim that nobody does and cannot be stopped without the warning.
  it("waits for the status before saying who uses it or letting it be stopped", async () => {
    mockStatus = null;
    vi.spyOn(api, "getConversion").mockResolvedValue({ ...stopped, enabled: true, has_api_key: true });
    renderSetting();
    const toggle = await screen.findByRole("switch", { name: "兼容模式" });
    await waitFor(() => expect(toggle).toBeChecked());
    expect(toggle).toBeDisabled();
    expect(screen.queryByText("没有 Agent 在使用")).toBeNull();
    expect(screen.getByText("正在读取环境状态")).toBeTruthy();
  });

  it("names the Agents using it while it runs", async () => {
    mockStatus = codexInCompatibilityMode;
    vi.spyOn(api, "getConversion").mockResolvedValue({ ...stopped, enabled: true, has_api_key: true });
    renderSetting();
    expect(await screen.findByText("Codex、Claude Code 正在使用，使用期间需要保持 BootAgent 运行")).toBeTruthy();
    expect(screen.getByRole("switch", { name: "兼容模式" })).toBeChecked();
  });

  // Stopping breaks every Agent pointed at it, so it names them and asks.
  it("asks before stopping it under the Agents that use it", async () => {
    mockStatus = codexInCompatibilityMode;
    vi.spyOn(api, "getConversion").mockResolvedValue({ ...stopped, enabled: true, has_api_key: true });
    const save = vi.spyOn(api, "saveConversion").mockImplementation(async (config) => config);
    const ask = vi.spyOn(confirm, "confirmAction").mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    renderSetting();
    const toggle = await screen.findByRole("switch", { name: "兼容模式" });
    await waitFor(() => expect(toggle).toBeChecked());

    fireEvent.click(toggle);
    await waitFor(() => expect(ask).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining("Codex、Claude Code") })));
    expect(save).not.toHaveBeenCalled();

    fireEvent.click(toggle);
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ enabled: false })));
    expect(await screen.findByText("兼容模式已停止")).toBeTruthy();
    expect(screen.getByText("已停止，Codex、Claude Code 暂时无法使用")).toBeTruthy();
    // With Agents waiting on it, it can be switched back on.
    expect(toggle).toBeEnabled();
  });

  it("offers a retry when the settings cannot be read", async () => {
    const load = vi.spyOn(api, "getConversion").mockRejectedValueOnce(new BootAgentApiError("boom", "INTERNAL_ERROR", true, 500));
    renderSetting();
    const receipt = await screen.findByRole("alert");
    expect(receipt).toHaveTextContent("无法读取兼容模式设置");
    expect(screen.getByRole("switch", { name: "兼容模式" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByText("高级设置")).toBeTruthy();
    expect(load).toHaveBeenCalledTimes(2);
  });

  it("saves an advanced setting without turning compatibility mode on", async () => {
    const save = vi.spyOn(api, "saveConversion").mockImplementation(async (config) => config);
    renderSetting();
    fireEvent.click(await screen.findByText("高级设置"));
    const saveButton = screen.getByRole("button", { name: "保存更改" });
    expect(saveButton).toBeDisabled();
    fireEvent.change(screen.getByLabelText("监听地址"), { target: { value: "127.0.0.1:9797" } });
    fireEvent.click(saveButton);
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ listen: "127.0.0.1:9797", enabled: false })));
    expect(await screen.findByText("已保存兼容模式设置")).toBeTruthy();
  });

  // A half-typed address belongs to the save button, not to the switch.
  it("does not save edited fields when only the switch is used", async () => {
    mockStatus = codexInCompatibilityMode;
    const save = vi.spyOn(api, "saveConversion").mockImplementation(async (config) => config);
    renderSetting();
    fireEvent.click(await screen.findByText("高级设置"));
    fireEvent.change(screen.getByLabelText("监听地址"), { target: { value: "127.0.0.1:1" } });
    fireEvent.click(screen.getByRole("switch", { name: "兼容模式" }));
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, listen: "127.0.0.1:8787" })));
  });

  it("keeps the details folded and never shows a local key", async () => {
    renderSetting();
    const fold = (await screen.findByText("高级设置")).closest("details");
    expect(fold).not.toHaveAttribute("open");
    expect(screen.queryByLabelText("本地 API Key")).toBeNull();
    expect(screen.getByText("启动时自动生成")).toBeTruthy();
  });
});
