import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { AgentSummaryBar } from "./AgentSummaryBar";

const AGENTS = ["claude-code", "codex"];

describe("AgentSummaryBar", () => {
  it("exposes tri-state counts as checkboxes", () => {
    render(<AgentSummaryBar agents={AGENTS} counts={{ "claude-code": 3, codex: 1 }} total={3} onToggleAll={() => undefined} />);
    const all = screen.getByRole("checkbox", { name: "为全部条目取消 claude-code" });
    expect(all.getAttribute("aria-checked")).toBe("true");
    expect(all.textContent).toContain("3");
    const partial = screen.getByRole("checkbox", { name: "为全部条目选择 codex" });
    expect(partial.getAttribute("aria-checked")).toBe("mixed");
  });

  it("asks to enable when not all rows are targeted, and to disable when all are", async () => {
    const onToggleAll = vi.fn();
    render(<AgentSummaryBar agents={AGENTS} counts={{ "claude-code": 2, codex: 0 }} total={2} onToggleAll={onToggleAll} />);
    await userEvent.click(screen.getByRole("checkbox", { name: "为全部条目取消 claude-code" }));
    expect(onToggleAll).toHaveBeenCalledWith("claude-code", false);
    await userEvent.click(screen.getByRole("checkbox", { name: "为全部条目选择 codex" }));
    expect(onToggleAll).toHaveBeenCalledWith("codex", true);
  });

  it("renders nothing without eligible agents", () => {
    const { container } = render(<AgentSummaryBar agents={[]} counts={{}} total={0} onToggleAll={() => undefined} />);
    expect(container.firstChild).toBeNull();
  });

  it("disables bulk toggles for an empty list", async () => {
    const onToggleAll = vi.fn();
    render(<AgentSummaryBar agents={AGENTS} counts={{}} total={0} onToggleAll={onToggleAll} />);
    await userEvent.click(screen.getByRole("checkbox", { name: "为全部条目选择 codex" }));
    expect(onToggleAll).not.toHaveBeenCalled();
  });
});
