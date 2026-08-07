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
 *
 * Falls back to window.confirm when the native dialog does not answer, which is a
 * real environment difference rather than a test affordance. Dialogs.Question
 * needs a WebView to host the dialog, and under `-tags server` — the browser
 * preview and the E2E build — there is none: the call neither resolves nor
 * rejects, it simply never settles. A rejection could be caught, but a hang
 * cannot, so the request is raced against a timer. Without this the delete button
 * silently did nothing in server mode, with no error to explain why.
 */
const NATIVE_DIALOG_TIMEOUT_MS = 1500;

export async function confirmDelete(options: {
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel: string;
}): Promise<boolean> {
  const pending = Dialogs.Question({
    Title: options.title,
    Message: options.message,
    Buttons: [
      { Label: options.confirmLabel },
      { Label: options.cancelLabel, IsCancel: true },
    ],
  }).then(
    (choice) => ({ answered: true, approved: choice === options.confirmLabel }),
    () => ({ answered: false, approved: false }),
  );
  // The sentinel is a distinct object rather than a boolean so a real answer of
  // "declined" is not confused with "never answered": one must not fall through
  // to a second prompt.
  const unanswered = { answered: false, approved: false };
  const timer = new Promise<typeof unanswered>((resolve) => setTimeout(() => resolve(unanswered), NATIVE_DIALOG_TIMEOUT_MS));
  const outcome = await Promise.race([pending, timer]);
  if (outcome.answered) return outcome.approved;
  // window.confirm is absent in some hardened webviews. Where neither prompt can
  // be shown, "cannot ask" has to mean "do not delete".
  if (typeof window.confirm !== "function") return false;
  return window.confirm(options.message);
}
