import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import { OneAgentApiError } from "../backend/errors";
import { ThemeProvider } from "../state/ThemeContext";
import { SettingsPage } from "./SettingsPage";

const show = () =>
  render(<ThemeProvider><MemoryRouter initialEntries={["/settings"]}><Routes><Route path="/settings" element={<SettingsPage />} /><Route path="/settings/transfer" element={<h1>transfer child</h1>} /></Routes></MemoryRouter></ThemeProvider>);

describe("SettingsPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("contains appearance and language settings and opens the transfer child page", () => {
    render(<ThemeProvider><MemoryRouter initialEntries={["/settings"]}><Routes><Route path="/settings" element={<SettingsPage />} /><Route path="/settings/transfer" element={<h1>transfer child</h1>} /></Routes></MemoryRouter></ThemeProvider>);
    expect(screen.getByRole("combobox", { name: "外观" })).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "语言" })).toBeTruthy();
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
      new OneAgentApiError("Unable to open the help site", "INTERNAL_ERROR", true, 500),
    );
    show();
    fireEvent.click(screen.getByRole("button", { name: /帮助文档/ }));
    await waitFor(() => expect(screen.getByText("Unable to open the help site")).toBeTruthy());
  });
});
