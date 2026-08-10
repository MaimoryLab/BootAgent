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
 * Browser previews have no native dialog host, so their unanswered request gets
 * a timed window.confirm fallback. A real WebView must not be timed: the user may
 * reasonably spend more than 1.5 seconds reading an irreversible-delete warning.
 */
const NATIVE_DIALOG_TIMEOUT_MS = 1500;

function hasNativeWebView(): boolean {
  const host = window as typeof window & {
    chrome?: { webview?: { postMessage?: unknown } };
    webkit?: { messageHandlers?: { external?: { postMessage?: unknown } } };
    wails?: { invoke?: unknown };
  };
  return Boolean(host.chrome?.webview?.postMessage || host.webkit?.messageHandlers?.external?.postMessage || host.wails?.invoke);
}

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
  const outcome = await (hasNativeWebView()
    ? pending
    : Promise.race([pending, new Promise<typeof unanswered>((resolve) => setTimeout(() => resolve(unanswered), NATIVE_DIALOG_TIMEOUT_MS))]));
  if (outcome.answered) return outcome.approved;
  // window.confirm is absent in some hardened webviews. Where neither prompt can
  // be shown, "cannot ask" has to mean "do not delete".
  if (typeof window.confirm !== "function") return false;
  return window.confirm(options.message);
}
