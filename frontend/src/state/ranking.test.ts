import { describe, expect, it } from "vitest";

import type { AgentCatalogItem } from "../types/api";
import { PRIMARY_RANK_LIMIT, byRank, splitByRank } from "./ranking";

function item(id: string, rank: number): AgentCatalogItem {
  return {
    id,
    name: id,
    group: "auto",
    configMode: "auto",
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
});
