import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { AgentActionMenu } from "./AgentActionMenu";

function menu(label = "更多操作", first = vi.fn(), second = vi.fn()) {
  return (
    <AgentActionMenu
      label={label}
      items={[
        { id: "update", label: "更新", onSelect: first },
        { id: "uninstall", label: "卸载 Agent", onSelect: second, tone: "danger", separatorBefore: true },
      ]}
    />
  );
}

describe("AgentActionMenu", () => {
  it("opens from a labelled icon button and closes after selecting an item", async () => {
    const user = userEvent.setup();
    const update = vi.fn();
    render(menu("Codex 更多操作", update));

    const trigger = screen.getByRole("button", { name: "Codex 更多操作" });
    expect(trigger.getAttribute("aria-haspopup")).toBe("menu");
    expect(trigger.getAttribute("aria-expanded")).toBe("false");

    await user.click(trigger);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    await user.click(screen.getByRole("menuitem", { name: "更新" }));

    expect(update).toHaveBeenCalledOnce();
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
  });

  it("closes on outside click and Escape, returning focus to the trigger", async () => {
    const user = userEvent.setup();
    render(<div>{menu()}<button type="button">外部</button></div>);
    const trigger = screen.getByRole("button", { name: "更多操作" });

    await user.click(trigger);
    fireEvent.mouseDown(screen.getByRole("button", { name: "外部" }));
    expect(screen.queryByRole("menu")).toBeNull();

    await user.click(trigger);
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("supports arrow-key navigation and skips disabled items", async () => {
    const user = userEvent.setup();
    render(
      <AgentActionMenu
        label="更多操作"
        items={[
          { id: "migration", label: "迁移对话", onSelect: vi.fn() },
          { id: "update", label: "更新中", onSelect: vi.fn(), disabled: true },
          { id: "uninstall", label: "卸载 Agent", onSelect: vi.fn() },
        ]}
      />,
    );

    const trigger = screen.getByRole("button", { name: "更多操作" });
    trigger.focus();
    await user.keyboard("{ArrowDown}");
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "迁移对话" }));
    await user.keyboard("{ArrowDown}");
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "卸载 Agent" }));
    await user.keyboard("{ArrowUp}");
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "迁移对话" }));
  });

  it("keeps only one card menu open", async () => {
    const user = userEvent.setup();
    render(<div>{menu("Codex 更多操作")}{menu("Aider 更多操作")}</div>);

    await user.click(screen.getByRole("button", { name: "Codex 更多操作" }));
    expect(screen.getAllByRole("menu")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Aider 更多操作" }));
    expect(screen.getAllByRole("menu")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Codex 更多操作" }).getAttribute("aria-expanded")).toBe("false");
  });
});
