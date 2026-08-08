import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// No I18nProvider, so translate() returns the Chinese keys unchanged.
import { RuntimePrompt } from "./RuntimePrompt";
import type { RuntimeStatus } from "../types/api";

vi.mock("../state/TaskCenterContext", async (importOriginal) => ({
  ...await importOriginal<typeof import("../state/TaskCenterContext")>(),
  useTaskCenter: () => ({
    startTask: vi.fn(() => true), finishTask: vi.fn(), setTaskCanceller: vi.fn(),
    // DownloadProgress renders inside this component and reads both.
    progress: {}, running: false,
    // A failed download for the runtime below.
    taskFor: (id: string) => id === "download:node"
      ? { state: "failure", message: "无法连接到模型服务" } : undefined,
  }),
  useTaskRoute: () => "/setup/agents",
}));

const node = { id: "node", name: "Node.js", installed: false, supported: true, lockedVersion: "24.16.0" } as unknown as RuntimeStatus;

describe("RuntimePrompt download failure", () => {
  it("says the mirror was already tried", () => {
    render(<RuntimePrompt runtimes={[node]} missingRuntime={{ codex: "node" }} selectedAgentIds={["codex"]} agents={{}} onInstalled={vi.fn()} />);
    expect(screen.getByText(/都已尝试过/)).toBeTruthy();
    // notice notice-error, matching RuntimeSection rather than the bare span it was.
    expect(document.querySelector(".notice-error")).toBeTruthy();
  });
});
