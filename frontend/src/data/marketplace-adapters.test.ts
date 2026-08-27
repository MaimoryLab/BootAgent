import { describe, expect, it, vi } from "vitest";

// Block the real Wails runtime: importing it registers a module-level timer in
// drag.js that fires after jsdom teardown ("window is not defined" in CI).
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

import { mapSkillhubEntry } from "./skillhub-adapter";
import { mcpserversItems } from "./mcpservers-adapter";
import { extensionItems } from "./extension-catalog";
import { githubItems } from "./github-adapter";
import { marketplaceIconCandidates, marketplaceIconUrl } from "./marketplace-icons";
import { normalizeShowcaseSkill, type ShowcaseSkill } from "./useMarketplaceCatalog";
import { validateMarketplaceCatalog } from "./marketplace-validation";
import { STATIC_CATALOG } from "./marketplace-catalog";
import { ecosystemItems } from "./ecosystem-catalog";
import { marketplaceSourceAdapters } from "./marketplace-source-adapters";
import { templateItems } from "./template-catalog";
import type { MarketplaceItem } from "../types/marketplace";

// ── live showcase payload normalisation (需求4) ───────────────────────────────

const LIVE_SKILL: ShowcaseSkill = {
  slug: "self-improving-agent",
  name: "self-improving agent",
  description: "A skill where the agent logs its own findings",
  description_zh: "记录自身发现以实现自我改进的技能",
  iconUrl: "https://example.com/icon.png",
  category: "skill",
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
    expect(item.category).toBe("skill");
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
    expect(extensionItems.find((item) => item.id === "ai-product-continue")?.categories).toEqual(["ai-product", "plugin"]);
  });
});

describe("GitHub adapter", () => {
  it("maps recommended repositories into detailed discovery entries", () => {
    expect(githubItems.length).toBeGreaterThanOrEqual(20);
    expect(githubItems.some((item) => item.id === "github-diegosouzapw-omniroute")).toBe(true);
    expect(githubItems.some((item) => item.category === "plugin")).toBe(true);
    expect(githubItems.some((item) => item.category === "ai-product")).toBe(true);
    for (const item of githubItems) {
      expect(item.source).toBe("github");
      expect(item.repositoryUrl).toMatch(/^https:\/\/github\.com\//);
      expect(item.readmeUrl).toMatch(/^https:\/\/raw\.githubusercontent\.com\//);
      expect(item.installPrompt).toContain("README");
      expect(item.githubStars).toBeGreaterThan(0);
    }
  });

  it("keeps category and item type aligned", () => {
    expect(githubItems.filter((item) => item.category === "plugin").every((item) => item.type === "plugin")).toBe(true);
    expect(githubItems.filter((item) => item.category === "ai-product").every((item) => item.type === "agent-product")).toBe(true);
  });

  it("preserves cross-type identities instead of forcing every repository into one type", () => {
    expect(githubItems.find((item) => item.id === "github-zhaoxuya520-reverse-skill")?.categories).toEqual(["plugin", "skill"]);
    expect(githubItems.find((item) => item.id === "github-langgenius-dify")?.categories).toEqual(["ai-product", "workflow"]);
  });
});

describe("marketplace catalog metadata", () => {
  it("has complete metadata for every discoverable item", () => {
    expect(validateMarketplaceCatalog(STATIC_CATALOG.items)).toEqual([]);
  });

  it("rejects a source-only item without an introduction document", () => {
    const sourceOnly = {
      id: "source-only", category: "plugin", type: "plugin", name: "Source only",
      description: "A test entry", icon: "Puzzle", iconColor: "oklch(55% 0.15 160)",
      tags: ["测试"], scene: "integration", source: "community", sourceUrl: "https://example.com",
    } satisfies MarketplaceItem;
    expect(validateMarketplaceCatalog([sourceOnly])).toEqual([
      expect.objectContaining({ issue: "missing-introduction-document" }),
    ]);
  });

  it("rejects ambiguous multi-type metadata", () => {
    const invalid = {
      ...extensionItems[0],
      categories: ["skill", "skill"],
    } satisfies MarketplaceItem;
    expect(validateMarketplaceCatalog([invalid])).toEqual([
      expect.objectContaining({ issue: "invalid-tool-types" }),
    ]);
  });

  it("includes first-party registries and package ecosystems as traceable sources", () => {
    expect(ecosystemItems.map((item) => item.source)).toEqual(expect.arrayContaining([
      "anthropic", "npm", "docker", "vscode", "pypi", "mcp-registry",
    ]));
    expect(ecosystemItems.every((item) => item.capabilities?.length && item.deploymentModes?.length)).toBe(true);
  });
});

describe("marketplace source adapters", () => {
  it("registers each source once and builds the bundled catalog through adapters", () => {
    const ids = marketplaceSourceAdapters.map((adapter) => adapter.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(marketplaceSourceAdapters.some((adapter) => adapter.loadLive)).toBe(true);
    expect(marketplaceSourceAdapters.every((adapter) => adapter.snapshot.length > 0)).toBe(true);
  });

  it("ships enough documented prompt and workflow entries to expose the category", () => {
    expect(templateItems).toHaveLength(7);
    expect(templateItems.every((item) => item.category === "workflow")).toBe(true);
    expect(templateItems.some((item) => item.installableKind === "prompt-template")).toBe(true);
    expect(templateItems.some((item) => item.installableKind === "workflow-script")).toBe(true);
    expect(templateItems.every((item) => item.documentationUrl && item.installPrompt)).toBe(true);
  });
});

describe("marketplace icon resolution", () => {
  it("uses explicit icons first, then GitHub identity, then a domain favicon", () => {
    expect(marketplaceIconCandidates({
      iconUrl: "https://cloudcache.tencent-cloud.com/icon.png",
      repositoryUrl: "https://github.com/acme/tool",
      externalUrl: "https://tool.example.com",
      sourceUrl: "https://source.example.com",
      documentationUrl: undefined,
    })[0]).toBe("https://cloudcache.tencent-cloud.com/icon.png");
    expect(marketplaceIconUrl({
      repositoryUrl: "https://github.com/acme/tool",
      externalUrl: undefined,
      sourceUrl: undefined,
      documentationUrl: undefined,
    })).toBe("https://github.com/acme.png?size=64");
  });

  it("rejects non-HTTPS and untrusted explicit icon URLs", () => {
    expect(marketplaceIconCandidates({
      iconUrl: "http://evil.example/icon.png",
      repositoryUrl: undefined,
      externalUrl: "javascript:alert(1)",
      sourceUrl: undefined,
      documentationUrl: undefined,
    })).toEqual([]);
  });
});
