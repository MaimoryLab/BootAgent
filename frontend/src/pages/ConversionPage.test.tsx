import { render, screen } from "@testing-library/react";
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
  it("explains the adapter purpose to Chinese users", async () => {
    render(<ConversionPage />);

    expect(await screen.findByText(/不同 Agent 要求的 API 协议不一样/)).toBeTruthy();
    expect(screen.getByText(/BootAgent 会在本机监听一个地址/)).toBeTruthy();
    expect(screen.queryByText("适配器介绍")).toBeNull();
  });
});
