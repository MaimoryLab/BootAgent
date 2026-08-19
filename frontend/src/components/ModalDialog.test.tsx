import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { ModalDialog } from "./ModalDialog";

describe("ModalDialog", () => {
  it("opens on the top layer rather than in document flow", () => {
    // The regression this guards: `<dialog open>` is visible but not modal, so
    // it stays at its static position inside whichever row rendered it. Only
    // showModal() promotes the element to the top layer, so assert the call
    // itself -- jsdom has no layout, and the position it produces is what the
    // E2E suite checks.
    const showModal = vi.spyOn(window.HTMLDialogElement.prototype, "showModal");
    render(<ModalDialog className="c"><p>body</p></ModalDialog>);
    expect(showModal).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("dialog")).toHaveAttribute("open");
    showModal.mockRestore();
  });

  it("labels the dialog so it is not announced as an unnamed region", () => {
    render(<ModalDialog label="选择启动目录"><p>body</p></ModalDialog>);
    expect(screen.getByRole("dialog", { name: "选择启动目录" })).toBeTruthy();
  });

  it("reports Esc to the caller instead of closing behind its state", () => {
    // The caller's state decides whether the modal renders. If Esc closed the
    // element directly, that state would still say "open" while the dialog was
    // gone -- unreachable, and impossible to reopen.
    const onDismiss = vi.fn();
    render(<ModalDialog onDismiss={onDismiss}><p>body</p></ModalDialog>);
    const dialog = screen.getByRole("dialog") as HTMLDialogElement;
    dialog.requestClose();
    expect(onDismiss).toHaveBeenCalledTimes(1);
    expect(dialog).toHaveAttribute("open");
  });

  it("closes the element when the caller stops rendering it", async () => {
    function Host() {
      const [open, setOpen] = useState(true);
      return (
        <>
          <button onClick={() => setOpen(false)}>close</button>
          {open ? <ModalDialog onDismiss={() => setOpen(false)}><p>body</p></ModalDialog> : null}
        </>
      );
    }
    render(<Host />);
    expect(screen.getByRole("dialog")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
