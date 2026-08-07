/**
 * Guards the `pattern` attributes on the ID fields.
 *
 * Browsers compile `pattern` as a `v`-flag regex, and under `v` a literal `-`
 * inside a character class is a syntax error rather than a hyphen. All three ID
 * fields shipped with an unescaped `[a-z0-9-]`, so validation threw, the browser
 * swallowed the error, and the attribute accepted every value including "ACME!!".
 * Nothing failed visibly — the Go validator caught it at save time instead — which
 * is why this went unnoticed and why it needs a test rather than just a fix.
 *
 * Reading the source is deliberate: the bug is in the attribute string, so a test
 * that imported the component and inspected the DOM would pass on a value the
 * browser had already rejected.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const FIELDS = [
  { file: "ProvidersPage.tsx", label: "Provider ID" },
  { file: "ProfilesPage.tsx", label: "Profile ID" },
  { file: "AgentProfilePage.tsx", label: "Agent Profile ID" },
];

function patterns(file: string): string[] {
  const source = readFileSync(fileURLToPath(new URL(file, import.meta.url)), "utf8");
  return [...source.matchAll(/pattern="([^"]+)"/g)].map((match) => match[1]);
}

describe("ID field patterns", () => {
  it("compiles under the v flag the browser actually uses", () => {
    for (const { file, label } of FIELDS) {
      const found = patterns(file);
      expect(found.length, `${label} should declare a pattern`).toBeGreaterThan(0);
      for (const pattern of found) {
        // Anchored the way a browser anchors `pattern`, and with the same flag.
        expect(() => new RegExp(`^(?:${pattern})$`, "v"), `${label}: ${pattern}`).not.toThrow();
      }
    }
  });

  it("still rejects the values it exists to reject", () => {
    for (const { file, label } of FIELDS) {
      for (const pattern of patterns(file)) {
        const valid = new RegExp(`^(?:${pattern})$`, "v");
        expect(valid.test("acme-x"), `${label} rejected a legal ID`).toBe(true);
        expect(valid.test("ACME!!"), `${label} accepted an illegal ID`).toBe(false);
        expect(valid.test("-leading"), `${label} accepted a leading hyphen`).toBe(false);
      }
    }
  });
});
