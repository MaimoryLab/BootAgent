import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

function renderRow(over: Partial<AgentStatus> = {}, onActivated = vi.fn()) {
  render(
    <AgentManageRow
      agentId="codex"
      catalog={catalogAgent}
      status={agentStatus(over)}
      providers={{
        ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" },
      }}
      onActivated={onActivated}
    />,
  );
  return onActivated;
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
    // The drift is what makes this a long-lived tool rather than a one-shot
    // installer, so it has to be visible without opening anything.
    expect(screen.getByText(/0\.144\.4/)).toBeTruthy();
    expect(screen.getByText(/0\.145\.0/)).toBeTruthy();
  });

  it("states a current version without editorialising", () => {
    // Five rows each saying "已是最新" is noise; being current is the norm.
    renderRow();
    expect(screen.getByText("0.145.0")).toBeTruthy();
    expect(screen.queryByText(/最新/)).toBeNull();
  });

  it("does not call a newer local version an update", () => {
    // Caught on a real machine: Claude Code was 2.1.220 against a locked
    // 2.1.217 and the row read "2.1.220 → 2.1.217 可更新", inviting a
    // downgrade. Being ahead of the lock is normal, not an update.
    renderRow({ version: "2.1.220", lockedVersion: "2.1.217" });
    expect(screen.queryByText(/→/)).toBeNull();
    expect(screen.getByText(/锁定 2\.1\.217/)).toBeTruthy();
  });

  it("orders versions numerically, not as strings", () => {
    // "0.9.0" > "0.10.0" under string comparison, which would hide a real
    // update.
    expect(compareVersions("0.9.0", "0.10.0")).toBe(-1);
    expect(compareVersions("1.2.3", "1.2.3")).toBe(0);
    expect(compareVersions("2.0.0", "1.9.9")).toBe(1);
    expect(isBehind("0.144.4", "0.145.0")).toBe(true);
    expect(isBehind("2.1.220", "2.1.217")).toBe(false);
  });

  it("keeps apply disabled until a probe succeeds", async () => {
    renderRow();
    fireEvent.click(screen.getByRole("button", { name: /改配置/ }));
    const apply = screen.getByRole("button", { name: /应用/ });
    expect(apply.hasAttribute("disabled")).toBe(true);
  });

  it("reports the restart instruction after a successful apply", async () => {
    const { api } = await import("../api/client");
    vi.spyOn(api, "probe").mockResolvedValue({
      ok: true,
      reachable: true,
      status: 200,
      message: "ok",
      error_code: null,
      retryable: false,
    });
    vi.spyOn(api, "activateAgent").mockResolvedValue({
      ok: true,
      agent: "codex",
      config: "/home/u/.codex/config.toml",
      provider: "ppio",
      model: "deepseek/deepseek-v3",
      restart: "Quit any running codex process, then start it again",
      next: "source ~/.oneagent/agents/codex.env && codex",
    });

    const onActivated = renderRow();
    fireEvent.click(screen.getByRole("button", { name: /改配置/ }));
    fireEvent.change(screen.getByLabelText(/API Key/i), { target: { value: "sk-test" } });
    fireEvent.click(screen.getByRole("button", { name: /测试连接/ }));
    await waitFor(() => expect(screen.getByRole("button", { name: /应用/ }).hasAttribute("disabled")).toBe(false));

    fireEvent.click(screen.getByRole("button", { name: /应用/ }));
    // An Agent reads its config at startup, so the switch is invisible until
    // the process restarts. Saying "done" without this reads as a failure.
    await waitFor(() => expect(screen.getByText(/Quit any running codex/)).toBeTruthy());
    expect(onActivated).toHaveBeenCalled();
  });

  it("masks the key and never persists it in the browser", () => {
    renderRow();
    fireEvent.click(screen.getByRole("button", { name: /改配置/ }));
    const field = screen.getByLabelText(/API Key/i) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "sk-secret-value" } });

    // The value lives in the input because the user must see what they typed;
    // the contract is that it reaches no storage the browser keeps.
    expect(field.type).toBe("password");
    expect(field.autocomplete).toBe("off");
    expect(JSON.stringify(localStorage)).not.toContain("sk-secret-value");
    expect(JSON.stringify(sessionStorage)).not.toContain("sk-secret-value");
    expect(document.cookie).not.toContain("sk-secret-value");
  });

  it("drops a passing verdict when the key is edited afterwards", async () => {
    const { api } = await import("../api/client");
    vi.spyOn(api, "probe").mockResolvedValue({
      ok: true,
      reachable: true,
      status: 200,
      message: "ok",
      error_code: null,
      retryable: false,
    });
    renderRow();
    fireEvent.click(screen.getByRole("button", { name: /改配置/ }));
    fireEvent.change(screen.getByLabelText(/API Key/i), { target: { value: "sk-good" } });
    fireEvent.click(screen.getByRole("button", { name: /测试连接/ }));
    await waitFor(() => expect(screen.getByRole("button", { name: /应用/ }).hasAttribute("disabled")).toBe(false));

    // Editing the key after a pass must re-disable apply, or a wrong key rides
    // in on the previous verdict.
    fireEvent.change(screen.getByLabelText(/API Key/i), { target: { value: "sk-different" } });
    expect(screen.getByRole("button", { name: /应用/ }).hasAttribute("disabled")).toBe(true);
  });
});
