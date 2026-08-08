import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// No I18nProvider here, so translate() returns the Chinese keys unchanged.
import type { AgentInstallResult } from "../types/api";
import { AgentProgressRow } from "./AgentProgressRow";

const failed = (over: Partial<AgentInstallResult> = {}): AgentInstallResult => ({
  agent: "codex",
  status: "failed",
  message: "npm is required to install Codex. Install the Node.js runtime first, then retry.",
  retryable: true,
  ...over,
});

describe("AgentProgressRow", () => {
  it("offers the runtime install beside the retry when a runtime is missing", () => {
    // Without this the row was terminal: the control that installs Node lives two
    // wizard steps back, and this page never linked to it.
    render(<AgentProgressRow name="Codex" result={failed()} loading={false} onRetry={vi.fn()} onInstallRuntime={vi.fn()} runtimeName="Node.js" />);
    expect(screen.getByRole("button", { name: "安装 Node.js" })).toBeTruthy();
    expect(screen.getByRole("button", { name: /重试/ })).toBeTruthy();
  });

  it("omits the runtime install when the caller offers none", () => {
    // The caller decides: PREREQUISITE_MISSING also covers manifest problems that
    // no download fixes, so the button must not appear for every failure.
    render(<AgentProgressRow name="Codex" result={failed()} loading={false} onRetry={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /安装/ })).toBeNull();
    expect(screen.getByRole("button", { name: /重试/ })).toBeTruthy();
  });

  it("falls back to a generic label when the runtime has no name", () => {
    render(<AgentProgressRow name="Codex" result={failed()} loading={false} onInstallRuntime={vi.fn()} />);
    expect(screen.getByRole("button", { name: "安装运行时" })).toBeTruthy();
  });

  it("shows no action on a row that succeeded", () => {
    render(<AgentProgressRow name="Codex" result={{ agent: "codex", status: "configured", retryable: false }} loading={false} onInstallRuntime={vi.fn()} runtimeName="Node.js" />);
    expect(screen.queryByRole("button")).toBeNull();
  });
});
