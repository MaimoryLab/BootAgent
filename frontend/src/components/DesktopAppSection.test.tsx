import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { BootAgentApiError } from "../backend/errors";
import type { DesktopAgentActionResult, DesktopAgentStatus } from "../types/api";
import { FeedbackProvider } from "../state/FeedbackContext";
import { TaskCenterProvider } from "../state/TaskCenterContext";
import { ACTION_DONE_MS } from "./ActionButton";
import { DesktopAppSection } from "./DesktopAppSection";
import { FeedbackViewport } from "./FeedbackViewport";
import { TaskCenter } from "./TaskCenter";

const bridge = vi.hoisted(() => ({
  onInstallOutput: () => () => {},
  installDesktopAgent: vi.fn(),
  openDesktopAgent: vi.fn(),
}));

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: bridge,
    describeError: errors.describeError,
    describeFailure: errors.describeFailure,
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
    // The settings file is a detail: in the row's closed fold, not its text.
    const configPath = screen.getByText("/home/u/.example/config.toml");
    expect(configPath.closest("details")?.open).toBe(false);
    expect(configPath.closest(".desktop-app-row")).toBeTruthy();
    expect(screen.getByText("和 Example CLI 用同一份设置，安装和启动不会改动它").closest("details")).toBe(configPath.closest("details"));
    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(bridge.installDesktopAgent).toHaveBeenCalledWith("chatgpt-desktop");
    // The outcome belongs to the Task Center, not to a notice inside the card
    // that pushed the rows below it down.
    expect(screen.queryByText("Example Desktop 安装完成")).toBeNull();
    expect(document.querySelector(".desktop-app-section .notice-success")).toBeNull();
  });

  it("offers conversation migration in the action menu for installed ChatGPT Desktop", () => {
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app({ installed: true })} onChanged={vi.fn()} />
      </TaskCenterProvider>,
    );

    expect(screen.queryByRole("menuitem", { name: "迁移对话" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Example Desktop 更多操作" }));
    expect(screen.getByRole("menuitem", { name: "迁移对话" })).toBeTruthy();
  });

  it("gives every installed desktop Agent a card-level refresh action", async () => {
    const onChanged = vi.fn();
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app({ id: "workbuddy", name: "WorkBuddy", installed: true })} onChanged={onChanged} />
      </TaskCenterProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "WorkBuddy 更多操作" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "刷新状态" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledOnce());
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

  it("configures a manually installed app without downloading it", () => {
    const onSetup = vi.fn();
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app({ id: "claude-desktop", name: "Claude Desktop", manualInstall: true })} onChanged={vi.fn()} onSetup={onSetup} />
      </TaskCenterProvider>,
    );

    expect(screen.getByText("配置完成后前往官网自行安装")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "配置" }));
    expect(onSetup).toHaveBeenCalledWith("claude-desktop");
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

  it("opens the installed app without offering an update", async () => {
    bridge.openDesktopAgent.mockResolvedValue(undefined);
    const onChanged = vi.fn();
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app({ installed: true, version: "26.727.51351" })} onChanged={onChanged} />
      </TaskCenterProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "启动" }));
    await waitFor(() => expect(bridge.openDesktopAgent).toHaveBeenCalledTimes(1));
    expect(bridge.openDesktopAgent).toHaveBeenCalledWith("chatgpt-desktop");
    // Confirmed on the button itself rather than in a notice above the card.
    expect(await screen.findByRole("button", { name: "已打开" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "更新" })).toBeNull();
    // The version is a token now, so its heading lives in title= rather than
    // beside the value. Assert the labelled element, not the concatenation.
    expect(screen.getByTitle("版本").textContent).toBe("26.727.51351");
  });

  it("returns the launch button to its label after confirming", async () => {
    vi.useFakeTimers();
    try {
      bridge.openDesktopAgent.mockResolvedValue(undefined);
      render(
        <TaskCenterProvider>
          <DesktopAppSection app={app({ installed: true })} onChanged={vi.fn()} />
        </TaskCenterProvider>,
      );

      const button = screen.getByRole("button", { name: "启动" });
      fireEvent.click(button);
      await act(async () => { await vi.advanceTimersByTimeAsync(0); });
      expect(button).toHaveAccessibleName("已打开");
      await act(async () => { await vi.advanceTimersByTimeAsync(ACTION_DONE_MS); });
      expect(button).toHaveAccessibleName("启动");
    } finally {
      vi.useRealTimers();
    }
  });

  it("reports an open failure as a receipt and keeps a marker that reopens it", async () => {
    bridge.openDesktopAgent.mockRejectedValue(new BootAgentApiError("Cannot open Example Desktop", "INTERNAL_ERROR", true, 500));
    const { container } = render(
      <TaskCenterProvider>
        <FeedbackProvider>
          <DesktopAppSection app={app({ installed: true })} onChanged={vi.fn()} />
          <FeedbackViewport />
        </FeedbackProvider>
      </TaskCenterProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "启动" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("无法打开 Example Desktop");
    // Nothing was written into the card.
    expect(container.querySelector(".desktop-app-section .notice-error")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "启动失败" }));
    expect(screen.getByRole("alert")).toHaveTextContent("无法打开 Example Desktop");

    // The next open that succeeds clears the marker.
    bridge.openDesktopAgent.mockResolvedValue(undefined);
    fireEvent.click(screen.getByRole("button", { name: "启动" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "启动失败" })).toBeNull());
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
        <TaskCenter />
        <DesktopAppSection app={app()} onChanged={onChanged} />
      </TaskCenterProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(screen.getByRole("progressbar", { name: "下载进度" })).toBeTruthy();

    // Leaving the page unmounts the row while the install keeps running.
    view.rerender(
      <TaskCenterProvider>
        <TaskCenter />
        <div>另一个页面</div>
      </TaskCenterProvider>,
    );
    expect(screen.queryByRole("progressbar", { name: "下载进度" })).toBeNull();

    // Coming back must show the bar again, without starting a second install.
    view.rerender(
      <TaskCenterProvider>
        <TaskCenter />
        <DesktopAppSection app={app()} onChanged={onChanged} />
      </TaskCenterProvider>,
    );
    expect(screen.getByRole("progressbar", { name: "下载进度" })).toBeTruthy();
    expect(bridge.installDesktopAgent).toHaveBeenCalledTimes(1);

    await act(async () => { complete(action(installed)); });
    // The verdict was recorded in the provider, so it survives the round trip
    // and is reported where install outcomes live: the Task Center.
    await waitFor(() => expect(screen.getByText(/Example Desktop 安装完成/)).toBeTruthy());
    expect(screen.queryByRole("progressbar", { name: "下载进度" })).toBeNull();
  });

  it("does not claim an app was found when inspection is unavailable", () => {
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app({ inspectionUnavailable: "PowerShell unavailable" })} onChanged={vi.fn()} />
      </TaskCenterProvider>,
    );

    expect(screen.getByText("暂时无法检查这个应用是否已安装。")).toBeTruthy();
    expect(screen.queryByText("读不到这个应用的版本，不影响启动和使用。")).toBeNull();
  });

  it("can omit an uninstalled app from the overview", () => {
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app()} onChanged={vi.fn()} showUninstalled={false} />
      </TaskCenterProvider>,
    );

    expect(screen.queryByRole("heading", { name: "桌面 Agent" })).toBeNull();
  });

  it("explains missing version metadata without blocking launch", async () => {
    bridge.openDesktopAgent.mockResolvedValue(undefined);
    render(
      <TaskCenterProvider>
        <DesktopAppSection app={app({ installed: true, source: "windows-store", inspectionUnavailable: "Windows registered the app, but AppX version metadata was unavailable" })} onChanged={vi.fn()} />
      </TaskCenterProvider>,
    );

    expect(screen.getByText("读不到这个应用的版本，不影响启动和使用。")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "启动" }));
    await waitFor(() => expect(bridge.openDesktopAgent).toHaveBeenCalledWith("chatgpt-desktop"));
  });

  // This component renders every desktop Agent, and it used to hardcode
  // agentId="codex" for all of them. WorkBuddy is a Tencent product, so its card
  // displayed OpenAI's mark -- a trademark problem rather than a styling one.
  it("marks each desktop Agent with its own icon, not the first one's", () => {
    const markOf = (value: DesktopAgentStatus) => {
      const { container } = render(
        <TaskCenterProvider>
          <DesktopAppSection app={value} onChanged={vi.fn()} />
        </TaskCenterProvider>,
      );
      return container.querySelector(".desktop-app-icon")!.innerHTML;
    };

    const chatgpt = markOf(app({ id: "chatgpt-desktop", name: "ChatGPT Desktop" }));
    const workbuddy = markOf(app({ id: "workbuddy", name: "WorkBuddy", profileAgentId: "workbuddy", protocol: "openai" }));

    expect(workbuddy).not.toBe(chatgpt);
    // ChatGPT Desktop is OpenAI's own app, so a licensed asset is correct there.
    expect(chatgpt).toContain('data-mark-kind="asset"');
    // WorkBuddy uses Tencent's own icon, shipped as a bitmap because no vector is
    // published. What matters here is that it is its own mark, not OpenAI's.
    expect(workbuddy).toContain('data-mark-kind="raster"');
  });
});
