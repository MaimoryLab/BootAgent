import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { SelectField } from "./SelectField";

const OPTIONS = [
  { value: "system", label: "跟随系统" },
  { value: "light", label: "浅色" },
  { value: "dark", label: "深色" },
];

/** Controlled, like every real caller, so a commit has to round-trip through props. */
function Harness({ initial = "system" }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <div>
      <SelectField label="外观" value={value} options={OPTIONS} onChange={setValue} />
      <button type="button">outside</button>
    </div>
  );
}

const trigger = () => screen.getByRole("combobox", { name: "外观" });

describe("SelectField", () => {
  it("keeps the combobox role a native select had", () => {
    // The three call sites were <select>, and their tests and the e2e suite find
    // them by role. Replacing the element must not change how it is addressed.
    render(<Harness />);
    expect(trigger()).toBeTruthy();
    expect(trigger().getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("commits a choice by pointer", async () => {
    render(<Harness />);
    await userEvent.click(trigger());
    await userEvent.click(screen.getByRole("option", { name: "深色" }));
    expect(trigger()).toHaveTextContent("深色");
    // Closing is part of committing; leaving the list open would trap the next click.
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("opens, moves and commits by keyboard", async () => {
    // The whole reason a native select is worth replacing carefully: none of this
    // is free once the OS is no longer providing it.
    render(<Harness />);
    trigger().focus();
    await userEvent.keyboard("{ArrowDown}");
    expect(screen.getByRole("listbox")).toBeTruthy();
    await userEvent.keyboard("{ArrowDown}{Enter}");
    expect(trigger()).toHaveTextContent("浅色");
  });

  it("does not change the value while arrowing through the list", async () => {
    // Committing on every keystroke would apply each option in passing, which for
    // the theme picker means the whole app flashing through palettes.
    render(<Harness />);
    trigger().focus();
    await userEvent.keyboard("{ArrowDown}{ArrowDown}{ArrowDown}");
    expect(trigger()).toHaveTextContent("跟随系统");
    await userEvent.keyboard("{Escape}");
    expect(trigger()).toHaveTextContent("跟随系统");
  });

  it("names the active option for assistive technology", async () => {
    // Focus stays on the trigger, so without aria-activedescendant a screen
    // reader would announce nothing as the user arrows down.
    render(<Harness />);
    trigger().focus();
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    const active = trigger().getAttribute("aria-activedescendant");
    expect(active).toBeTruthy();
    expect(document.getElementById(active!)).toHaveTextContent("浅色");
    expect(trigger().getAttribute("aria-expanded")).toBe("true");
  });

  it("marks the current value as selected, not merely visible", async () => {
    render(<Harness initial="dark" />);
    await userEvent.click(trigger());
    const selected = screen.getAllByRole("option").filter((option) => option.getAttribute("aria-selected") === "true");
    expect(selected).toHaveLength(1);
    expect(selected[0]).toHaveTextContent("深色");
  });

  it("closes without committing on Escape and returns focus", async () => {
    render(<Harness />);
    trigger().focus();
    await userEvent.keyboard("{ArrowDown}{ArrowDown}{Escape}");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(trigger()).toHaveTextContent("跟随系统");
    // Focus has to come back, or Escape strands keyboard users at the document.
    expect(document.activeElement).toBe(trigger());
  });

  it("closes when a click lands outside", async () => {
    render(<Harness />);
    await userEvent.click(trigger());
    await userEvent.click(screen.getByRole("button", { name: "outside" }));
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  describe("scrolling while the list is open", () => {
    /** jsdom lays nothing out, so the trigger's position is whatever this says. */
    function placeTriggerAt(top: number) {
      trigger().getBoundingClientRect = () => ({ top, bottom: top + 36, left: 0, right: 200, width: 200, height: 36, x: 0, y: top, toJSON: () => ({}) });
    }

    // The list is positioned from the trigger, so a scroll that moves the
    // trigger leaves it in the wrong place.
    it("closes when the scroll moved the trigger", async () => {
      render(<Harness />);
      placeTriggerAt(100);
      await userEvent.click(trigger());
      placeTriggerAt(40);
      fireEvent.scroll(document.body);
      expect(screen.queryByRole("listbox")).toBeNull();
    });

    // Opening the list makes the browser scroll its container to show it. That
    // scroll arrives after the list has been placed, and closing on it shut the
    // list the moment it opened on a page that was scrolled down.
    it("stays open when the scroll left the trigger where it was", async () => {
      render(<Harness />);
      placeTriggerAt(100);
      await userEvent.click(trigger());
      fireEvent.scroll(document.body);
      expect(screen.getByRole("listbox")).toBeTruthy();
    });
  });

  it("jumps to an option by typing its first letters", async () => {
    render(<Harness />);
    trigger().focus();
    await userEvent.keyboard("{ArrowDown}");
    await userEvent.keyboard("浅");
    const active = trigger().getAttribute("aria-activedescendant");
    expect(document.getElementById(active!)).toHaveTextContent("浅色");
  });

  it("renders no stale value when the current one is not in the list", () => {
    // A Provider can be deleted while its id is still the selected value; the
    // trigger should read empty rather than invent a label or crash.
    render(<SelectField label="模型服务" value="deleted-provider" options={OPTIONS} onChange={() => {}} />);
    expect(screen.getByRole("combobox", { name: "模型服务" }).textContent?.trim()).toBe("");
  });
});
