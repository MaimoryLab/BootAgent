import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { BootAgentApiError } from "../backend/errors";
import { FeedbackViewport } from "../components/FeedbackViewport";
import { I18nProvider, LOCALE_STORAGE_KEY } from "../i18n";
import {
  advanceFeedback,
  enqueueFeedback,
  FEEDBACK_SUCCESS_MS,
  FeedbackProvider,
  type FeedbackItem,
  useFeedback,
} from "./FeedbackContext";

let feedback: ReturnType<typeof useFeedback>;

function Capture() {
  feedback = useFeedback();
  return null;
}

function mount() {
  render(
    <I18nProvider>
      <FeedbackProvider>
        <Capture />
        <FeedbackViewport />
      </FeedbackProvider>
    </I18nProvider>,
  );
}

const item = (id: number, tone: FeedbackItem["tone"], key = `k${id}`, createdAt = 0): FeedbackItem =>
  ({ id, key, tone, message: `m${id}`, createdAt });

describe("feedback queue", () => {
  it("replaces a message with the same key in place", () => {
    const next = enqueueFeedback([item(1, "failure", "launch"), item(2, "success")], item(3, "success", "launch"));
    expect(next.map((entry) => entry.id)).toEqual([3, 2]);
  });

  it("queues a success behind an open failure instead of replacing it", () => {
    const next = enqueueFeedback([item(1, "failure")], item(2, "success"));
    expect(next.map((entry) => entry.id)).toEqual([1, 2]);
  });

  it("drops the oldest waiting success first when the queue is full", () => {
    const full = [item(1, "failure"), item(2, "failure"), item(3, "success"), item(4, "failure")];
    expect(enqueueFeedback(full, item(5, "failure")).map((entry) => entry.id)).toEqual([1, 2, 4, 5]);
  });

  it("discards a success that waited longer than it would have been shown", () => {
    const remaining = advanceFeedback([item(1, "success", "a", 0), item(2, "failure", "b", 0), item(3, "success", "c", 9_000)], 10_000);
    expect(remaining.map((entry) => entry.id)).toEqual([2, 3]);
  });
});

describe("FeedbackViewport", () => {
  beforeEach(() => {
    localStorage.setItem(LOCALE_STORAGE_KEY, "zh-CN");
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("renders nothing without a provider", () => {
    const { container } = render(<FeedbackViewport />);
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps the live region mounted while empty", () => {
    mount();
    expect(screen.getByLabelText("通知")).toHaveAttribute("aria-live", "polite");
  });

  it("retires a success after three seconds", () => {
    mount();
    act(() => feedback.show({ tone: "success", message: "已保存" }));
    expect(screen.getByText("已保存")).toBeInTheDocument();
    // A success is announced politely, not as an alert.
    expect(screen.queryByRole("alert")).toBeNull();
    act(() => vi.advanceTimersByTime(FEEDBACK_SUCCESS_MS));
    expect(screen.queryByText("已保存")).toBeNull();
  });

  it("holds a success while the pointer is over it", () => {
    mount();
    act(() => feedback.show({ tone: "success", message: "已保存" }));
    fireEvent.mouseEnter(screen.getByText("已保存").closest(".feedback-toast")!);
    act(() => vi.advanceTimersByTime(FEEDBACK_SUCCESS_MS * 3));
    expect(screen.getByText("已保存")).toBeInTheDocument();
    fireEvent.mouseLeave(screen.getByText("已保存").closest(".feedback-toast")!);
    act(() => vi.advanceTimersByTime(FEEDBACK_SUCCESS_MS));
    expect(screen.queryByText("已保存")).toBeNull();
  });

  it("keeps a failure until it is closed, then shows what waited behind it", () => {
    mount();
    act(() => feedback.show({ tone: "failure", message: "无法启动 Codex" }));
    act(() => feedback.show({ tone: "success", message: "已保存" }));
    act(() => vi.advanceTimersByTime(FEEDBACK_SUCCESS_MS / 2));
    expect(screen.getByRole("alert")).toHaveTextContent("无法启动 Codex");
    expect(screen.queryByText("已保存")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(screen.getByText("已保存")).toBeInTheDocument();
  });

  it("closes on Escape", () => {
    mount();
    act(() => feedback.show({ tone: "failure", message: "无法启动 Codex" }));
    fireEvent.keyDown(screen.getByRole("alert"), { key: "Escape" });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("puts the localised reason, the remedy and the code behind 详情", () => {
    mount();
    act(() => feedback.failure(new BootAgentApiError("Endpoint returned HTTP 429.", "PROVIDER_UNREACHABLE", true, 429), "无法检测模型"));
    expect(screen.getByRole("alert")).toHaveTextContent("无法检测模型");
    expect(screen.queryByText(/请求过于频繁/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "详情" }));
    const detail = screen.getByText(/请求过于频繁/);
    expect(detail).toHaveTextContent("稍等一会儿再试");
    expect(detail).toHaveTextContent("错误码: PROVIDER_UNREACHABLE");
    // The English backend sentence is for logs, not for this card.
    expect(detail).not.toHaveTextContent("Endpoint returned");
  });

  it("says nothing about a cancellation", () => {
    mount();
    act(() => feedback.failure(Object.assign(new Error("cancelled"), { name: "CancelError" }), "无法启动 Codex"));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("closes itself before running its action", () => {
    mount();
    const run = vi.fn();
    act(() => feedback.show({ tone: "failure", message: "无法启动 Codex", action: { label: "重试", run } }));
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(run).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
