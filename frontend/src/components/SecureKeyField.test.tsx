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
});
