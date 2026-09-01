/**
 * Clipboard utility.
 *
 * Prefers the Clipboard API. Wails embeds a Chromium-based WebView on macOS
 * and Windows, so navigator.clipboard is available in practice; the execCommand
 * fallback covers edge cases and future Linux GTK builds where the permission
 * model differs.
 *
 * Returns true when the write succeeded, false otherwise. Callers are
 * responsible for showing feedback — this function is silent on failure.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fall through to execCommand
    }
  }
  // execCommand fallback: create a transient textarea, select its content,
  // copy, then remove it. Only works while the document has focus.
  try {
    const el = document.createElement("textarea");
    el.value = text;
    el.style.cssText = "position:fixed;top:-9999px;left:-9999px;opacity:0";
    document.body.appendChild(el);
    el.select();
    const ok = document.execCommand("copy");
    document.body.removeChild(el);
    return ok;
  } catch {
    return false;
  }
}
