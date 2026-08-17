import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { AgentTargetGroup } from "./AgentTargetGroup";

const AGENTS = ["claude-code", "codex", "opencode"];

describe("AgentTargetGroup", () => {
  it("renders one pressed toggle per selected agent", () => {
    render(<AgentTargetGroup agents={AGENTS} selected={["codex"]} onToggle={() => undefined} />);
    const group = screen.getByRole("group", { name: "选择目标 Agent" });
    expect(group).toBeTruthy();
    expect(screen.getAllByRole("button")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "codex" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "claude-code" }).getAttribute("aria-pressed")).toBe("false");
  });

  it("reports the inverted state on click", async () => {
    const onToggle = vi.fn();
    render(<AgentTargetGroup agents={AGENTS} selected={["codex"]} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole("button", { name: "codex" }));
    expect(onToggle).toHaveBeenCalledWith("codex", false);
    await userEvent.click(screen.getByRole("button", { name: "opencode" }));
    expect(onToggle).toHaveBeenCalledWith("opencode", true);
  });

  it("prefers catalog display names for the accessible label", () => {
    render(
      <AgentTargetGroup agents={["claude-code"]} selected={[]} onToggle={() => undefined} labels={{ "claude-code": "Claude Code" }} />,
    );
    expect(screen.getByRole("button", { name: "Claude Code" })).toBeTruthy();
  });

  it("blocks interaction when disabled", async () => {
    const onToggle = vi.fn();
    render(<AgentTargetGroup agents={AGENTS} selected={[]} onToggle={onToggle} disabled />);
    await userEvent.click(screen.getByRole("button", { name: "codex" }));
    expect(onToggle).not.toHaveBeenCalled();
  });
});
