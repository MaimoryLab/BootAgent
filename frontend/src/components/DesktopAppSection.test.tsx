import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { DesktopAgentActionResult, DesktopAgentStatus } from "../types/api";
import { TaskCenterProvider } from "../state/TaskCenterContext";
import { DesktopAppSection } from "./DesktopAppSection";

const bridge = vi.hoisted(() => ({
  onInstallOutput: () => () => {},
  installDesktopAgent: vi.fn(),
  openDesktopAgent: vi.fn(),
  openDesktopAgentInstaller: vi.fn(),
}));

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: bridge,
    describeError: errors.describeError,
  };
});

function app(overrides: Partial<DesktopAgentStatus> = {}): DesktopAgentStatus {
  return {
    id: "chatgpt-desktop",
    name: "Example Desktop",
    installed: false,
    supported: true,
    version: null,
    source: "macos-dmg",
    protocol: "responses",
    profileAgentId: "codex",
    profileId: null,
    configPath: "/home/u/.example/config.toml",
    configSharedWith: "Example CLI",
    ...overrides,
  };
}

function action(value: DesktopAgentStatus): DesktopAgentActionResult {
  return { status: "installed", message: "installed", refreshNeeded: true, app: value };
}

describe("DesktopAppSection", () => {
  beforeEach(() => {
    bridge.installDesktopAgent.mockReset();
    bridge.openDesktopAgent.mockReset();
    bridge.openDesktopAgentInstaller.mockReset();
  });

  it("installs a missing app and refreshes status", async () => {
    const installed = app({ installed: true, version: "26.727.51351", path: "/Applications/Example.app" });
    bridge.installDesktopAgent.mockResolvedValue(action(installed));
    const onChanged = vi.fn();
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app()} onChanged={onChanged} />
      </TaskCenterProvider>,
    );

    expect(screen.getByRole("heading", { name: "桌面 Agent" })).toBeTruthy();
    const configNote = screen.getByText("与 Example CLI 共用配置文件 /home/u/.example/config.toml；安装和启动不会改动配置");
    expect(configNote.closest(".desktop-app-row")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(bridge.installDesktopAgent).toHaveBeenCalledWith("chatgpt-desktop");
    expect(screen.getByText("Example Desktop 安装完成")).toBeTruthy();
  });

  it("delegates an uninstalled app to setup when requested", () => {
    const onSetup = vi.fn();
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app()} onChanged={vi.fn()} onSetup={onSetup} />
      </TaskCenterProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(onSetup).toHaveBeenCalledOnce();
    expect(bridge.installDesktopAgent).not.toHaveBeenCalled();
  });

  it("shows download progress until the install request completes", async () => {
    const installed = app({ installed: true, version: "26.727.51351", path: "/Applications/Example.app" });
    let complete!: (result: DesktopAgentActionResult) => void;
    bridge.installDesktopAgent.mockReturnValue(new Promise((resolve) => { complete = resolve; }));
    const onChanged = vi.fn();
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app()} onChanged={onChanged} />
      </TaskCenterProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(screen.getByRole("progressbar", { name: "下载进度" })).toBeTruthy();
    expect(screen.getByText(/已下载 0\.0 MB/)).toBeTruthy();

    await act(async () => { complete(action(installed)); });
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("progressbar", { name: "下载进度" })).toBeNull();
  });

  it("opens the installed app and starts its downloaded installer", async () => {
    bridge.openDesktopAgent.mockResolvedValue(undefined);
    bridge.openDesktopAgentInstaller.mockResolvedValue({
      ...action(app({ installed: true, version: "26.727.51351" })),
      status: "installer-started",
    });
    const onChanged = vi.fn();
    const view = render(
      <TaskCenterProvider>
        <DesktopAppSection app={app({ installed: true, version: "26.727.51351" })} onChanged={onChanged} />
      </TaskCenterProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "启动" }));
    await waitFor(() => expect(bridge.openDesktopAgent).toHaveBeenCalledTimes(1));
    expect(bridge.openDesktopAgent).toHaveBeenCalledWith("chatgpt-desktop");
    expect(screen.getByText("Example Desktop 已打开")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "更新" }));
    await waitFor(() => expect(bridge.openDesktopAgentInstaller).toHaveBeenCalledTimes(1));
    expect(bridge.openDesktopAgentInstaller).toHaveBeenCalledWith("chatgpt-desktop");
    expect(screen.getByText("官方安装器已启动")).toBeTruthy();
    // The version is a token now, so its heading lives in title= rather than
    // beside the value. Assert the labelled element, not the concatenation.
    expect(screen.getByTitle("版本").textContent).toBe("26.727.51351");
  });

  it("keeps the bar and reports the outcome across a navigation mid-download", async () => {
    // The download outlives this row: the user can leave the page while it runs.
    // The bar and the final verdict used to hang off local useState, so
    // unmounting dropped both -- the bar never came back and "安装完成" was
    // never shown. One provider spans the unmount here, standing in for the one
    // mounted above the router.
    const installed = app({ installed: true, version: "26.727.51351" });
    let complete!: (result: DesktopAgentActionResult) => void;
    bridge.installDesktopAgent.mockReturnValue(new Promise((resolve) => { complete = resolve; }));
    const onChanged = vi.fn();

    const view = render(
      <TaskCenterProvider>
        <DesktopAppSection app={app()} onChanged={onChanged} />
      </TaskCenterProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(screen.getByRole("progressbar", { name: "下载进度" })).toBeTruthy();

    // Leaving the page unmounts the row while the install keeps running.
    view.rerender(
      <TaskCenterProvider>
        <div>另一个页面</div>
      </TaskCenterProvider>,
    );
    expect(screen.queryByRole("progressbar", { name: "下载进度" })).toBeNull();

    // Coming back must show the bar again, without starting a second install.
    view.rerender(
      <TaskCenterProvider>
        <DesktopAppSection app={app()} onChanged={onChanged} />
      </TaskCenterProvider>,
    );
    expect(screen.getByRole("progressbar", { name: "下载进度" })).toBeTruthy();
    expect(bridge.installDesktopAgent).toHaveBeenCalledTimes(1);

    await act(async () => { complete(action(installed)); });
    // The verdict was recorded in the provider, so it survives the round trip.
    await waitFor(() => expect(screen.getByText("Example Desktop 安装完成")).toBeTruthy());
    expect(screen.queryByRole("progressbar", { name: "下载进度" })).toBeNull();
  });

  it("does not claim an app was found when inspection is unavailable", () => {
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app({ inspectionUnavailable: "PowerShell unavailable" })} onChanged={vi.fn()} />
      </TaskCenterProvider>,
    );

    expect(screen.getByText("应用状态检测不可用")).toBeTruthy();
    expect(screen.queryByText("已检测到应用，但版本信息不可用")).toBeNull();
  });

  it("can omit an uninstalled app from the overview", () => {
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app()} onChanged={vi.fn()} showUninstalled={false} />
      </TaskCenterProvider>,
    );

    expect(screen.queryByRole("heading", { name: "桌面 Agent" })).toBeNull();
  });
});
