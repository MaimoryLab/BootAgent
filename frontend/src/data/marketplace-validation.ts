import type { MarketplaceItem } from "../types/marketplace";

export type MarketplaceValidationIssue =
  | "missing-description"
  | "missing-tags"
  | "missing-scene"
  | "missing-source"
  | "invalid-tool-types"
  | "category-type-mismatch"
  | "missing-detail-link"
  | "missing-introduction-document";

/** Validates the metadata required for reliable discovery and filtering. */
export function validateMarketplaceItem(item: MarketplaceItem): MarketplaceValidationIssue[] {
  const issues: MarketplaceValidationIssue[] = [];
  if (!item.description.trim()) issues.push("missing-description");
  if (!item.tags?.length && !item.tagKeys?.length) issues.push("missing-tags");
  if (!item.scene && !item.scenes?.length) issues.push("missing-scene");
  if (!item.source) issues.push("missing-source");
  if (item.categories && (
    !item.categories.includes(item.category) ||
    new Set(item.categories).size !== item.categories.length
  )) issues.push("invalid-tool-types");

  const expectedType = item.category === "skill" || item.category === "mcp-server"
    ? "installable"
    : item.category === "plugin" ? "plugin"
      : item.category === "ai-product" ? "agent-product" : undefined;
  if (expectedType && item.type !== expectedType) issues.push("category-type-mismatch");

  const hasDetailLink = Boolean(item.installPrompt || item.readmeUrl || item.documentationUrl || item.externalUrl || item.sourceUrl);
  if (!hasDetailLink) issues.push("missing-detail-link");
  // SkillHub README is fetched through its slug binding; all other entries
  // must expose a stable documentation or README URL in the catalog.
  if (!item.documentationUrl && !item.readmeUrl && item.source !== "skillhub") {
    issues.push("missing-introduction-document");
  }
  return issues;
}

export function validateMarketplaceCatalog(items: MarketplaceItem[]) {
  return items.flatMap((item) => validateMarketplaceItem(item).map((issue) => ({ item, issue })));
}
