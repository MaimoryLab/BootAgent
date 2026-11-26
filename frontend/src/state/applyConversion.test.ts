import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { sourceTranslate } from "../i18n";
import type { Settings } from "../types/api";
import { offerAutostart, resetAutostartOffer } from "./applyConversion";
import * as confirm from "./confirmDelete";

const settings = (autostart: boolean) => ({ autostart }) as Settings;

beforeEach(() => resetAutostartOffer());
afterEach(() => vi.restoreAllMocks());

// An Agent in compatibility mode stops working when BootAgent is not running,
// so switching one to it is when starting with the system is worth a question.
describe("offerAutostart", () => {
  it("turns on starting with the system when the user agrees", async () => {
    vi.spyOn(api, "getSettings").mockResolvedValue(settings(false));
    const save = vi.spyOn(api, "saveSettings").mockResolvedValue(settings(true));
    const ask = vi.spyOn(confirm, "confirmAction").mockResolvedValue(true);
    await offerAutostart(sourceTranslate, vi.fn());
    expect(ask).toHaveBeenCalledWith(expect.objectContaining({ message: "兼容模式需要 BootAgent 保持运行。要让 BootAgent 随系统启动吗？" }));
    expect(save).toHaveBeenCalledWith({ autostart: true });
  });

  it("does not ask when BootAgent already starts with the system", async () => {
    vi.spyOn(api, "getSettings").mockResolvedValue(settings(true));
    const ask = vi.spyOn(confirm, "confirmAction");
    await offerAutostart(sourceTranslate, vi.fn());
    expect(ask).not.toHaveBeenCalled();
  });

  it("asks once, and leaves the setting alone after a no", async () => {
    vi.spyOn(api, "getSettings").mockResolvedValue(settings(false));
    const save = vi.spyOn(api, "saveSettings");
    const ask = vi.spyOn(confirm, "confirmAction").mockResolvedValue(false);
    await offerAutostart(sourceTranslate, vi.fn());
    await offerAutostart(sourceTranslate, vi.fn());
    expect(ask).toHaveBeenCalledTimes(1);
    expect(save).not.toHaveBeenCalled();
  });

  it("reports a setting that could not be saved", async () => {
    const failure = new Error("autostart is not supported on this platform");
    vi.spyOn(api, "getSettings").mockResolvedValue(settings(false));
    vi.spyOn(api, "saveSettings").mockRejectedValue(failure);
    vi.spyOn(confirm, "confirmAction").mockResolvedValue(true);
    const report = vi.fn();
    await offerAutostart(sourceTranslate, report);
    expect(report).toHaveBeenCalledWith(failure);
  });
});
