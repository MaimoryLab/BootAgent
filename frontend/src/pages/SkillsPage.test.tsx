import { describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

import { applySkillTarget, filterSkillRows, skillSelectedAgents } from "./SkillsPage";
import type { SkillChange, SkillSummary } from "../types/api";

const row = (partial: Partial<SkillSummary>): SkillSummary => ({
  id: "skill",
  name: "Skill",
  description: "",
  variants: 1,
  variant_hashes: ["hash"],
  agents: [],
  conflict: false,
  ...partial,
});

describe("filterSkillRows", () => {
  const rows = [
    row({ id: "dataviz", name: "DataViz", description: "chart palettes" }),
    row({ id: "pdf", name: "PDF tools", description: "merge and split" }),
  ];

  it("returns everything for a blank query", () => {
    expect(filterSkillRows(rows, "  ")).toHaveLength(2);
  });

  it("matches id, name, and description case-insensitively", () => {
    expect(filterSkillRows(rows, "DATAVIZ")).toEqual([rows[0]]);
    expect(filterSkillRows(rows, "merge")).toEqual([rows[1]]);
    expect(filterSkillRows(rows, "nothing")).toEqual([]);
  });
});

describe("skillSelectedAgents", () => {
  it("starts from the scanned agents", () => {
    expect(skillSelectedAgents(row({ agents: ["codex"] }), {})).toEqual(["codex"]);
  });

  it("prefers the draft targets once a change exists", () => {
    const changes: Record<string, SkillChange> = {
      skill: { id: "skill", variant_hash: "hash", targets: ["claude-code"] },
    };
    expect(skillSelectedAgents(row({ agents: ["codex"] }), changes)).toEqual(["claude-code"]);
  });

  it("drops agents queued for removal", () => {
    const changes: Record<string, SkillChange> = {
      "skill::remove::codex": { id: "skill", variant_hash: "hash", targets: ["codex"], delete: true },
    };
    expect(skillSelectedAgents(row({ agents: ["codex", "opencode"] }), changes)).toEqual(["opencode"]);
  });
});

describe("applySkillTarget", () => {
  const target = { id: "skill", hash: "hash", scannedAgents: ["codex"] };

  it("selects a new agent starting from the scanned set", () => {
    const next = applySkillTarget({}, target, "claude-code", true);
    expect(next["skill"].targets?.sort()).toEqual(["claude-code", "codex"]);
  });

  it("records a removal entry when deselecting a scanned agent", () => {
    const next = applySkillTarget({}, target, "codex", false);
    expect(next["skill::remove::codex"]).toMatchObject({ id: "skill", delete: true, targets: ["codex"] });
  });

  it("re-selecting a removed agent clears its removal entry", () => {
    const removed = applySkillTarget({}, target, "codex", false);
    const restored = applySkillTarget(removed, target, "codex", true);
    expect(restored["skill::remove::codex"]).toBeUndefined();
    expect(restored["skill"].targets).toContain("codex");
  });

  it("folds sequential toggles without losing earlier ones", () => {
    // The bulk toggle applies this reducer once per row inside a single
    // setState updater; two toggles on one row must both survive.
    let draft: Record<string, SkillChange> = {};
    draft = applySkillTarget(draft, target, "claude-code", true);
    draft = applySkillTarget(draft, target, "opencode", true);
    expect(draft["skill"].targets?.sort()).toEqual(["claude-code", "codex", "opencode"]);
  });
});
