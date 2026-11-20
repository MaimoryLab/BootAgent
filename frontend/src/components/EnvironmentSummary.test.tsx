import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { StatusResponse } from "../types/api";
import { EnvironmentSummary } from "./EnvironmentSummary";

function runtime(id: string, name: string, installed: boolean, version = "") {
  return {
    id, name, command: id, installed, version, lockedVersion: "1.0.0", managed: false, supported: true,
    note: "", license: "", licenseUrl: "", source: "", installPath: "",
  };
}

function status(overrides: Partial<StatusResponse> = {}): StatusResponse {
  return {
    platform: { os: "macos", arch: "arm64", shell: "zsh" },
    runtimes: [runtime("node", "Node.js", true, "22.1.0"), runtime("uv", "uv", false)],
    capabilities: { canInstall: {}, missingRuntime: { aider: "uv" }, supportedAgentIds: [] },
    catalog: [{ id: "codex" }, { id: "aider" }],
    agents: { codex: { installed: true }, aider: { installed: false }, workbuddy: { installed: true } },
    desktopAgents: [{ id: "workbuddy", installed: true }, { id: "zcode", installed: false }],
    ...overrides,
  } as unknown as StatusResponse;
}

function facts() {
  return [...screen.getByRole("group", { name: "本机环境" }).children].map((item) => item.textContent);
}

describe("EnvironmentSummary", () => {
  it("names the system, each runtime and how many Agents are installed", () => {
    render(<EnvironmentSummary status={status()} selectedAgentIds={[]} />);
    // Two installed, not three: workbuddy is in agents as the owner of the
    // desktop app's profile, and counting that map would count it twice.
    expect(facts()).toEqual(["系统macOS · arm64", "Node.js版本 22.1.0", "uv未安装", "已安装的 Agent2 个"]);
  });

  // The default arrival is a desktop Agent, which needs no runtime. A bare "not
  // installed" beside it would read as a fault on a machine that has none.
  it("says whether the current selection needs a missing runtime", () => {
    const { rerender } = render(<EnvironmentSummary status={status()} selectedAgentIds={["workbuddy"]} />);
    expect(facts()[2]).toBe("uv未安装，当前选择用不到");
    rerender(<EnvironmentSummary status={status()} selectedAgentIds={["aider"]} />);
    expect(facts()[2]).toBe("uv未安装，当前选择需要");
  });

  it("reports a machine with nothing installed without a zero", () => {
    render(<EnvironmentSummary status={status({ agents: {}, desktopAgents: [] })} selectedAgentIds={[]} />);
    expect(facts().at(-1)).toBe("已安装的 Agent还没有");
  });
});
