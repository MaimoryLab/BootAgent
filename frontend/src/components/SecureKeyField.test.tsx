import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { SecureKeyField } from "./SecureKeyField";

describe("SecureKeyField", () => {
  it("echoes typed input without the parent re-rendering", async () => {
    // The wizard stores the key in a ref, so the parent may not re-render per
    // keystroke. The field is uncontrolled, which is what makes echo work without
    // React holding a copy of the characters.
    const onChange = vi.fn();
    render(<SecureKeyField onChange={onChange} />);
    const input = screen.getByLabelText("API Key");
    await userEvent.type(input, "sk-secret");
    expect(input).toHaveValue("sk-secret");
    expect(onChange).toHaveBeenLastCalledWith("sk-secret");
  });

  it("keeps the key out of React and out of the serialised markup", async () => {
    // A browser review found the opposite: the field mirrored the key into local
    // state, which made it a controlled input, and React writes a controlled
    // input's value through to the DOM as an attribute. The credential was then
    // in the page's markup in plain text, where anything reading the DOM sees it.
    // Restoring `value={draft}` fails this.
    render(<SecureKeyField onChange={() => {}} />);
    const input = screen.getByLabelText("API Key");
    await userEvent.type(input, "sk-must-not-be-in-markup");

    expect(input).not.toHaveAttribute("value");
    expect(document.body.innerHTML).not.toContain("sk-must-not-be-in-markup");
    // Not vacuous: the characters really are in the field.
    expect(input).toHaveValue("sk-must-not-be-in-markup");
  });

  it("hands its input to the caller so a clear can empty it", async () => {
    // Clearing used to reset the wizard's ref while the field kept its own copy,
    // so the key stayed on screen after an install and across navigation. One call
    // site worked around it by remounting the component; the other did not.
    let node: HTMLInputElement | null = null;
    render(<SecureKeyField onChange={() => {}} register={(element) => (node = element)} />);
    const input = screen.getByLabelText("API Key");
    await userEvent.type(input, "sk-clear-me");
    expect(node).toBe(input);

    // What clearApiKey does, through the registered node.
    node!.value = "";
    expect(input).toHaveValue("");
  });

  it("repopulates from the caller's copy without putting it in the markup", () => {
    // Making the field uncontrolled fixed the leak but left a worse-looking state:
    // navigating away and back showed an empty field while the wizard still held
    // the key, so the form looked unfilled and probed successfully anyway. The
    // restore is imperative, which is what keeps the characters out of the markup.
    render(<SecureKeyField initialValue="sk-restored-value" onChange={() => {}} />);
    const input = screen.getByLabelText("API Key");
    expect(input).toHaveValue("sk-restored-value");
    expect(input).not.toHaveAttribute("value");
    expect(document.body.innerHTML).not.toContain("sk-restored-value");
  });

  it("toggles visibility between password and text", async () => {
    render(<SecureKeyField onChange={() => {}} />);
    const input = screen.getByLabelText("API Key");
    expect(input).toHaveAttribute("type", "password");
    await userEvent.click(screen.getByRole("button", { name: "显示密钥" }));
    expect(input).toHaveAttribute("type", "text");
    await userEvent.click(screen.getByRole("button", { name: "隐藏密钥" }));
    expect(input).toHaveAttribute("type", "password");
  });
});
