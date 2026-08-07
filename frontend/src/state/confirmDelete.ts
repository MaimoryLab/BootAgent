import { Dialogs } from "@wailsio/runtime";

/**
 * Asks the user to confirm destroying something, returning false if they decline.
 *
 * Deleting a Profile or a Provider removes a record and its API key from disk
 * with no undo, so both go through here. It is a plain function rather than a
 * React component because the two callers need an answer in the middle of an
 * async handler, not a second render pass with a pending-delete state to unwind.
 *
 * The native dialog is the same mechanism the update prompt and the export
 * password flow already use, which is why there is no in-app modal to build: it
 * is modal for free, it cannot be dismissed by a stray click on the page, and it
 * needs no styling in either theme.
 *
 * Neither button is marked IsDefault. A default button is the one Enter
 * activates, and for an irreversible delete the safe outcome of an accidental
 * keypress is "nothing happened".
 */
export async function confirmDelete(options: {
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel: string;
}): Promise<boolean> {
  let choice: string;
  try {
    choice = await Dialogs.Question({
      Title: options.title,
      Message: options.message,
      Buttons: [
        { Label: options.confirmLabel },
        { Label: options.cancelLabel, IsCancel: true },
      ],
    });
  } catch {
    // A dialog that could not be shown must not be read as approval. Returning
    // false leaves the record in place, which is the recoverable direction.
    return false;
  }
  return choice === options.confirmLabel;
}
