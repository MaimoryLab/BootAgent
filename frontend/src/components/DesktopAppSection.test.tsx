import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { DesktopAgentActionResult, DesktopAgentStatus } from "../types/api";
import { DesktopAppSection } from "./DesktopAppSection";

const bridge = vi.hoisted(() => ({
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
    id: "desktop-agent",
    name: "Example Desktop",
    installed: false,
    supported: true,
    version: null,
    source: "macos-dmg",
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
    render(<DesktopAppSection app={app()} onChanged={onChanged} />);

    expect(screen.getByRole("heading", { name: "桌面 Agent" })).toBeTruthy();
    const configNote = screen.getByText("与 Example CLI 共用配置文件 /home/u/.example/config.toml；安装和启动不会改动配置");
    expect(configNote.closest(".desktop-app-row")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(bridge.installDesktopAgent).toHaveBeenCalledWith();
    expect(screen.getByText("Example Desktop 安装完成")).toBeTruthy();
  });

  it("delegates an uninstalled app to setup when requested", () => {
    const onSetup = vi.fn();
    render(<DesktopAppSection app={app()} onChanged={vi.fn()} onSetup={onSetup} />);

    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(onSetup).toHaveBeenCalledOnce();
    expect(bridge.installDesktopAgent).not.toHaveBeenCalled();
  });

  it("shows download progress until the install request completes", async () => {
    const installed = app({ installed: true, version: "26.727.51351", path: "/Applications/Example.app" });
    let complete!: (result: DesktopAgentActionResult) => void;
    bridge.installDesktopAgent.mockReturnValue(new Promise((resolve) => { complete = resolve; }));
    const onChanged = vi.fn();
    render(<DesktopAppSection app={app()} onChanged={onChanged} />);

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
    const view = render(<DesktopAppSection app={app({ installed: true, version: "26.727.51351" })} onChanged={onChanged} />);

    fireEvent.click(screen.getByRole("button", { name: "启动" }));
    await waitFor(() => expect(bridge.openDesktopAgent).toHaveBeenCalledTimes(1));
    expect(screen.getByText("Example Desktop 已打开")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "更新" }));
    await waitFor(() => expect(bridge.openDesktopAgentInstaller).toHaveBeenCalledTimes(1));
    expect(screen.getByText("官方安装器已启动")).toBeTruthy();
    expect(view.container.textContent).toContain("版本26.727.51351");
  });

  it("does not claim an app was found when inspection is unavailable", () => {
    render(<DesktopAppSection app={app({ inspectionUnavailable: "PowerShell unavailable" })} onChanged={vi.fn()} />);

    expect(screen.getByText("应用状态检测不可用")).toBeTruthy();
    expect(screen.queryByText("已检测到应用，但版本信息不可用")).toBeNull();
  });

  it("can omit an uninstalled app from the overview", () => {
    render(<DesktopAppSection app={app()} onChanged={vi.fn()} showUninstalled={false} />);

    expect(screen.queryByRole("heading", { name: "桌面 Agent" })).toBeNull();
  });
});
