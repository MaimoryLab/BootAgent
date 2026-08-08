import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { I18nProvider } from "../i18n";
import { ModelPicker } from "./ModelPicker";

const MODELS = ["gpt-4.1", "deepseek/deepseek-v4-pro", "qwen3-max"];

/** Controlled, like both real callers: a commit has to round-trip through props. */
function Harness({ initial = "", collapsible = true, models = MODELS }: { initial?: string; collapsible?: boolean; models?: string[] }) {
  const [value, setValue] = useState(initial);
  return (
    <I18nProvider>
      <ModelPicker models={models} value={value} onChange={setValue} collapsible={collapsible} inputLabel="Model" />
      <button type="button">outside</button>
    </I18nProvider>
  );
}

const field = () => screen.getByLabelText("Model");
const arrow = () => screen.getByRole("button", { name: /model list/i });

describe("ModelPicker", () => {
  it("offers every model when the field is prefilled with one that was not discovered", async () => {
    // The bug this component was rewritten for. The Profile editor prefills the
    // Provider's default_model, and value doubled as the filter, so a default
    // absent from the discovered list left "no matching models" sitting directly
    // under a notice reporting how many had been found -- with no way to reach
    // any of them.
    render(<Harness initial="provider-default-model" />);
    await userEvent.click(arrow());
    for (const model of MODELS) expect(screen.getByRole("radio", { name: new RegExp(model) })).toBeTruthy();
    expect(screen.queryByText("No matching models")).toBeNull();
  });

  it("commits the clicked model and closes", async () => {
    render(<Harness initial="provider-default-model" />);
    await userEvent.click(arrow());
    await userEvent.click(screen.getByRole("radio", { name: /qwen3-max/ }));
    expect(field()).toHaveValue("qwen3-max");
    expect(screen.queryByRole("radiogroup")).toBeNull();
  });

  it("keeps the list closed until asked", async () => {
    // An always-open list is what pushed the editor's Save button off screen.
    render(<Harness />);
    expect(screen.queryByRole("radiogroup")).toBeNull();
    await userEvent.click(arrow());
    expect(screen.getByRole("radiogroup")).toBeTruthy();
  });

  it("filters as the user types and still reports the typed value", async () => {
    render(<Harness />);
    await userEvent.type(field(), "deep");
    expect(screen.getByRole("radio", { name: /deepseek-v4-pro/ })).toBeTruthy();
    expect(screen.queryByRole("radio", { name: /qwen3-max/ })).toBeNull();
    // Typing is also how a model absent from the list is entered, so the value
    // has to track the keystrokes rather than only a click.
    expect(field()).toHaveValue("deep");
  });

  it("shows the whole list again after a filtered session", async () => {
    // The query is cleared on commit and on reopening; keeping it would restore
    // exactly the stale-filter behaviour above.
    render(<Harness />);
    await userEvent.type(field(), "deep");
    await userEvent.click(screen.getByRole("radio", { name: /deepseek-v4-pro/ }));
    await userEvent.click(arrow());
    for (const model of MODELS) expect(screen.getByRole("radio", { name: new RegExp(model) })).toBeTruthy();
  });

  it("closes on Escape and on a click outside", async () => {
    render(<Harness />);
    await userEvent.click(arrow());
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("radiogroup")).toBeNull();
    await userEvent.click(arrow());
    await userEvent.click(screen.getByRole("button", { name: "outside" }));
    expect(screen.queryByRole("radiogroup")).toBeNull();
  });

  it("opens from the keyboard", async () => {
    render(<Harness />);
    field().focus();
    await userEvent.keyboard("{ArrowDown}");
    expect(screen.getByRole("radiogroup")).toBeTruthy();
  });

  it("leaves the wizard's list open and unarrowed", async () => {
    // The model step is a whole page about choosing one; collapsing it there
    // would hide the list behind a control the page does not need.
    render(<Harness collapsible={false} />);
    expect(screen.getByRole("radiogroup")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /model list/i })).toBeNull();
  });

  it("falls back to a plain input when discovery returned nothing", async () => {
    render(<Harness models={[]} />);
    expect(screen.queryByRole("radiogroup")).toBeNull();
    expect(screen.queryByRole("button", { name: /model list/i })).toBeNull();
    await userEvent.type(field(), "custom-model");
    expect(field()).toHaveValue("custom-model");
  });
});
