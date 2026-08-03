import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OneAgentApiError } from "../backend/errors";
import type { RuntimeStatus } from "../types/api";
import { RuntimePrompt } from "./RuntimePrompt";
import { RuntimeSection } from "./RuntimeSection";

const installRuntime = vi.fn();
const getSettings = vi.fn();
const saveSettings = vi.fn();

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: {
      installRuntime: (runtime: string) => installRuntime(runtime),
      getSettings: () => getSettings(),
      saveSettings: (settings: unknown) => saveSettings(settings),
    },
    describeError: errors.describeError,
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
    getSettings.mockResolvedValue({ schema_version: 1, prefer_mirror: false, mirror_from_region: false });
    saveSettings.mockImplementation(async (settings: { prefer_mirror: boolean }) => ({
      schema_version: 1,
      prefer_mirror: settings.prefer_mirror,
      mirror_from_region: false,
    }));
  });

  it("reports installed runtimes with their version and offers no install button", () => {
    render(<RuntimeSection runtimes={[runtime({ installed: true, version: "24.18.1", managed: true })]} onInstalled={vi.fn()} />);
    expect(screen.getByText("Node.js")).toBeTruthy();
    expect(screen.getByText("版本 24.18.1")).toBeTruthy();
    expect(screen.getByText("已安装")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
  });

  it("installs a missing runtime and refreshes status afterwards", async () => {
    installRuntime.mockResolvedValue({ runtime: "node", installed: true, version: "24.18.1", pathUpdated: true, runtimes: [] });
    const onInstalled = vi.fn();
    render(<RuntimeSection runtimes={[runtime()]} onInstalled={onInstalled} />);
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
    render(<RuntimeSection runtimes={[runtime()]} onInstalled={onInstalled} />);

    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    await waitFor(() => expect(screen.getByText("校验和不匹配")).toBeTruthy());
    expect(onInstalled).not.toHaveBeenCalled();
  });

  it("hides runtimes with no locked download for this platform", () => {
    render(<RuntimeSection runtimes={[runtime({ supported: false })]} onInstalled={vi.fn()} />);
    expect(screen.queryByText("Node.js")).toBeNull();
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
