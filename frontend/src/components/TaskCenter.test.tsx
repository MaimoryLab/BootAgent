import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { TaskCenterProvider } from "../state/TaskCenterContext";
import type { InstallOutput } from "../types/api";
import { DownloadProgress } from "./DownloadProgress";
import { TaskCenter } from "./TaskCenter";

let emit: ((output: InstallOutput) => void) | null = null;
const unsubscribe = vi.fn();

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: {
      onInstallOutput: (listener: (output: InstallOutput) => void) => {
        emit = listener;
        return unsubscribe;
      },
    },
    describeError: errors.describeError,
  };
});

function send(output: InstallOutput) {
  act(() => {
    emit?.(output);
  });
}

describe("TaskCenter", () => {
  beforeEach(() => {
    emit = null;
    unsubscribe.mockReset();
  });

  it("shows commands and their output once opened", async () => {
    const user = userEvent.setup();
    render(
      <TaskCenterProvider>
        <TaskCenter />
      </TaskCenterProvider>,
    );
    await user.click(screen.getByRole("button", { name: /任务中心/ }));
    expect(screen.getByText(/暂无任务日志/)).toBeTruthy();

    send({ kind: "command", args: ["npm", "install", "-g", "@openai/codex"] });
    // Output arrives in chunks that split mid-line; the feed must join them
    // rather than render each chunk as its own line.
    send({ kind: "output", stream: "stdout", text: "added 1 " });
    send({ kind: "output", stream: "stdout", text: "package\n" });

    const feed = screen.getByText(/npm install/);
    expect(feed.textContent).toContain("$ npm install -g @openai/codex");
    expect(feed.textContent).toContain("added 1 package");
  });

  it("keeps collecting output while the pane is closed", async () => {
    const user = userEvent.setup();
    render(
      <TaskCenterProvider>
        <TaskCenter />
      </TaskCenterProvider>,
    );
    send({ kind: "command", args: ["uv", "tool", "install", "aider-chat"] });
    // A user who opens the pane after an install started still needs the log.
    await user.click(screen.getByRole("button", { name: /任务中心/ }));
    expect(screen.getByText(/uv tool install aider-chat/)).toBeTruthy();
  });

  it("clears the feed on request", async () => {
    const user = userEvent.setup();
    render(
      <TaskCenterProvider>
        <TaskCenter />
      </TaskCenterProvider>,
    );
    await user.click(screen.getByRole("button", { name: /任务中心/ }));
    send({ kind: "output", stream: "stderr", text: "boom\n" });
    await user.click(screen.getByRole("button", { name: /清空/ }));
    expect(screen.queryByText("boom")).toBeNull();
    expect(screen.getByText(/暂无任务日志/)).toBeTruthy();
  });

  it("shows the backend's own log directory, not a Unix path", async () => {
    // The label used to hardcode "~/.oneagent/logs", which names a directory
    // that does not exist on Windows.
    const user = userEvent.setup();
    // Braces, not a bare attribute string: JSX string literals do not process
    // backslash escapes, so logDir="C:\\Users" would pass two backslashes.
    const windowsDir = "C:\\Users\\u\\.oneagent\\logs";
    render(
      <TaskCenterProvider>
        <TaskCenter logDir={windowsDir} />
      </TaskCenterProvider>,
    );
    await user.click(screen.getByRole("button", { name: /任务中心/ }));
    expect(screen.getByText(`完整日志：${windowsDir}`)).toBeTruthy();
    expect(screen.queryByText(/~\/\.oneagent/)).toBeNull();
  });

  it("drops the path from the label when the backend reported none", async () => {
    const user = userEvent.setup();
    render(
      <TaskCenterProvider>
        <TaskCenter />
      </TaskCenterProvider>,
    );
    await user.click(screen.getByRole("button", { name: /任务中心/ }));
    // "完整日志：" with nothing after it would read as a missing value.
    expect(screen.getByText("完整日志")).toBeTruthy();
  });
});

describe("DownloadProgress", () => {
  beforeEach(() => {
    emit = null;
  });

  it("can show an indeterminate bar before the first byte event", () => {
    render(
      <TaskCenterProvider>
        <DownloadProgress runtimeId="node" pending />
      </TaskCenterProvider>,
    );
    const bar = screen.getByRole("progressbar");
    expect(bar.className).toContain("is-indeterminate");
    expect(screen.getByText(/已下载 0\.0 MB/)).toBeTruthy();
  });

  it("renders nothing until the download reports bytes, then tracks the percentage", () => {
    render(
      <TaskCenterProvider>
        <DownloadProgress runtimeId="node" />
      </TaskCenterProvider>,
    );
    expect(screen.queryByRole("progressbar")).toBeNull();

    send({ kind: "progress", target: "node", received: 5 * 1024 * 1024, total: 20 * 1024 * 1024 });
    const bar = screen.getByRole("progressbar");
    expect(bar.getAttribute("aria-valuenow")).toBe("25");
    expect(screen.getByText(/5\.0 MB \/ 20\.0 MB/)).toBeTruthy();
  });

  it("ignores a progress event for another runtime", () => {
    render(
      <TaskCenterProvider>
        <DownloadProgress runtimeId="node" />
      </TaskCenterProvider>,
    );
    send({ kind: "progress", target: "uv", received: 1024, total: 2048 });
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  it("stays indeterminate when the server sent no length", () => {
    render(
      <TaskCenterProvider>
        <DownloadProgress runtimeId="node" />
      </TaskCenterProvider>,
    );
    // A percentage computed against a zero total would read as 0% forever.
    send({ kind: "progress", target: "node", received: 3 * 1024 * 1024, total: 0 });
    const bar = screen.getByRole("progressbar");
    expect(bar.getAttribute("aria-valuenow")).toBeNull();
    expect(bar.className).toContain("is-indeterminate");
    expect(screen.getByText(/已下载 3\.0 MB/)).toBeTruthy();
  });
});
