import { afterEach, describe, expect, it, vi } from "vitest";

// Hoisted so the module factory can close over it: vi.mock is lifted above the
// imports, so a plain const here would not exist yet when the factory runs.
const { question } = vi.hoisted(() => ({ question: vi.fn<() => Promise<string>>() }));
vi.mock("@wailsio/runtime", () => ({ Dialogs: { Question: question } }));

const { confirmDelete } = await import("./confirmDelete");

const options = { title: "Delete", message: "Delete “acme”?", confirmLabel: "删除", cancelLabel: "取消" };

describe("confirmDelete", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    question.mockReset();
    delete (window as typeof window & { webkit?: unknown }).webkit;
    vi.useRealTimers();
  });

  it("approves only the confirm button", async () => {
    question.mockResolvedValue("删除");
    expect(await confirmDelete(options)).toBe(true);
    question.mockResolvedValue("取消");
    expect(await confirmDelete(options)).toBe(false);
  });

  it("waits for an answer in a native WebView", async () => {
    vi.useFakeTimers();
    Object.defineProperty(window, "webkit", {
      configurable: true,
      value: { messageHandlers: { external: { postMessage: vi.fn() } } },
    });
    question.mockImplementation(() => new Promise((resolve) => setTimeout(() => resolve("删除"), 2_000)));
    const confirm = vi.spyOn(window, "confirm");

    const result = confirmDelete(options);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(await result).toBe(true);
    expect(confirm).not.toHaveBeenCalled();
  });

  // Dialogs.Question needs a WebView to host the dialog. Under `-tags server`
  // there is none and the call never settles -- it does not reject, so a catch
  // could not see it. This is what broke the E2E delete: the button did nothing
  // and produced no error.
  it("falls back to window.confirm when the native dialog never answers", async () => {
    question.mockReturnValue(new Promise(() => {}));
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    expect(await confirmDelete(options)).toBe(true);
    expect(confirm).toHaveBeenCalledWith(options.message);

    confirm.mockReturnValue(false);
    expect(await confirmDelete(options)).toBe(false);
  });

  it("falls back when the native dialog rejects outright", async () => {
    question.mockRejectedValue(new Error("no webview"));
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    expect(await confirmDelete(options)).toBe(true);
  });

  // A declined native dialog is an answer. Re-prompting through window.confirm
  // would ask the user twice and turn a "no" into a second chance to say yes.
  it("does not re-prompt when the native dialog was declined", async () => {
    question.mockResolvedValue("取消");
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    expect(await confirmDelete(options)).toBe(false);
    expect(confirm).not.toHaveBeenCalled();
  });

  it("refuses when neither prompt can be shown", async () => {
    question.mockRejectedValue(new Error("no webview"));
    // A hardened webview can omit window.confirm; "cannot ask" must not delete.
    vi.stubGlobal("confirm", undefined);
    expect(await confirmDelete(options)).toBe(false);
    vi.unstubAllGlobals();
  });
});
