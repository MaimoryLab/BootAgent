import { describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

import { mapSkillhubEntry } from "./skillhub-adapter";
import { marketplaceIconCandidates, marketplaceIconUrl } from "./marketplace-icons";
import { normalizeShowcaseSkill, type ShowcaseSkill } from "./useMarketplaceCatalog";
import { validateMarketplaceCatalog } from "./marketplace-validation";
import type { MarketplaceItem } from "../types/marketplace";

const LIVE_SKILL: ShowcaseSkill = {
  slug: "self-improving-agent",
  name: "self-improving agent",
  description: "A skill where the agent logs its own findings",
  description_zh: "记录自身发现以实现自我改进的技能",
  iconUrl: "https://example.com/icon.png",
  subCategories: [{ key: "agent-context" }, { key: "agent-memory" }],
  labels: { requires_api_key: "false" },
  namespace: { canonicalName: "@clawhub_pskoett/self-improving-agent" },
};

describe("SkillHub marketplace adapter", () => {
  it("normalises live records and maps them to the marketplace contract", () => {
    const entry = normalizeShowcaseSkill(LIVE_SKILL)!;
    expect(entry.subCategories).toEqual(["agent-context", "agent-memory"]);
    const item = mapSkillhubEntry(entry);
    expect(item.id).toBe("skillhub-self-improving-agent");
    expect(item.descriptionEn).toBe(LIVE_SKILL.description);
    expect(item.source).toBe("skillhub");
  });

  it("rejects records without slug or name", () => {
    expect(normalizeShowcaseSkill({ ...LIVE_SKILL, slug: undefined })).toBeNull();
    expect(normalizeShowcaseSkill({ ...LIVE_SKILL, name: "" })).toBeNull();
  });
});

describe("marketplace metadata and icon helpers", () => {
  it("validates detail links and rejects ambiguous categories", () => {
    const item = {
      id: "test", category: "plugin", type: "plugin", name: "Test", description: "Test",
      icon: "Puzzle", iconColor: "oklch(55% 0.15 160)", tags: ["测试"], scene: "integration",
      source: "community", sourceUrl: "https://example.com", documentationUrl: "https://example.com",
    } satisfies MarketplaceItem;
    expect(validateMarketplaceCatalog([item])).toEqual([]);
    expect(validateMarketplaceCatalog([{ ...item, categories: ["skill", "skill"] }])).toEqual([
      expect.objectContaining({ issue: "invalid-tool-types" }),
    ]);
  });

  it("uses trusted explicit icons before repository identity", () => {
    expect(marketplaceIconCandidates({
      iconUrl: "https://cloudcache.tencent-cloud.com/icon.png",
      repositoryUrl: "https://github.com/acme/tool",
      externalUrl: "https://tool.example.com",
      sourceUrl: "https://source.example.com",
      documentationUrl: undefined,
    })[0]).toBe("https://cloudcache.tencent-cloud.com/icon.png");
    expect(marketplaceIconUrl({ repositoryUrl: "https://github.com/acme/tool", externalUrl: undefined, sourceUrl: undefined, documentationUrl: undefined })).toBe("https://github.com/acme.png?size=64");
  });
});
