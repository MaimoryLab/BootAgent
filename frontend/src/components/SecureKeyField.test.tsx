import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { SecureKeyField } from "./SecureKeyField";

describe("SecureKeyField", () => {
  it("echoes typed input even when the parent never re-renders", async () => {
    // The wizard stores the key in a ref, so the parent may not re-render per
    // keystroke; echo must not depend on the value prop flowing back.
    const onChange = vi.fn();
    render(<SecureKeyField value="" onChange={onChange} />);
    const input = screen.getByLabelText("API Key");
    await userEvent.type(input, "sk-secret");
    expect(input).toHaveValue("sk-secret");
    expect(onChange).toHaveBeenLastCalledWith("sk-secret");
  });

  it("toggles visibility between password and text", async () => {
    render(<SecureKeyField value="" onChange={() => {}} />);
    const input = screen.getByLabelText("API Key");
    expect(input).toHaveAttribute("type", "password");
    await userEvent.click(screen.getByRole("button", { name: "显示密钥" }));
    expect(input).toHaveAttribute("type", "text");
    await userEvent.click(screen.getByRole("button", { name: "隐藏密钥" }));
    expect(input).toHaveAttribute("type", "password");
  });

  it("shows a key loaded after the field opens", () => {
    const page = render(<SecureKeyField value="" onChange={() => {}} />);
    page.rerender(<SecureKeyField value="sk-persisted" onChange={() => {}} />);
    expect(screen.getByLabelText("API Key")).toHaveValue("sk-persisted");
  });

  // The wizard passes a constant value="", so the effect on `value` cannot see a
  // reset. Without resetKey a key typed for one Provider stayed on screen after
  // switching to another, while the ref behind it had been cleared -- the field
  // displayed a secret that was no longer going to be saved.
  it("clears a typed key when the reset token changes", async () => {
    const page = render(<SecureKeyField value="" onChange={() => {}} resetKey="ppio" />);
    const input = screen.getByLabelText("API Key");
    await userEvent.type(input, "sk-for-ppio");
    expect(input).toHaveValue("sk-for-ppio");

    page.rerender(<SecureKeyField value="" onChange={() => {}} resetKey="novita" />);
    expect(screen.getByLabelText("API Key")).toHaveValue("");
  });

  it("hides a revealed key on reset", async () => {
    // Leaving it revealed would show the next Provider's field unmasked.
    const page = render(<SecureKeyField value="" onChange={() => {}} resetKey="ppio" />);
    await userEvent.type(screen.getByLabelText("API Key"), "sk-for-ppio");
    await userEvent.click(screen.getByRole("button", { name: "显示密钥" }));
    expect(screen.getByLabelText("API Key")).toHaveAttribute("type", "text");

    page.rerender(<SecureKeyField value="" onChange={() => {}} resetKey="novita" />);
    expect(screen.getByLabelText("API Key")).toHaveAttribute("type", "password");
  });

  it("keeps a key that was loaded on mount", () => {
    // The reset effect must skip its own first run: the Provider editor mounts
    // this with the saved key already in `value`, and clearing would discard it.
    render(<SecureKeyField value="sk-saved" onChange={() => {}} resetKey="ppio" />);
    expect(screen.getByLabelText("API Key")).toHaveValue("sk-saved");
  });
});
