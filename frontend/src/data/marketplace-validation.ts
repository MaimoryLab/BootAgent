import type { MarketplaceItem } from "../types/marketplace";

export type MarketplaceValidationIssue =
  | "missing-description"
  | "missing-tags"
  | "missing-scene"
  | "missing-source"
  | "category-type-mismatch"
  | "missing-detail-link";

/** Validates the metadata required for reliable discovery and filtering. */
export function validateMarketplaceItem(item: MarketplaceItem): MarketplaceValidationIssue[] {
  const issues: MarketplaceValidationIssue[] = [];
  if (!item.description.trim()) issues.push("missing-description");
  if (!item.tags?.length && !item.tagKeys?.length) issues.push("missing-tags");
  if (!item.scene && !item.scenes?.length) issues.push("missing-scene");
  if (!item.source) issues.push("missing-source");

  const expectedType = item.category === "skill" || item.category === "mcp-server"
    ? "installable"
    : item.category === "plugin" ? "plugin"
      : item.category === "ai-product" ? "agent-product" : undefined;
  if (expectedType && item.type !== expectedType) issues.push("category-type-mismatch");

  const hasDetailLink = Boolean(item.installPrompt || item.readmeUrl || item.documentationUrl || item.externalUrl || item.sourceUrl);
  if (!hasDetailLink) issues.push("missing-detail-link");
  return issues;
}

export function validateMarketplaceCatalog(items: MarketplaceItem[]) {
  return items.flatMap((item) => validateMarketplaceItem(item).map((issue) => ({ item, issue })));
}
