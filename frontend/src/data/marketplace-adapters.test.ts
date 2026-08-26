import { describe, expect, it, vi } from "vitest";

// Block the real Wails runtime: importing it registers a module-level timer in
// drag.js that fires after jsdom teardown ("window is not defined" in CI).
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

import { mapSkillhubEntry } from "./skillhub-adapter";
import { mcpserversItems } from "./mcpservers-adapter";
import { extensionItems } from "./extension-catalog";
import { normalizeShowcaseSkill, type ShowcaseSkill } from "./useMarketplaceCatalog";

// ── live showcase payload normalisation (需求4) ───────────────────────────────

const LIVE_SKILL: ShowcaseSkill = {
  slug: "self-improving-agent",
  name: "self-improving agent",
  description: "A skill where the agent logs its own findings",
  description_zh: "记录自身发现以实现自我改进的技能",
  iconUrl: "https://example.com/icon.png",
  category: "ai-agent",
  // Live API sends objects; the bundled snapshot uses plain strings.
  subCategories: [
    { key: "agent-context", name: "上下文管理" },
    { key: "agent-memory", name: "记忆增强" },
  ],
  stars: 4433,
  downloads: 1132494,
  score: 100000,
  // Live API serialises the flag as a string.
  labels: { requires_api_key: "false" },
  namespace: { canonicalName: "@clawhub_pskoett/self-improving-agent" },
  homepage: "https://api.skillhub.cn/clawhub_pskoett/self-improving-agent",
};

describe("normalizeShowcaseSkill", () => {
  it("normalises object subCategories to key strings", () => {
    const entry = normalizeShowcaseSkill(LIVE_SKILL);
    expect(entry?.subCategories).toEqual(["agent-context", "agent-memory"]);
  });

  it("accepts snapshot-style string subCategories unchanged", () => {
    const entry = normalizeShowcaseSkill({ ...LIVE_SKILL, subCategories: ["dev-git", "dev-api"] });
    expect(entry?.subCategories).toEqual(["dev-git", "dev-api"]);
  });

  it("parses string requires_api_key flags", () => {
    expect(normalizeShowcaseSkill(LIVE_SKILL)?.requiresApiKey).toBe(false);
    expect(
      normalizeShowcaseSkill({ ...LIVE_SKILL, labels: { requires_api_key: "true" } })?.requiresApiKey,
    ).toBe(true);
  });

  it("uses description_zh as description and keeps the English one", () => {
    const entry = normalizeShowcaseSkill(LIVE_SKILL);
    expect(entry?.description).toBe("记录自身发现以实现自我改进的技能");
    expect(entry?.descriptionEn).toBe("A skill where the agent logs its own findings");
  });

  it("reads the canonical namespace for the install prompt", () => {
    expect(normalizeShowcaseSkill(LIVE_SKILL)?.namespace).toBe(
      "@clawhub_pskoett/self-improving-agent",
    );
  });

  it("rejects records without slug or name", () => {
    expect(normalizeShowcaseSkill({ ...LIVE_SKILL, slug: undefined })).toBeNull();
    expect(normalizeShowcaseSkill({ ...LIVE_SKILL, name: "" })).toBeNull();
  });
});

// ── shared skillhub mapper (需求1/4) ──────────────────────────────────────────

describe("mapSkillhubEntry", () => {
  it("carries descriptionEn through to the MarketplaceItem", () => {
    const entry = normalizeShowcaseSkill(LIVE_SKILL)!;
    const item = mapSkillhubEntry(entry);
    expect(item.id).toBe("skillhub-self-improving-agent");
    expect(item.description).toBe("记录自身发现以实现自我改进的技能");
    expect(item.descriptionEn).toBe("A skill where the agent logs its own findings");
    expect(item.source).toBe("skillhub");
  });

  it("keeps raw subCategory keys in tagKeys and Chinese labels in tags", () => {
    const item = mapSkillhubEntry(normalizeShowcaseSkill(LIVE_SKILL)!);
    expect(item.tagKeys).toEqual(["agent-context", "agent-memory"]);
    expect(item.tags).toEqual(["上下文管理", "记忆增强"]);
  });
});

// ── mcpservers GitHub avatar icons (需求3) ────────────────────────────────────

describe("mcpservers iconUrl", () => {
  it("derives the GitHub owner avatar for entries with a repo", () => {
    const withGithub = mcpserversItems.find((i) => i.id === "mcp-airtable-mcp-server");
    expect(withGithub?.iconUrl).toBe("https://github.com/airtable.png?size=64");
  });

  it("leaves entries without a repo on the lucide fallback", () => {
    const withoutGithub = mcpserversItems.find((i) => i.id === "mcp-ahrefs-mcp-server");
    expect(withoutGithub).toBeDefined();
    expect(withoutGithub?.iconUrl).toBeUndefined();
  });
});

describe("extension catalog", () => {
  it("contains official plugin and standalone AI product entries", () => {
    expect(extensionItems.filter((item) => item.category === "plugin")).toHaveLength(2);
    expect(extensionItems.filter((item) => item.category === "ai-product")).toHaveLength(3);
    expect(extensionItems.every((item) => item.externalUrl && item.sourceUrl)).toBe(true);
    expect(extensionItems.every((item) => item.type !== "installable")).toBe(true);
  });
});
