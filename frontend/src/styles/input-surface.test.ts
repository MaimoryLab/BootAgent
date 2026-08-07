/**
 * Guards the text inputs that sit inside a styled wrapper.
 *
 * .secure-field (the API Key row) and .search-field draw their own border, focus
 * ring and background, and hold the <input> as a bare text surface. That shape is
 * what makes them fragile: `.field-stack > input` -- the rule that gives every
 * other field its themed colours -- is a child selector, so the wrapper in
 * between stops it from applying. Nothing else colours these inputs, and the UA
 * sheet's default is white-on-black, so a missing declaration renders the field
 * white in dark mode while the box around it goes dark.
 *
 * Two things therefore have to hold, and neither is covered elsewhere:
 *
 *   - the wrapper carries themed color/background, and its input inherits them
 *     rather than falling back to the UA sheet, and
 *   - color-scheme tracks the forced palette, which is what colours the parts of
 *     a password field the stylesheet cannot reach: the masking dots, the caret
 *     and the autofill background.
 *
 * jsdom does not resolve var(), so getComputedStyle sees none of this; the
 * assertions read the stylesheets as text, as agent-mark.test.ts does.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

/** Comments carry `--token` names and `:` in prose, so they must go first. */
function sheet(name: string): string {
  const css = readFileSync(fileURLToPath(new URL(name, import.meta.url)), "utf8");
  return css.replace(/\/\*[\s\S]*?\*\//g, " ");
}

/**
 * The declarations of the last block whose prelude, with whitespace collapsed,
 * is exactly `selector`. Last rather than first because a later block at equal
 * specificity is what actually applies.
 */
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

const WRAPPED_FIELDS = ["secure-field", "search-field"];

describe("inputs inside a styled wrapper", () => {
  it("themes the wrapper rather than leaving it on the UA default", () => {
    const app = sheet("app.css");
    for (const field of WRAPPED_FIELDS) {
      const rules = declarations(app, `.${field}`);
      expect(rules.get("color"), field).toBe("var(--text-primary)");
      expect(rules.get("background"), field).toBe("var(--window-bg)");
    }
  });

  it("hands the wrapper's colours to the input", () => {
    // inherit/transparent rather than repeating the tokens: the wrapper is what
    // draws the field, and a second copy of the values would be one more place to
    // miss on a palette change.
    const app = sheet("app.css");
    for (const field of WRAPPED_FIELDS) {
      const rules = declarations(app, `.${field} input`);
      expect(rules.get("color"), field).toBe("inherit");
      expect(rules.get("background"), field).toBe("transparent");
    }
  });

  it("moves color-scheme with an explicitly forced palette", () => {
    // Without these two, forcing a theme leaves color-scheme following the
    // desktop, so the password dots, caret and autofill background come from the
    // opposite palette -- the API Key field reading white on a dark form.
    const tokens = sheet("tokens.css");
    expect(declarations(tokens, ":root").get("color-scheme")).toBe("light dark");
    expect(declarations(tokens, ":root.theme-dark").get("color-scheme")).toBe("dark");
    expect(declarations(tokens, ":root.theme-light").get("color-scheme")).toBe("light");
  });
});
