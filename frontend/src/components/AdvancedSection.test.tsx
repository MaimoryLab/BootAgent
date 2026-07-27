import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AdvancedSection } from "./AdvancedSection";

function renderSection() {
  render(
    <AdvancedSection hint="包含认证字段与分模型指定。大多数情况保持默认即可。">
      <label htmlFor="x">认证字段</label>
      <input id="x" />
    </AdvancedSection>,
  );
}

describe("AdvancedSection", () => {
  it("starts collapsed so the common path stays short", () => {
    renderSection();
    expect(screen.queryByLabelText("认证字段")).toBeNull();
    expect(screen.getByRole("button", { name: /高级选项/ }).getAttribute("aria-expanded")).toBe("false");
  });

  it("says what is inside while collapsed", () => {
    // A bare triangle makes users wonder whether they missed something. Naming
    // the contents and adding "keep the defaults" answers that without opening.
    renderSection();
    expect(screen.getByText(/保持默认即可/)).toBeTruthy();
  });

  it("reveals the fields on demand and hides the hint once open", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: /高级选项/ }));
    expect(screen.getByLabelText("认证字段")).toBeTruthy();
    // The hint has served its purpose; leaving it doubles as noise above the
    // fields it was describing.
    expect(screen.queryByText(/保持默认即可/)).toBeNull();
  });

  it("can be collapsed again", () => {
    renderSection();
    const trigger = screen.getByRole("button", { name: /高级选项/ });
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(screen.queryByLabelText("认证字段")).toBeNull();
  });
});
