import { describe, expect, it } from "vitest";

import { localizeTag, marketplaceTagPairs } from "./tag-labels";
import { translateIfKnown } from "../i18n";

describe("localizeTag", () => {
  it("maps known subCategory keys per locale", () => {
    expect(localizeTag("agent-context", "zh-CN")).toBe("上下文管理");
    expect(localizeTag("agent-context", "en")).toBe("Context");
    expect(localizeTag("data-web-scraping", "zh-CN")).toBe("网页抓取");
    expect(localizeTag("data-web-scraping", "en")).toBe("Scraping");
  });

  it("falls back to the truncated de-prefixed key in both locales", () => {
    // "brand-new-key" is 13 chars -> first 7 + ellipsis.
    expect(localizeTag("custom-brand-new-key", "zh-CN")).toBe("brand-n…");
    expect(localizeTag("custom-brand-new-key", "en")).toBe("brand-n…");
    expect(localizeTag("misc", "en")).toBe("misc");
  });
});

describe("translateIfKnown", () => {
  it("translates registered dictionary keys", () => {
    expect(translateIfKnown("en", "官方")).toBe("Official");
    expect(translateIfKnown("en", "MCP")).toBe("MCP");
    expect(translateIfKnown("zh-CN", "官方")).toBe("官方");
  });

  it("passes unknown strings through unchanged", () => {
    expect(translateIfKnown("en", "不是字典里的词")).toBe("不是字典里的词");
  });
});

describe("marketplaceTagPairs", () => {
  it("prefers tagKeys and localises them", () => {
    const pairs = marketplaceTagPairs(
      { tagKeys: ["agent-memory"], tags: ["记忆增强"] },
      "en",
    );
    expect(pairs).toEqual([["agent-memory", "Memory"]]);
  });

  it("translates plain tags without tagKeys when they are dictionary keys", () => {
    expect(marketplaceTagPairs({ tags: ["官方", "MCP"] }, "en")).toEqual([
      ["官方", "Official"],
      ["MCP", "MCP"],
    ]);
    expect(marketplaceTagPairs({ tags: ["官方"] }, "zh-CN")).toEqual([["官方", "官方"]]);
  });

  it("returns an empty list for items without tags", () => {
    expect(marketplaceTagPairs({}, "en")).toEqual([]);
  });
});
