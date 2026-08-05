import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import App from "./App";
import { api } from "./backend/api";
import { LOCALE_STORAGE_KEY } from "./i18n";
import type { StatusResponse } from "./types/api";

// App mounts the real I18nProvider, and jsdom's navigator.language is en-US, so
// pin the locale rather than assert against whichever one the host implies.
beforeEach(() => localStorage.setItem(LOCALE_STORAGE_KEY, "zh-CN"));

const status = {
  apiVersion: 1,
  platform: { os: "linux", arch: "x64", shell: "bash" },
  runtimes: [],
  capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
  agents: {},
  catalog: [],
  groups: [],
  providers: {},
  mirrors: [],
  paths: {},
  backups: {},
  environment: null,
  environmentError: null,
  desktopAgent: { id: "desktop-agent", name: "ChatGPT Desktop", installed: false, supported: false, version: null, source: "unknown" },
  profiles: [],
  activeProfile: null,
  firstRun: false,
} satisfies StatusResponse;

describe("landing route", () => {
  it("opens onboarding on a machine with no ~/.oneagent", async () => {
    vi.spyOn(api, "status").mockResolvedValue({ ...status, firstRun: true });
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>);
    expect(await screen.findByRole("heading", { name: "选择 Agent", level: 1 })).toBeTruthy();
  });

  it("opens the overview once OneAgent has state", async () => {
    vi.spyOn(api, "status").mockResolvedValue(status);
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>);
    expect(await screen.findByRole("heading", { name: "环境总览" })).toBeTruthy();
  });

  it("does not send a failed status read into onboarding", async () => {
    // firstRun is unknown when the status call fails. Guessing "first run" would
    // drop a configured user into setup; the overview reports the error instead.
    vi.spyOn(api, "status").mockRejectedValue(new Error("offline"));
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>);
    expect(await screen.findByRole("heading", { name: "环境总览" })).toBeTruthy();
    expect(screen.getByText("offline")).toBeTruthy();
  });
});
