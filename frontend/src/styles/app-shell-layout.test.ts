/**
 * Guards the window shell against a notice above the page clipping the footer.
 *
 * AppWindow puts the legacy-migration notice inside .app-main, above the routed
 * page. .app-window is `overflow: hidden` and exactly the window's height, so
 * whatever .app-main stacks has to fit within it -- and .page-scaffold's last
 * grid row is the footer holding the page's Back and Continue buttons.
 *
 * The regression this exists for: .app-main was a plain block and .page-scaffold
 * asked for `height: 100%`. With no notice that is correct, which is why it went
 * unnoticed. With one, the page still claimed the full window height while
 * sitting 49px lower, so the footer ran past the clipped bottom edge -- measured
 * at 1181x796, 49 of its 68px were cut and the Continue button lost 35 of 38px.
 * Anyone who had ever installed the old OneAgent saw it on every page.
 *
 * The fix is a column that hands the notice its content height and the page the
 * rest, so the assertions below are about which of the two owns the leftover
 * space. jsdom resolves no layout at all, so a render test cannot catch this;
 * the stylesheet is read as text, as input-surface.test.ts does.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

/** Comments carry `:` and property names in prose, so they must go first. */
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

describe("the window shell with a notice above the page", () => {
  it("stacks .app-main as a column so the notice and the page divide the height", () => {
    // A block container would let each child size itself, and the page asks for
    // the whole window -- the sum is what overflowed.
    const rules = declarations(sheet("app.css"), ".app-main");
    expect(rules.get("display")).toBe("flex");
    expect(rules.get("flex-direction")).toBe("column");
  });

  it("gives the page what the notice leaves rather than the full window height", () => {
    const rules = declarations(sheet("app.css"), ".page-scaffold");
    // `height: 100%` is the regression itself: it measures the window, not the
    // space left over once the notice above has taken its lines.
    expect(rules.has("height")).toBe(false);
    expect(rules.get("flex")).toBe("1");
    // Without this the inner grid's content floor keeps the page from shrinking,
    // which puts the footer back below the bottom edge.
    expect(rules.get("min-height")).toBe("0");
  });

  it("keeps the footer as the page's own last row", () => {
    // The footer is a fixed-height row of .page-scaffold, so the buttons stay at
    // the bottom of the page rather than after its scrolling body.
    const rules = declarations(sheet("app.css"), ".page-scaffold");
    expect(rules.get("display")).toBe("grid");
    expect(rules.get("grid-template-rows")).toBe("auto minmax(0, 1fr) var(--footer-height)");
  });
});
