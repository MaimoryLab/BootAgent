import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ChatGPTAppActionResult, ChatGPTAppStatus } from "../types/api";
import { DesktopAppSection } from "./DesktopAppSection";

const bridge = vi.hoisted(() => ({
  installChatGPTApp: vi.fn(),
  openChatGPTApp: vi.fn(),
  openChatGPTInstaller: vi.fn(),
}));

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: bridge,
    describeError: errors.describeError,
  };
});

function app(overrides: Partial<ChatGPTAppStatus> = {}): ChatGPTAppStatus {
  return {
    id: "chatgpt-desktop",
    name: "ChatGPT Desktop",
    installed: false,
    supported: true,
    version: null,
    source: "macos-dmg",
    ...overrides,
  };
}

function action(value: ChatGPTAppStatus): ChatGPTAppActionResult {
  return { status: "installed", message: "installed", refreshNeeded: true, app: value };
}

describe("DesktopAppSection", () => {
  beforeEach(() => {
    bridge.installChatGPTApp.mockReset();
    bridge.openChatGPTApp.mockReset();
    bridge.openChatGPTInstaller.mockReset();
  });

  it("installs a missing app and refreshes status", async () => {
    const installed = app({ installed: true, version: "26.727.51351", path: "/Applications/ChatGPT.app" });
    bridge.installChatGPTApp.mockResolvedValue(action(installed));
    const onChanged = vi.fn();
    render(<DesktopAppSection app={app()} onChanged={onChanged} />);

    fireEvent.click(screen.getByRole("button", { name: "安装 ChatGPT Desktop" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(bridge.installChatGPTApp).toHaveBeenCalledWith();
    expect(screen.getByText("ChatGPT Desktop 安装完成")).toBeTruthy();
  });

  it("opens the installed app and its official installer", async () => {
    bridge.openChatGPTApp.mockResolvedValue(undefined);
    bridge.openChatGPTInstaller.mockResolvedValue(action(app({ installed: true, version: "26.727.51351" })));
    const onChanged = vi.fn();
    const view = render(<DesktopAppSection app={app({ installed: true, version: "26.727.51351" })} onChanged={onChanged} />);

    fireEvent.click(screen.getByRole("button", { name: "打开" }));
    await waitFor(() => expect(bridge.openChatGPTApp).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole("button", { name: "更新或重新安装" }));
    await waitFor(() => expect(bridge.openChatGPTInstaller).toHaveBeenCalledTimes(1));
    expect(view.container.textContent).toContain("版本 26.727.51351");
  });

  it("does not claim an app was found when inspection is unavailable", () => {
    render(<DesktopAppSection app={app({ inspectionUnavailable: "PowerShell unavailable" })} onChanged={vi.fn()} />);

    expect(screen.getByText("应用状态检测不可用")).toBeTruthy();
    expect(screen.queryByText("已检测到应用，但版本信息不可用")).toBeNull();
  });
});
