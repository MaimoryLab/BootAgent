import { act, fireEvent, render, screen } from "@testing-library/react";
import { Play } from "lucide-react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { BootAgentApiError } from "../backend/errors";
import { I18nProvider, LOCALE_STORAGE_KEY } from "../i18n";
import { FeedbackProvider } from "../state/FeedbackContext";
import { ACTION_DONE_MS, ActionButton } from "./ActionButton";
import { FeedbackViewport } from "./FeedbackViewport";

function mount(onAction: () => unknown) {
  render(
    <I18nProvider>
      <FeedbackProvider>
        <ActionButton icon={Play} label="启动" doneLabel="已打开" failureMessage="无法启动 Codex" onAction={onAction} />
        <FeedbackViewport />
      </FeedbackProvider>
    </I18nProvider>,
  );
}

/** Settles the awaited action inside act, without letting fake time move. */
const settle = () => act(async () => { await Promise.resolve(); });

describe("ActionButton", () => {
  beforeEach(() => {
    localStorage.setItem(LOCALE_STORAGE_KEY, "zh-CN");
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("confirms in place, then returns to its label", async () => {
    let resolve: () => void = () => {};
    mount(() => new Promise<void>((done) => { resolve = done; }));
    const button = screen.getByRole("button", { name: "启动" });

    fireEvent.click(button);
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("aria-busy", "true");

    resolve();
    await settle();
    // Same element throughout: the face changes, the button does not.
    expect(screen.getByRole("button", { name: "已打开" })).toBe(button);
    expect(button).toBeEnabled();

    act(() => vi.advanceTimersByTime(ACTION_DONE_MS));
    expect(screen.getByRole("button", { name: "启动" })).toBe(button);
  });

  it("claims nothing when the action did not go ahead", async () => {
    mount(() => Promise.resolve(false));
    fireEvent.click(screen.getByRole("button", { name: "启动" }));
    await settle();
    expect(screen.getByRole("button", { name: "启动" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "已打开" })).toBeNull();
  });

  it("returns to its label and reports a failure as a receipt", async () => {
    mount(() => Promise.reject(new BootAgentApiError("Invalid launch directory: /missing", "INVALID_REQUEST", false, 400)));
    fireEvent.click(screen.getByRole("button", { name: "启动" }));
    await settle();
    expect(screen.getByRole("button", { name: "启动" })).toBeEnabled();
    expect(screen.getByRole("alert")).toHaveTextContent("无法启动 Codex");
    fireEvent.click(screen.getByRole("button", { name: "详情" }));
    expect(screen.getByText(/Invalid launch directory: \/missing/)).toBeInTheDocument();
  });
});
