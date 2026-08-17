import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { ManagementSearch } from "./ManagementSearch";

function Harness() {
  const [value, setValue] = useState("");
  return <ManagementSearch value={value} onValueChange={setValue} placeholder="搜索 Skills" />;
}

describe("ManagementSearch", () => {
  it("is a labelled search landmark", () => {
    render(<Harness />);
    expect(screen.getByRole("search")).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "搜索 Skills" })).toBeTruthy();
    // No clear affordance until there is something to clear.
    expect(screen.queryByRole("button", { name: "清空搜索" })).toBeNull();
  });

  it("clears via the button and via Escape", async () => {
    render(<Harness />);
    const input = screen.getByRole("textbox", { name: "搜索 Skills" });
    await userEvent.type(input, "codegraph");
    expect(input).toHaveValue("codegraph");
    await userEvent.click(screen.getByRole("button", { name: "清空搜索" }));
    expect(input).toHaveValue("");
    await userEvent.type(input, "mcp{Escape}");
    expect(input).toHaveValue("");
  });
});
