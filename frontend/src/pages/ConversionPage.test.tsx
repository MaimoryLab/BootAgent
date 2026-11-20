import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import { FeedbackProvider } from "../state/FeedbackContext";
import type { StatusResponse } from "../types/api";
import { ConversionPage } from "./ConversionPage";

let mockStatus: StatusResponse | null = null;

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: { status: mockStatus }, refreshStatus: vi.fn() }),
}));

beforeEach(() => {
  localStorage.setItem("bootagent.locale", "zh-CN");
  vi.spyOn(api, "getConversion").mockResolvedValue({
    enabled: false,
    listen: "127.0.0.1:8787",
    has_api_key: false,
    target_profile: "",
    routes: {},
    anthropic_model: "claude-sonnet-5",
    responses_model: "gpt-5.6-sol",
    chat_model: "gpt-5.6-sol",
  });
});

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  mockStatus = null;
});

describe("ConversionPage", () => {
  it("leads with status and the Agents using it, and keeps the format details behind a disclosure", async () => {
    render(
      <MemoryRouter>
        <ConversionPage />
      </MemoryRouter>,
    );

    const status = await screen.findByText("兼容模式未开启");
    const users = screen.getByText("正在使用的 Agent");
    const guidance = screen.getByText("了解兼容模式");

    expect(status.compareDocumentPosition(users) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(users.compareDocumentPosition(guidance) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // There is no single model to pick: each Agent has its own.
    expect(screen.queryByRole("combobox")).toBeNull();
    const formats = screen.getByText(/OpenAI Responses 格式/);
    expect(formats.closest("details")).not.toHaveAttribute("open");
    fireEvent.click(guidance);
    expect(formats.closest("details")).toHaveAttribute("open");
  });

  it("offers one primary action, and no stop button while stopped", async () => {
    render(
      <MemoryRouter>
        <ConversionPage />
      </MemoryRouter>,
    );

    // Nothing uses it yet, so there is nothing to turn on.
    expect(await screen.findByRole("button", { name: "开启兼容模式" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "停止兼容模式" })).toBeNull();
  });

  // A failed load leaves the page with nothing on it, so the receipt carries
  // the retry rather than the page.
  it("offers a retry when the settings cannot be read", async () => {
    const load = vi.spyOn(api, "getConversion").mockRejectedValueOnce(new BootAgentApiError("boom", "INTERNAL_ERROR", true, 500));
    render(
      <FeedbackProvider>
        <MemoryRouter>
          <ConversionPage />
        </MemoryRouter>
      </FeedbackProvider>,
    );
    const receipt = await screen.findByRole("alert");
    expect(receipt).toHaveTextContent("无法读取兼容模式设置");
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByText("兼容模式未开启")).toBeTruthy();
    expect(load).toHaveBeenCalledTimes(2);
  });

  it("confirms a saved change with a receipt instead of a line in the page", async () => {
    vi.spyOn(api, "saveConversion").mockImplementation(async (config) => config);
    vi.spyOn(api, "getSettings").mockResolvedValue({ autostart: true } as never);
    render(
      <FeedbackProvider>
        <MemoryRouter>
          <ConversionPage />
        </MemoryRouter>
      </FeedbackProvider>,
    );
    await screen.findByText("兼容模式未开启");
    fireEvent.click(screen.getByRole("button", { name: "高级设置" }));
    fireEvent.change(screen.getByLabelText("监听地址"), { target: { value: "127.0.0.1:9797" } });
    fireEvent.click(screen.getByRole("button", { name: "保存更改" }));
    expect(await screen.findByText("已保存兼容模式设置")).toBeTruthy();
  });

  it("saves an advanced setting without turning compatibility mode on", async () => {
    const save = vi.spyOn(api, "saveConversion").mockImplementation(async (config) => config);
    vi.spyOn(api, "getSettings").mockResolvedValue({ autostart: true } as never);
    render(
      <MemoryRouter>
        <ConversionPage />
      </MemoryRouter>,
    );

    await screen.findByText("兼容模式未开启");
    fireEvent.click(screen.getByRole("button", { name: "高级设置" }));
    fireEvent.change(screen.getByLabelText("监听地址"), { target: { value: "127.0.0.1:9797" } });
    fireEvent.click(screen.getByRole("button", { name: "保存更改" }));
    await waitFor(() => expect(save).toHaveBeenCalledWith(expect.objectContaining({ listen: "127.0.0.1:9797", enabled: false })));
  });

  // The single shared target this replaced showed every Agent on the same
  // model; each now names the service its own requests go to.
  it("shows each Agent's own model service and links to change only that Agent", async () => {
    mockStatus = {
      providers: { ppio: { name: "PPIO" }, novita: { name: "Novita" } },
      catalog: [{ id: "codex", name: "Codex" }, { id: "claude-code", name: "Claude Code" }],
      agents: {
        codex: { profileId: "bootagent-converter-for-codex" },
        "claude-code": { profileId: "bootagent-converter-for-claude-code" },
      },
      desktopAgents: [],
      profiles: [
        { id: "ppio-chat", label: "PPIO", provider: "ppio", model: "deepseek-v3", protocol: "openai" },
        { id: "novita-chat", label: "Novita", provider: "novita", model: "glm-5", protocol: "openai" },
      ],
      conversionRoutes: { codex: "ppio-chat", "claude-code": "novita-chat" },
    } as unknown as StatusResponse;
    render(
      <MemoryRouter>
        <ConversionPage />
      </MemoryRouter>,
    );

    const list = await screen.findByRole("list");
    const rows = Array.from(list.querySelectorAll("li")).map((row) => row.textContent);
    expect(rows[0]).toContain("Codex");
    expect(rows[0]).toContain("PPIO · deepseek-v3");
    expect(rows[1]).toContain("Claude Code");
    expect(rows[1]).toContain("Novita · glm-5");
    expect(screen.getByRole("button", { name: "为 Codex 更换模型" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "开启兼容模式" })).toBeEnabled();
  });

  it("does not expose a local API key input", async () => {
    render(
      <MemoryRouter>
        <ConversionPage />
      </MemoryRouter>,
    );

    await screen.findByText("兼容模式未开启");
    fireEvent.click(screen.getByRole("button", { name: "高级设置" }));
    expect(screen.queryByLabelText("本地 API Key")).toBeNull();
    expect(screen.getByText("启动时自动生成")).toBeTruthy();
  });
});
