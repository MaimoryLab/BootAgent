/**
 * Guards the one mechanism that puts modals above the page.
 *
 * Two ways of doing this had grown side by side. Four sites used `<dialog open>`,
 * which is visible but *not* modal: it stays in document flow at its static
 * position, so the launch dialog rendered inside its Agent row, well down a long
 * list, and pushed the page instead of covering it. Two others hand-rolled a
 * fixed overlay whose `z-index: 20` tied the task centre's 20, leaving DOM order
 * to break the tie.
 *
 * Both are now one ModalDialog calling showModal(), which is above every
 * stacking context and outside `.app-window`'s `overflow: hidden`. These are
 * text assertions over source, as transfer-dialog.test.ts is, because jsdom does
 * no layout and cannot fail on position.
 */
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// Built with node:path rather than `new URL("../", import.meta.url)`: Vite
// statically rewrites that pattern into a dev-server asset URL, which is not a
// file: URL and cannot be read from disk.
const root = dirname(dirname(fileURLToPath(import.meta.url)));

function sources(): { path: string; text: string }[] {
  const found: { path: string; text: string }[] = [];
  const walk = (dir: string, prefix: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        walk(join(dir, entry.name), `${prefix}${entry.name}/`);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name) || entry.name.includes(".test.")) continue;
      found.push({ path: `${prefix}${entry.name}`, text: readFileSync(join(dir, entry.name), "utf8") });
    }
  };
  walk(root, "");
  return found;
}

describe("modal layering", () => {
  it("renders a <dialog> only through ModalDialog", () => {
    const offenders = sources().filter((file) => file.path !== "components/ModalDialog.tsx" && /<dialog[\s>]/.test(file.text));
    expect(offenders.map((file) => file.path)).toEqual([]);
  });

  it("opens with showModal, never with the open attribute", () => {
    const component = sources().find((file) => file.path === "components/ModalDialog.tsx");
    expect(component).toBeTruthy();
    expect(component?.text).toContain("showModal()");
    // `open` as a JSX attribute is the defect; the `dialog.open` property and
    // the word in prose are not.
    expect(/<dialog[^>]*\sopen[\s/>]/.test(component?.text ?? "")).toBe(false);
  });

  it("keeps no competing fixed overlay for modals", () => {
    const css = readFileSync(join(root, "styles/app.css"), "utf8");
    expect(css).not.toContain("mcp-modal-backdrop");
    // The backdrop comes from the top layer now, for both modal skins.
    expect(css).toContain(".mcp-modal::backdrop");
    expect(css).toContain(".transfer-password-dialog::backdrop");
  });
});
