import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentCatalogItem, AgentStatus } from "../types/api";
import { AgentManageRow, compareVersions, isBehind } from "./AgentManageRow";

const catalogAgent: AgentCatalogItem = {
  id: "codex",
  name: "Codex",
  group: "auto",
  configMode: "auto",
  guideOnly: false,
  lockedVersion: "0.145.0",
  protocol: "responses",
  platforms: ["macos", "linux", "windows"],
  platformNote: "",
};

function agentStatus(over: Partial<AgentStatus> = {}): AgentStatus {
  return {
    installed: true,
    configured: true,
    guideOnly: false,
    config: "/home/u/.codex/config.toml",
    version: "0.145.0",
    lockedVersion: "0.145.0",
    canInstall: true,
    provider: "ppio",
    model: "deepseek/deepseek-v3",
    baseUrl: "https://api.ppio.com/openai",
    updatedAt: "2026-07-27T00:00:00Z",
    ...over,
  };
}

function renderRow(over: Partial<AgentStatus> = {}, onOpen = vi.fn()) {
  render(
    <AgentManageRow
      agentId="codex"
      catalog={catalogAgent}
      status={agentStatus(over)}
      providers={{
        ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" },
      }}
      onOpen={onOpen}
    />,
  );
  return onOpen;
}

describe("AgentManageRow", () => {
  it("shows what the Agent is pointed at", () => {
    renderRow();
    expect(screen.getByText("Codex")).toBeTruthy();
    expect(screen.getByText(/PPIO/)).toBeTruthy();
    expect(screen.getByText(/deepseek\/deepseek-v3/)).toBeTruthy();
  });

  it("distinguishes an unconfigured Agent from a configured one", () => {
    renderRow({ configured: false, provider: null, model: null, baseUrl: null });
    expect(screen.getByText("未配置")).toBeTruthy();
  });

  it("flags a version behind the locked one", () => {
    renderRow({ version: "0.144.4" });
    expect(screen.getByText("0.144.4 → 0.145.0")).toBeTruthy();
  });

  it("states a current version without editorialising", () => {
    renderRow();
    expect(screen.getByText("0.145.0")).toBeTruthy();
    expect(screen.queryByText(/最新/)).toBeNull();
  });

  it("does not call a newer local version an update", () => {
    // Caught on a real machine: Claude Code was 2.1.220 against a locked
    // 2.1.217 and the row read "可更新", inviting a downgrade.
    renderRow({ version: "2.1.220", lockedVersion: "2.1.217" });
    expect(screen.queryByText(/→/)).toBeNull();
    expect(screen.getByText(/锁定 2\.1\.217/)).toBeTruthy();
  });

  it("orders versions numerically, not as strings", () => {
    expect(compareVersions("0.9.0", "0.10.0")).toBe(-1);
    expect(compareVersions("1.2.3", "1.2.3")).toBe(0);
    expect(compareVersions("2.0.0", "1.9.9")).toBe(1);
    expect(isBehind("0.144.4", "0.145.0")).toBe(true);
    expect(isBehind("2.1.220", "2.1.217")).toBe(false);
  });

  it("delegates configuration instead of editing in place", () => {
    // The form moved to /agents/:agentId. Keeping it here grew the list by a
    // whole form's height and let several rows sit half-configured at once.
    const onOpen = renderRow();
    expect(screen.queryByLabelText(/API Key/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /测试连接/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Codex/ }));
    expect(onOpen).toHaveBeenCalled();
  });
});
