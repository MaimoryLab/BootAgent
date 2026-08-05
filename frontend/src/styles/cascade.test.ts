/**
 * Guards against same-selector rules that silently override each other.
 *
 * Three layout bugs came from this one pattern: a selector declared twice at
 * equal specificity, where the later block quietly won on source order.
 * `.agent-manage-identity` gained a stray `display: grid` that stacked the
 * Agent name under its icon; `.agent-manage-actions` lost `justify-content` so
 * buttons stopped right-aligning; and a 28px `min-height` meant as a floor for
 * `.provider-link` was applied to a selector list that also held `.button`,
 * `.disclosure-trigger` and `.agent-manage-row`, shortening all three from
 * their declared 38px, 46px and 84px.
 *
 * None of it was catchable before this test: no CI gate read CSS, so the only
 * way to notice was spotting it by eye in a running window.
 *
 * The rule enforced is narrow on purpose. Re-declaring a selector is a normal
 * authoring move -- a shared base plus a later addition of *different*
 * properties is fine, and so is a deliberate override inside a media query.
 * What is never intentional at equal specificity in the same context is two
 * different values for the same property, because the losing one reads as
 * live intent while having no effect.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

/** Load order matters: a later file overrides an earlier one at equal specificity. */
const SHEETS = ["tokens.css", "base.css", "app.css"] as const;

interface Declaration {
  sheet: string;
  line: number;
  value: string;
}

interface Rule {
  sheet: string;
  line: number;
  context: string;
  selector: string;
  declarations: Map<string, string>;
}

/** Replace comments with spaces so byte offsets, and thus line numbers, survive. */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, (block) => block.replace(/[^\n]/g, " "));
}

function parseRules(sheet: string, source: string): Rule[] {
  const css = stripComments(source);
  const rules: Rule[] = [];
  // Tracks nesting so a rule inside `@media` is not compared against one
  // outside it. `null` marks a plain rule block.
  const stack: Array<string | null> = [];
  let prelude = "";
  let preludeLine = 0;
  let line = 1;

  for (let i = 0; i < css.length; i += 1) {
    const char = css[i];
    if (char === "\n") line += 1;

    if (char === "{") {
      const head = prelude.trim().replace(/\s+/g, " ");
      if (head.startsWith("@")) {
        stack.push(head);
      } else {
        const body = css.slice(i + 1, findBlockEnd(css, i));
        const declarations = new Map<string, string>();
        for (const part of body.split(";")) {
          // A nested block's contents are handled by the outer walk; skip them
          // here so a nested selector is not mistaken for a declaration.
          if (part.includes("{") || !part.includes(":")) continue;
          const split = part.indexOf(":");
          const property = part.slice(0, split).trim();
          const value = part.slice(split + 1).trim().replace(/\s+/g, " ");
          if (property && value) declarations.set(property, value);
        }
        const context = stack.filter((entry): entry is string => entry !== null).join(" | ");
        for (const selector of head.split(",")) {
          const cleaned = selector.trim().replace(/\s+/g, " ");
          if (cleaned) {
            rules.push({ sheet, line: preludeLine, context, selector: cleaned, declarations });
          }
        }
        stack.push(null);
      }
      prelude = "";
    } else if (char === "}") {
      stack.pop();
      prelude = "";
    } else {
      if (prelude.trim() === "" && char.trim() !== "") preludeLine = line;
      prelude += char;
    }
  }
  return rules;
}

function findBlockEnd(css: string, open: number): number {
  let depth = 1;
  for (let i = open + 1; i < css.length; i += 1) {
    if (css[i] === "{") depth += 1;
    else if (css[i] === "}") {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return css.length;
}

function loadRules(): Rule[] {
  return SHEETS.flatMap((sheet) => {
    const path = fileURLToPath(new URL(sheet, import.meta.url));
    return parseRules(sheet, readFileSync(path, "utf8"));
  });
}

/**
 * Conflicts keyed by `context\0selector`, each listing the property and the
 * competing values in source order. Only the last value takes effect.
 */
function findConflicts(rules: Rule[]): string[] {
  const byKey = new Map<string, Rule[]>();
  for (const rule of rules) {
    const key = `${rule.context}\0${rule.selector}`;
    const bucket = byKey.get(key);
    if (bucket) bucket.push(rule);
    else byKey.set(key, [rule]);
  }

  const conflicts: string[] = [];
  for (const [key, group] of byKey) {
    if (group.length < 2) continue;
    const [context, selector] = key.split("\0");
    const seen = new Map<string, Declaration[]>();
    for (const rule of group) {
      for (const [property, value] of rule.declarations) {
        const hits = seen.get(property) ?? [];
        hits.push({ sheet: rule.sheet, line: rule.line, value });
        seen.set(property, hits);
      }
    }
    for (const [property, hits] of seen) {
      if (hits.length < 2) continue;
      if (new Set(hits.map((hit) => hit.value)).size < 2) continue;
      const trail = hits.map((hit) => `${hit.value} (${hit.sheet}:${hit.line})`).join(" then ");
      const winner = hits[hits.length - 1];
      conflicts.push(
        `${selector}${context ? ` inside ${context}` : ""}: ${property} is set to ${trail}` +
          ` -- only ${winner.value} applies.`,
      );
    }
  }
  return conflicts.sort();
}

describe("stylesheet cascade", () => {
  it("declares no property twice for the same selector in the same context", () => {
    expect(findConflicts(loadRules())).toEqual([]);
  });

  it("detects a conflict introduced by a later duplicate rule", () => {
    // Proves the check above can fail. Without this, a parser that silently
    // returned nothing would look like a passing gate forever.
    const rules = parseRules(
      "fixture.css",
      ".card {\n  min-height: 84px;\n}\n.button,\n.card {\n  min-height: 28px;\n}\n",
    );

    expect(findConflicts(rules)).toEqual([
      ".card: min-height is set to 84px (fixture.css:1) then 28px (fixture.css:4) -- only 28px applies.",
    ]);
  });

  it("allows a media query to override the same property", () => {
    const rules = parseRules(
      "fixture.css",
      ".row {\n  width: 1040px;\n}\n@media (max-width: 600px) {\n  .row { width: 100%; }\n}\n",
    );

    expect(findConflicts(rules)).toEqual([]);
  });

  it("reports a descendant selector conflict without mangling the name", () => {
    const rules = parseRules(
      "fixture.css",
      ".a > b svg:last-child {\n  margin-left: auto;\n}\n.a > b svg:last-child {\n  margin-left: 0;\n}\n",
    );
    expect(findConflicts(rules)).toEqual([
      ".a > b svg:last-child: margin-left is set to auto (fixture.css:1) then 0 (fixture.css:4) -- only 0 applies.",
    ]);
  });

  it("allows a later rule that adds different properties", () => {
    const rules = parseRules(
      "fixture.css",
      ".picker,\n.other {\n  display: flex;\n}\n.picker {\n  margin-top: auto;\n}\n",
    );

    expect(findConflicts(rules)).toEqual([]);
  });
});
