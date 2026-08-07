import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { ThemeProvider } from "../state/ThemeContext";
import { SettingsPage } from "./SettingsPage";

describe("SettingsPage", () => {
  it("contains appearance and language settings and opens the transfer child page", () => {
    render(<ThemeProvider><MemoryRouter initialEntries={["/settings"]}><Routes><Route path="/settings" element={<SettingsPage />} /><Route path="/settings/transfer" element={<h1>transfer child</h1>} /></Routes></MemoryRouter></ThemeProvider>);
    expect(screen.getByRole("combobox", { name: "外观" })).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "语言" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /导入导出/ }));
    expect(screen.getByRole("heading", { name: "transfer child" })).toBeTruthy();
  });
});
