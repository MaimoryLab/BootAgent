import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { ConversionPage } from "./ConversionPage";

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: { status: null }, refreshStatus: vi.fn() }),
}));

beforeEach(() => {
  localStorage.setItem("bootagent.locale", "zh-CN");
  vi.spyOn(api, "getConversion").mockResolvedValue({
    enabled: false,
    listen: "127.0.0.1:8787",
    api_key: "",
    target_profile: "",
    anthropic_model: "claude-sonnet-5",
    responses_model: "gpt-5.6-sol",
    chat_model: "gpt-5.6-sol",
  });
});

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("ConversionPage", () => {
  it("keeps the operational controls ahead of optional protocol guidance", async () => {
    render(
      <MemoryRouter>
        <ConversionPage />
      </MemoryRouter>,
    );

    const status = await screen.findByText("适配服务已停止");
    const target = screen.getByText("请求最终发往");
    const guidance = screen.getByText("了解协议适配");

    expect(status.compareDocumentPosition(target) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(target.compareDocumentPosition(guidance) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.queryByText(/不同 Agent 要求的 API 协议不一样/)).toBeNull();

    fireEvent.click(guidance);
    expect(screen.getByText(/不同 Agent 使用的 API 协议并不相同/)).toBeTruthy();
  });

  it("offers an actionable route when no compatible profile exists", async () => {
    render(
      <MemoryRouter>
        <ConversionPage />
      </MemoryRouter>,
    );

    expect(await screen.findByRole("link", { name: "创建配置模板" })).toHaveAttribute("href", "/profiles");
  });
});
