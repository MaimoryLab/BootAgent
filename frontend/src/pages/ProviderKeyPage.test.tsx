import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { initialWizardState, wizardReducer, type WizardAction, type WizardState } from "../state/wizardReducer";
import type { StatusResponse } from "../types/api";
import { ProviderKeyPage } from "./ProviderKeyPage";

let state: WizardState;
const keyRef = { current: "test-key" };
const dispatch = vi.fn((action: WizardAction) => {
  state = wizardReducer(state, action);
});

const status = {
  providers: { ppio: { name: "PPIO", home: "", base_url: "https://api.ppinfra.com/openai", has_key: true } },
  catalog: [],
} as unknown as StatusResponse;

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({
    state,
    dispatch,
    secret: { keyRef, setApiKey: vi.fn(), clearApiKey: vi.fn() },
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

    fireEvent.change(screen.getByLabelText("自定义模型名称（可选）"), { target: { value: "vendor/custom-model" } });
    page.rerender(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);
    expect(screen.queryByLabelText("API Key")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));

    await waitFor(() =>
      expect(probe).toHaveBeenCalledWith(expect.objectContaining({ model: "vendor/custom-model", apiKey: "" })),
    );
  });

  it("blocks probing when the Provider has no saved key", () => {
    state = {
      ...initialWizardState,
      status: { ...status, providers: { ppio: { ...status.providers.ppio, has_key: false } } },
      statusState: "success",
    };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByRole("button", { name: "测试连接" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "前往 Provider" })).toBeTruthy();
    expect(screen.queryByLabelText("API Key")).toBeNull();
  });

  it("allows continuing without a connection test", () => {
    state = { ...initialWizardState, status, statusState: "success", hasApiKey: true, keyVerified: false };
    render(<MemoryRouter><ProviderKeyPage /></MemoryRouter>);

    expect(screen.getByRole("button", { name: "继续选择模型" })).not.toBeDisabled();
  });
});
