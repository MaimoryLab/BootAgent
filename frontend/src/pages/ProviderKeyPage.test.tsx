import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { initialWizardState, wizardReducer, type WizardAction, type WizardState } from "../state/wizardReducer";
import { ProviderKeyPage } from "./ProviderKeyPage";

let state: WizardState;
const keyRef = { current: "test-key" };
const dispatch = vi.fn((action: WizardAction) => {
  state = wizardReducer(state, action);
});

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({
    state,
    dispatch,
    secret: { keyRef, setApiKey: vi.fn(), clearApiKey: vi.fn() },
  }),
}));

describe("ProviderKeyPage", () => {
  it("uses a custom model name for the connection test", async () => {
    state = { ...initialWizardState, hasApiKey: true };
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
    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));

    await waitFor(() =>
      expect(probe).toHaveBeenCalledWith(expect.objectContaining({ model: "vendor/custom-model", apiKey: "test-key" })),
    );
  });
});
