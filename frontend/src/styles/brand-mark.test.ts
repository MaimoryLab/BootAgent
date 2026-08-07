/**
 * Guards the product mark in the sidebar lockup.
 *
 * Two things here are load-bearing and neither is visible to jsdom, which does
 * not resolve var():
 *
 *   - The mark paints var(--blue), not a literal. #007AFF matches --blue exactly
 *     on the light theme, which is why a hardcoded fill looks correct until you
 *     switch to dark, where --blue becomes #0a84ff.
 *   - The tagline rule stays scoped to the tagline. It was written as
 *     `.brand-lockup span:last-child`, which matched any last-child span in the
 *     lockup -- including the one wrapping the mark -- and painted it
 *     --text-secondary. That stayed hidden only while the mark was a Lucide
 *     <svg> rather than a <span>, so the next markup change would reintroduce it.
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

describe("sidebar product mark", () => {
  it("takes its colour from the theme token rather than a literal", () => {
    const rules = declarations(sheet("app.css"), ".brand-mark-glyph");
    expect(rules.get("color")).toBe("var(--blue)");
  });

  it("scopes the tagline colour so it cannot reach the mark", () => {
    const app = sheet("app.css");
    // The child combinator is the whole point: the mark's span is a direct child
    // of .brand-lockup, the tagline is nested one level deeper.
    const scoped = declarations(app, ".brand-lockup > div > span:last-child");
    expect(scoped.get("color")).toBe("var(--text-secondary)");
    expect(app).not.toMatch(/\.brand-lockup\s+span:last-child\s*\{/);
  });

  it("sizes the glyph from the element box so the svg cannot overflow the plate", () => {
    const rules = declarations(sheet("app.css"), ".brand-mark-glyph > svg");
    expect(rules.get("width")).toBe("100%");
    expect(rules.get("height")).toBe("100%");
  });
});
