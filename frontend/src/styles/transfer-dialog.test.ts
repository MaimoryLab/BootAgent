/**
 * Guards the export dialog's action row.
 *
 * The dialog is capped at 360px and pads 20px a side, leaving 320px of content.
 * Its footer now holds four actions -- cancel, plain text, encrypted, and the
 * default "exclude keys" -- which measure about 388px plus three 8px gaps. On one
 * line the last button runs past the dialog's right edge, and the primary action
 * is the one that goes missing.
 *
 * jsdom does not lay this out and the headless preview reports a 0-width
 * viewport, so neither can catch it; this reads the stylesheet as text, as
 * input-surface.test.ts does.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

function sheet(name: string): string {
  const css = readFileSync(fileURLToPath(new URL(name, import.meta.url)), "utf8");
  return css.replace(/\/\*[\s\S]*?\*\//g, " ");
}

function declarations(css: string, selector: string): Map<string, string> {
  const want = selector.replace(/\s+/g, " ").trim();
  const found = new Map<string, string>();
  let seen = false;
  for (const block of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (block[1].replace(/\s+/g, " ").trim() !== want) continue;
    seen = true;
    found.clear();
    for (const part of block[2].split(";")) {
      const split = part.indexOf(":");
      if (split < 0) continue;
      found.set(part.slice(0, split).trim(), part.slice(split + 1).trim().replace(/\s+/g, " "));
    }
  }
  expect(seen, `${selector} should exist`).toBe(true);
  return found;
}

describe("transfer dialog footer", () => {
  it("wraps so four actions cannot overflow the dialog", () => {
    const rules = declarations(sheet("./app.css"), ".transfer-password-dialog footer");
    expect(rules.get("display")).toBe("flex");
    expect(rules.get("flex-wrap")).toBe("wrap");
    // Right-aligned on whichever row it lands on, so the default action stays
    // where a user looks for it.
    expect(rules.get("justify-content")).toBe("flex-end");
  });
});
