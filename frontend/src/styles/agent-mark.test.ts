/**
 * Guards the Agent marks on the environment overview.
 *
 * The overview shows the mark on a transparent box, so unlike every other place
 * a mark appears there is no chip tint and no border separating it from the
 * card -- the glyph itself is the only thing carrying contrast. That makes two
 * things load-bearing, neither of which any other gate covers:
 *
 *   - the box stays transparent, and
 *   - --agent-mark-fg is full black on light and full white on dark.
 *
 * jsdom does not resolve var(), so getComputedStyle cannot see any of this; the
 * assertions read the stylesheets as text instead. The CLI and desktop rules are
 * checked together because they must agree: a row styled one way and its
 * neighbour the other would read as two different kinds of object.
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

const OVERVIEW_MARK_BOXES = [".agent-manage-identity .agent-icon", ".desktop-app-icon"];

describe("overview Agent marks", () => {
  it("paints the mark on a transparent box in both card types", () => {
    const app = sheet("app.css");
    for (const selector of OVERVIEW_MARK_BOXES) {
      const rules = declarations(app, selector);
      expect(rules.get("background"), selector).toBe("transparent");
      expect(rules.get("color"), selector).toBe("var(--agent-mark-fg)");
      // Neutralised, not removed: dropping the border would hand its 2px to the
      // glyph, because .agent-icon sizes the 34px box as border-box.
      expect(rules.get("border") ?? rules.get("border-color"), selector).toContain("transparent");
      expect(rules.get("width"), selector).toBe("34px");
      expect(rules.get("height"), selector).toBe("34px");
    }
  });

  it("resolves the mark to black on light and white on dark", () => {
    // The dark palette is declared twice in tokens.css -- once for the media
    // query, once for .theme-dark -- and both copies must carry the override or
    // forcing a theme would fall back to the light value.
    const tokens = sheet("tokens.css");
    expect(declarations(tokens, ":root").get("--agent-mark-fg")).toBe("#000000");
    for (const selector of [":root:not(.theme-light)", ":root.theme-dark"]) {
      expect(declarations(tokens, selector).get("--agent-mark-fg"), selector).toBe("#ffffff");
    }
  });

  it("keeps the tinted chip for marks outside the overview", () => {
    // .agent-icon is shared with the onboarding Agent list, which does want a
    // bordered chip. Only the two overview rules opt out, so a change that
    // flattened the base rule instead would be caught here.
    const base = declarations(sheet("app.css"), ".agent-icon, .choice-icon, .progress-icon");
    expect(base.get("background")).toBe("var(--surface-subtle)");
    expect(base.get("color")).toBe("var(--icon-fg)");
  });
});
