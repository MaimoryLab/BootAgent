import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OneAgentApiError } from "../backend/errors";
import type { RuntimeStatus } from "../types/api";
import { TaskCenterProvider } from "../state/TaskCenterContext";
import { RuntimePrompt } from "./RuntimePrompt";
import { RuntimeSection, runtimeRoot } from "./RuntimeSection";

const installRuntime = vi.fn();
const getSettings = vi.fn();
const saveSettings = vi.fn();

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: {
      onInstallOutput: () => () => {},
      installRuntime: (runtime: string) => installRuntime(runtime),
      getSettings: () => getSettings(),
      saveSettings: (settings: unknown) => saveSettings(settings),
    },
    describeError: errors.describeError,
    describeFailure: errors.describeFailure,
  };
});

function runtime(overrides: Partial<RuntimeStatus> = {}): RuntimeStatus {
  return {
    id: "node",
    name: "Node.js",
    command: "npm",
    installed: false,
    version: "",
    lockedVersion: "24.18.1",
    managed: false,
    supported: true,
    note: "",
    license: "MIT",
    licenseUrl: "https://example.test/license",
    source: "https://nodejs.org/dist/",
    installPath: "/home/user/.oneagent/runtimes/node/v24.18.1",
    requiredByHint: "Codex, Claude Code",
    ...overrides,
  };
}

describe("RuntimeSection", () => {
  beforeEach(() => {
    installRuntime.mockReset();
    getSettings.mockReset();
    saveSettings.mockReset();
    getSettings.mockResolvedValue({ schema_version: 1, prefer_mirror: false, mirror_from_region: false, backup_retention: 3 });
    saveSettings.mockImplementation(async (settings: { prefer_mirror: boolean; backup_retention: number }) => ({
      schema_version: 1,
      prefer_mirror: settings.prefer_mirror,
      mirror_from_region: false,
      backup_retention: settings.backup_retention,
    }));
  });

  it("reports installed runtimes with their version and offers no install button", () => {
    render(
      <TaskCenterProvider>
        <RuntimeSection runtimes={[runtime({ installed: true, version: "24.18.1", managed: true })]} onInstalled={vi.fn()} />
      </TaskCenterProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "运行时" }));
    expect(screen.getByText("Node.js")).toBeTruthy();
    expect(screen.getByText("版本 24.18.1")).toBeTruthy();
    expect(screen.getByText("已安装")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
  });

  it("installs a missing runtime and refreshes status afterwards", async () => {
    installRuntime.mockResolvedValue({ runtime: "node", installed: true, version: "24.18.1", pathUpdated: true, runtimes: [] });
    const onInstalled = vi.fn();
    render(
      <TaskCenterProvider>
        <RuntimeSection runtimes={[runtime()]} onInstalled={onInstalled} />
      </TaskCenterProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "运行时" }));
    expect(screen.getByText("未安装")).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    await waitFor(() => expect(onInstalled).toHaveBeenCalledTimes(1));
    expect(installRuntime).toHaveBeenCalledWith("node");
  });

  it("surfaces an install failure without refreshing status", async () => {
    installRuntime.mockImplementation(async () => {
      throw new OneAgentApiError("校验和不匹配", "AGENT_INSTALL_FAILED", true, 400);
    });
    const onInstalled = vi.fn();
    render(
      <TaskCenterProvider>
        <RuntimeSection runtimes={[runtime()]} onInstalled={onInstalled} />
      </TaskCenterProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "运行时" }));

    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    await waitFor(() => expect(screen.getByText("校验和不匹配")).toBeTruthy());
    expect(onInstalled).not.toHaveBeenCalled();
  });

  it("hides runtimes with no locked download for this platform", () => {
    render(
      <TaskCenterProvider>
        <RuntimeSection runtimes={[runtime({ supported: false })]} onInstalled={vi.fn()} />
      </TaskCenterProvider>,
    );
    expect(screen.queryByText("Node.js")).toBeNull();
  });

  it("starts collapsed for secondary overview details", async () => {
    render(
      <TaskCenterProvider>
        <RuntimeSection runtimes={[runtime()]} onInstalled={vi.fn()} />
      </TaskCenterProvider>,
    );
    const trigger = screen.getByRole("button", { name: "运行时" });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Node.js")).toBeNull();

    await userEvent.click(trigger);
    expect(screen.getByText("Node.js")).toBeTruthy();
  });
});

describe("runtimeRoot", () => {
  // The install note used to spell out "~/.oneagent/runtimes", which names a
  // path that does not exist on Windows. The directory now comes from the
  // backend's installPath, so both separator styles have to work.
  it("names the managed parent from a Windows path", () => {
    expect(runtimeRoot([runtime({ installPath: "C:\\Users\\u\\.oneagent\\runtimes\\node\\v24.18.1" })])).toBe(
      "C:\\Users\\u\\.oneagent\\runtimes",
    );
  });

  it("names the managed parent from a POSIX path", () => {
    expect(runtimeRoot([runtime()])).toBe("/home/user/.oneagent/runtimes");
  });

  it("skips a runtime with no path and uses the next one", () => {
    expect(runtimeRoot([runtime({ installPath: "" }), runtime()])).toBe("/home/user/.oneagent/runtimes");
  });

  it("does not collapse a mixed-separator path to the drive letter", () => {
    // Picking one separator for the whole string turned this into "C:", which
    // the note then rendered as the install directory.
    expect(runtimeRoot([runtime({ installPath: "C:\\Users\\u/.oneagent/runtimes/node/v1" })])).toBe(
      "C:\\Users\\u\\.oneagent\\runtimes",
    );
  });

  it("ignores a path too short to have a managed parent", () => {
    expect(runtimeRoot([runtime({ installPath: "node/v1" })])).toBe("");
  });

  it("returns nothing rather than a half path when no runtime carries one", () => {
    // The caller falls back to a sentence without a directory; returning a
    // fragment here would render "运行时会安装到 ，".
    expect(runtimeRoot([runtime({ installPath: "" })])).toBe("");
  });
});

describe("RuntimePrompt", () => {
  beforeEach(() => {
    installRuntime.mockReset();
  });

  const agents = {
    codex: { installed: false } as never,
    opencode: { installed: true } as never,
  };

  it("prompts for the runtime a selected, not-yet-installed Agent needs", async () => {
    installRuntime.mockResolvedValue({ runtime: "node", installed: true, version: "24.18.1", pathUpdated: true, runtimes: [] });
    const onInstalled = vi.fn();
    render(
      <RuntimePrompt
        runtimes={[runtime()]}
        missingRuntime={{ codex: "node" }}
        selectedAgentIds={["codex"]}
        agents={agents}
        onInstalled={onInstalled}
      />,
    );
    expect(screen.getByText("需要先安装运行时")).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "安装 Node.js 24.18.1" }));
    await waitFor(() => expect(onInstalled).toHaveBeenCalledTimes(1));
    expect(installRuntime).toHaveBeenCalledWith("node");
  });

  it("stays hidden when the only selected Agent is already installed", () => {
    render(
      <RuntimePrompt
        runtimes={[runtime()]}
        missingRuntime={{ codex: "node", opencode: "node" }}
        selectedAgentIds={["opencode"]}
        agents={agents}
        onInstalled={vi.fn()}
      />,
    );
    expect(screen.queryByText("需要先安装运行时")).toBeNull();
  });

  it("stays hidden once the runtime is present", () => {
    render(
      <RuntimePrompt
        runtimes={[runtime({ installed: true, version: "22.0.0" })]}
        missingRuntime={{ codex: "node" }}
        selectedAgentIds={["codex"]}
        agents={agents}
        onInstalled={vi.fn()}
      />,
    );
    expect(screen.queryByText("需要先安装运行时")).toBeNull();
  });
});
