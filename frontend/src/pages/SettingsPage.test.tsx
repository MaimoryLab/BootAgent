import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import { ThemeProvider } from "../state/ThemeContext";
import { SettingsPage } from "./SettingsPage";

const show = () =>
  render(<ThemeProvider><MemoryRouter initialEntries={["/settings"]}><Routes><Route path="/settings" element={<SettingsPage />} /><Route path="/settings/transfer" element={<h1>transfer child</h1>} /></Routes></MemoryRouter></ThemeProvider>);

describe("SettingsPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("contains appearance and download settings and opens the transfer child page", async () => {
    vi.spyOn(api, "getSettings").mockResolvedValue({
      schema_version: 1,
      autostart: false,
      prefer_mirror: false,
      mirror_from_region: false,
      backup_retention: 3,
    });
    render(<ThemeProvider><MemoryRouter initialEntries={["/settings"]}><Routes><Route path="/settings" element={<SettingsPage />} /><Route path="/settings/transfer" element={<h1>transfer child</h1>} /></Routes></MemoryRouter></ThemeProvider>);
    expect(screen.getByRole("combobox", { name: "外观" })).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "语言" })).toBeTruthy();
    expect(await screen.findByRole("switch")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /导入导出/ }));
    expect(screen.getByRole("heading", { name: "transfer child" })).toBeTruthy();
  });

  // Through the backend, not an <a target="_blank">: the webview has no tab to
  // open one in, so a link would navigate the app away from itself. The URL is
  // the backend's, so nothing is passed from here.
  it("opens the help site in the real browser", async () => {
    const open = vi.spyOn(api, "openHelp").mockResolvedValue();
    show();
    fireEvent.click(screen.getByRole("button", { name: /帮助文档/ }));
    await waitFor(() => expect(open).toHaveBeenCalledWith());
  });

  it("reports a browser that could not be opened", async () => {
    // Otherwise a click that does nothing looks like a dead button.
    vi.spyOn(api, "openHelp").mockRejectedValue(
      new BootAgentApiError("Unable to open the help site", "INTERNAL_ERROR", true, 500),
    );
    show();
    fireEvent.click(screen.getByRole("button", { name: /帮助文档/ }));
    await waitFor(() => expect(screen.getByText("Unable to open the help site")).toBeTruthy());
  });

  it("loads and saves per-target backup retention", async () => {
    const getSettings = vi.spyOn(api, "getSettings").mockResolvedValue({
      schema_version: 1,
      autostart: false,
      prefer_mirror: false,
      mirror_from_region: false,
      backup_retention: 3,
    });
    const saveSettings = vi.spyOn(api, "saveSettings").mockResolvedValue({
      schema_version: 1,
      autostart: false,
      prefer_mirror: false,
      mirror_from_region: false,
      backup_retention: 7,
    });
    show();
    await waitFor(() => expect(getSettings).toHaveBeenCalled());
    const input = await screen.findByRole("spinbutton", { name: "备份历史版本数" });
    expect(input).toHaveValue(3);
    await userEvent.clear(input);
    await userEvent.type(input, "7");
    await userEvent.tab();
    await waitFor(() => expect(saveSettings).toHaveBeenCalledWith({ backup_retention: 7 }));
  });

  it("saves the launch-at-login checkbox", async () => {
    vi.spyOn(api, "getSettings").mockResolvedValue({
      schema_version: 1,
      autostart: false,
      prefer_mirror: false,
      mirror_from_region: false,
      backup_retention: 3,
    });
    const saveSettings = vi.spyOn(api, "saveSettings").mockResolvedValue({
      schema_version: 1,
      autostart: true,
      prefer_mirror: false,
      mirror_from_region: false,
      backup_retention: 3,
    });
    show();
    const checkbox = await screen.findByRole("checkbox", { name: "开机自启动" });
    await userEvent.click(checkbox);
    await waitFor(() => expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ autostart: true })));
  });

  // An app running from a mounted dmg sits on a read-only volume, so the update
  // helper cannot write its backup and abandons the swap without restarting the
  // app. The backend withholds the version and reports the location instead; this
  // one line is the whole report, so it has to carry the instruction too.
  it("says to move the app when it cannot update in place", async () => {
    const check = vi.spyOn(api, "checkUpdate").mockRejectedValue(
      new BootAgentApiError(
        "BootAgent is running from a disk image. Move BootAgent to the Applications folder, then check again.",
        "UPDATE_LOCATION_BLOCKED",
        false,
        409,
      ),
    );
    show();

    fireEvent.click(screen.getByRole("button", { name: "检查更新" }));

    await waitFor(() => expect(check).toHaveBeenCalled());
    const report = await screen.findByRole("status");
    expect(report.textContent).toContain("无法在当前位置自我更新");
    expect(report.textContent).toContain("应用程序");
    // No install button: there is nothing to install until the app is moved.
    expect(screen.queryByRole("button", { name: "立即更新" })).toBeNull();
  });
});
