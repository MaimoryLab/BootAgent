import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentCatalogItem, AgentStatus } from "../types/api";
import { AgentManageRow, compareVersions, isBehind, targetSummary } from "./AgentManageRow";

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
  rank: 1,
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
    detected: null,
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

describe("targetSummary", () => {
  const providers = { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" } };

  it("reports a config OneAgent never wrote instead of calling it unconfigured", () => {
    // The defect this exists for: status reported configured=true with a null
    // provider, so a live hand-written configuration rendered as "未配置".
    const summary = targetSummary(
      agentStatus({
        provider: null,
        model: null,
        baseUrl: null,
        detected: {
          baseUrl: "https://api.other-vendor.com/v1",
          model: "gpt-5-mini",
          managedByOneAgent: false,
          unreadable: null,
        },
      }),
      providers,
    );
    expect(summary.text).toContain("https://api.other-vendor.com/v1");
    expect(summary.text).toContain("gpt-5-mini");
    expect(summary.note).toContain("非 OneAgent 写入");
  });

  it("says the file changed under us when the two sources disagree", () => {
    // Drift means someone edited the config outside OneAgent; the file wins
    // because that is what the Agent will actually load.
    const summary = targetSummary(
      agentStatus({
        detected: { baseUrl: "https://moved.example/v1", model: "m", managedByOneAgent: false, unreadable: null },
      }),
      providers,
    );
    expect(summary.text).toContain("PPIO");
    expect(summary.note).toContain("https://moved.example/v1");
  });

  it("stays quiet when the record and the file agree", () => {
    const summary = targetSummary(
      agentStatus({
        detected: {
          baseUrl: "https://api.ppio.com/openai",
          model: "deepseek/deepseek-v3",
          managedByOneAgent: true,
          unreadable: null,
        },
      }),
      providers,
    );
    expect(summary.text).toBe("PPIO · deepseek/deepseek-v3");
    expect(summary.note).toBe("");
  });

  it("shows an unreadable config as such rather than guessing", () => {
    const summary = targetSummary(
      agentStatus({
        provider: null,
        model: null,
        detected: { baseUrl: "", model: "", managedByOneAgent: false, unreadable: "TOML 无法解析：第 3 行" },
      }),
      providers,
    );
    expect(summary.text).toBe("配置无法解析");
    expect(summary.note).toContain("第 3 行");
  });

  it("still says 未配置 when there is genuinely nothing", () => {
    const summary = targetSummary(
      agentStatus({ provider: null, model: null, baseUrl: null, detected: null }),
      providers,
    );
    expect(summary.text).toBe("未配置");
  });
});
