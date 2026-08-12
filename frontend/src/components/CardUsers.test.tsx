import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { CardUsers } from "./CardUsers";
import { I18nProvider } from "../i18n";

function show(count: number) {
  const users = Array.from({ length: count }, (_, index) => ({ id: `a${index}`, name: `Agent ${index}` }));
  render(<I18nProvider><CardUsers users={users} /></I18nProvider>);
}

describe("CardUsers", () => {
  it("names every Agent while they fit", () => {
    show(3);
    expect(screen.getByText("Agent 0")).toBeTruthy();
    expect(screen.getByText("Agent 2")).toBeTruthy();
    expect(screen.queryByText(/^\+/)).toBeNull();
  });

  // The cap is a layout constraint, not a stylistic one: .card-users wraps, so an
  // uncapped list added a row of chips at a time and a bound card grew taller than
  // an unbound one in the same grid row. Asserting the chip count -- not just the
  // "+N" label -- is what keeps that from regressing.
  it("collapses the rest into +N once past three", () => {
    show(8);
    const chips = document.querySelectorAll(".card-user-chip");
    expect(chips.length).toBe(4);
    expect(screen.getByText("+5")).toBeTruthy();
    expect(screen.queryByText("Agent 3")).toBeNull();
  });

  it("keeps the hidden names recoverable on hover", () => {
    show(6);
    const overflow = document.querySelector(".card-user-chip.is-overflow");
    expect(overflow?.getAttribute("title")).toContain("Agent 5");
  });

  it("says so when nothing is bound", () => {
    show(0);
    expect(document.querySelector(".card-users.is-empty")).toBeTruthy();
    expect(document.querySelectorAll(".card-user-chip").length).toBe(0);
  });
});
