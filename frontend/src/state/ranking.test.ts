import { describe, expect, it } from "vitest";

import type { AgentCatalogItem } from "../types/api";
import { PRIMARY_RANK_LIMIT, byProfileCreatedAt, byProviderCreatedAt, byRank, preferProviderWithKey, splitByRank } from "./ranking";

function item(id: string, rank: number): AgentCatalogItem {
  return {
    id,
    name: id,
    group: "auto",
    configMode: "auto", selectsModel: true, webApp: false,
    guideOnly: false,
    lockedVersion: null,
    protocol: null,
    platforms: ["macos"],
    platformNote: "",
    rank,
  };
}

describe("ranking", () => {
  it("sorts by rank regardless of the order the server sent", () => {
    // The response type does not promise an order, so the layout must not
    // inherit one. Both pages sort locally for this reason.
    const sorted = byRank([item("aider", 9), item("codex", 1), item("cursor", 3)]);
    expect(sorted.map((entry) => entry.id)).toEqual(["codex", "cursor", "aider"]);
  });

  it("breaks ties by id so the order is stable", () => {
    const sorted = byRank([item("beta", 5), item("alpha", 5)]);
    expect(sorted.map((entry) => entry.id)).toEqual(["alpha", "beta"]);
  });

  it("treats a missing catalog as empty rather than throwing", () => {
    // Both pages call this while the first status request is still in flight.
    expect(byRank(undefined)).toEqual([]);
    expect(splitByRank(undefined)).toEqual({ primary: [], secondary: [] });
  });

  it("splits at the limit inclusively", () => {
    const { primary, secondary } = splitByRank([
      item("at-limit", PRIMARY_RANK_LIMIT),
      item("past-limit", PRIMARY_RANK_LIMIT + 1),
    ]);
    expect(primary.map((entry) => entry.id)).toEqual(["at-limit"]);
    expect(secondary.map((entry) => entry.id)).toEqual(["past-limit"]);
  });

  it("puts newest custom providers first and built-ins last, in manifest order", () => {
    const sorted = byProviderCreatedAt({
      ppio: { name: "PPIO", home: "", base_url: "", order: 2 },
      older: { name: "Older", home: "", base_url: "", custom: true, created_at: "2026-01-01T00:00:00Z" },
      novita: { name: "Novita", home: "", base_url: "", order: 3 },
      newer: { name: "Newer", home: "", base_url: "", custom: true, created_at: "2026-02-01T00:00:00Z" },
      jiekou: { name: "JieKou.AI", home: "", base_url: "", order: 1 },
    });
    expect(sorted.map(([id]) => id)).toEqual(["newer", "older", "jiekou", "ppio", "novita"]);
  });

  it("orders built-ins by `order`, not by name", () => {
    // The fall-through used to be `name`, which produced DeepSeek, Moonshot,
    // Novita, PPIO -- alphabetical order reading as a recommendation, and it
    // decided which Provider a protocol matched first. Every name here sorts
    // against the intended sequence.
    const sorted = byProviderCreatedAt({
      alpha: { name: "Alpha", home: "", base_url: "", order: 3 },
      beta: { name: "Beta", home: "", base_url: "", order: 1 },
      gamma: { name: "Gamma", home: "", base_url: "", order: 2 },
    });
    expect(sorted.map(([id]) => id)).toEqual(["beta", "gamma", "alpha"]);
  });

  it("keeps built-in order when one of them has a saved key", () => {
    // Store.Public stamps created_at onto any Provider with a saved key, built-in
    // ones included. Comparing it before `order` let saving a key move a built-in
    // to the head of the list, and only on machines where someone had done so.
    const sorted = byProviderCreatedAt({
      ppio: { name: "PPIO", home: "", base_url: "", order: 2, has_key: true, created_at: "2026-05-01T00:00:00Z" },
      jiekou: { name: "JieKou.AI", home: "", base_url: "", order: 1 },
    });
    expect(sorted.map(([id]) => id)).toEqual(["jiekou", "ppio"]);
  });

  it("treats any Provider without `custom` as built-in, whatever its id", () => {
    // The built-in check used to be a hardcoded Set(["ppio", "novita"]). Adding
    // deepseek to providers.lock.json then sorted it as user-defined, so it
    // jumped ahead of PPIO and became the first match for a protocol -- which
    // broke the Profile editor's model list. Nothing here may name an id: the
    // absence of `custom` is what makes a Provider built-in.
    const sorted = byProviderCreatedAt({
      deepseek: { name: "DeepSeek", home: "", base_url: "", order: 4 },
      mine: { name: "Mine", home: "", base_url: "", custom: true, created_at: "2026-03-01T00:00:00Z" },
      ppio: { name: "PPIO", home: "", base_url: "", order: 2 },
    });
    expect(sorted.map(([id]) => id)).toEqual(["mine", "ppio", "deepseek"]);
  });

  // Only the E2E run caught this: ordering built-ins by the manifest is right for
  // display, but pre-selection had been riding on the created_at sort, because
  // Store.Public stamps created_at onto whichever Provider holds a key. Losing it
  // left the wizard on a keyless Provider whose connection test can never enable.
  it("prefers a Provider holding a key over an earlier one without", () => {
    const ranked = byProviderCreatedAt({
      jiekou: { name: "JieKou.AI", home: "", base_url: "", order: 1 },
      ppio: { name: "PPIO", home: "", base_url: "", order: 2, has_key: true },
    });
    expect(ranked.map(([id]) => id)).toEqual(["jiekou", "ppio"]);
    expect(preferProviderWithKey(ranked)?.[0]).toBe("ppio");
  });

  it("falls back to the first candidate when none holds a key", () => {
    // A fresh machine: nothing is configured yet, so the manifest's first
    // Provider is the one to land on.
    const ranked = byProviderCreatedAt({
      ppio: { name: "PPIO", home: "", base_url: "", order: 2 },
      jiekou: { name: "JieKou.AI", home: "", base_url: "", order: 1 },
    });
    expect(preferProviderWithKey(ranked)?.[0]).toBe("jiekou");
  });

  it("reports nothing to pre-select when there are no candidates", () => {
    expect(preferProviderWithKey([])).toBeUndefined();
  });

  it("puts newest profiles first", () => {
    const sorted = byProfileCreatedAt([
      { id: "old", label: "Old", provider: "ppio", baseUrl: null, model: "m", protocol: "openai", activatedAt: null, createdAt: "2026-01-01T00:00:00Z" },
      { id: "new", label: "New", provider: "ppio", baseUrl: null, model: "m", protocol: "openai", activatedAt: null, createdAt: "2026-02-01T00:00:00Z" },
    ]);
    expect(sorted.map((profile) => profile.id)).toEqual(["new", "old"]);
  });
});
