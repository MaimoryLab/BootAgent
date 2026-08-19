import { useLayoutEffect, useRef, type PropsWithChildren } from "react";

/**
 * A modal that renders on the browser's top layer.
 *
 * The `open` attribute alone does not do this. It only makes a <dialog>
 * visible, and a visible non-modal dialog stays in normal document flow at its
 * static position: no ::backdrop, no focus containment, no Esc. Every modal
 * here is authored inside the row or page that owns its state, so under
 * `open` the launch dialog rendered wherever its Agent row happened to sit --
 * far down a long list, adding its own height to the page instead of covering
 * the window. `.app-window` also sets `overflow: hidden`, which clips anything
 * that tries to escape by positioning alone.
 *
 * showModal() is what promotes the element to the top layer, above every
 * stacking context and outside the clip. It also brings the parts we were
 * otherwise hand-rolling or missing: a real ::backdrop, focus trapped inside,
 * and Esc. Nothing needs a z-index, so a modal can no longer tie with the task
 * centre's and lose the tie to DOM order.
 */
export function ModalDialog({ className, label, onDismiss, children }: PropsWithChildren<{
  className?: string;
  label?: string;
  /**
   * Called when the user asks to close: Esc, or the backdrop-cancel the
   * platform may add. Callers own the state that renders this component, so
   * they close it by unmounting rather than by mutating the element.
   */
  onDismiss?: () => void;
}>) {
  const ref = useRef<HTMLDialogElement>(null);

  // Layout, not passive: a plain effect leaves one frame in which the element is
  // in the DOM but not yet on the top layer, so it paints once at its static
  // position -- the very flash this component exists to remove -- and is not yet
  // reachable as a dialog.
  useLayoutEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    dialog.showModal();
    // Symmetric with the call above so a remount cannot open an already-open
    // dialog. React 19 double-invokes effects under StrictMode.
    return () => dialog.close();
  }, []);

  return (
    <dialog
      ref={ref}
      className={className}
      aria-label={label}
      // The state that renders this component is the single source of truth, so
      // Esc must not close the element behind React's back -- that would leave
      // a dialog the caller still believes is open, invisible and unreachable.
      onCancel={(event) => {
        event.preventDefault();
        onDismiss?.();
      }}
    >
      {children}
    </dialog>
  );
}
