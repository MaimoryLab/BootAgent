import type { MarketplaceCategory, MarketplaceItem, MarketplaceKind } from "../types/marketplace";

const CATEGORY_KIND: Partial<Record<MarketplaceCategory, MarketplaceKind>> = {
  skill: "skill",
  "mcp-server": "mcp",
  plugin: "plugin",
  "ai-product": "agent-product",
  workflow: "workflow-script",
};

export function marketplaceCategories(item: MarketplaceItem): MarketplaceCategory[] {
  return [...new Set([item.category, ...(item.categories ?? [])])];
}

export function marketplaceKinds(item: MarketplaceItem): MarketplaceKind[] {
  const kinds = new Set<MarketplaceKind>();
  for (const category of marketplaceCategories(item)) {
    const kind = CATEGORY_KIND[category];
    if (kind) kinds.add(kind);
  }
  if (item.installableKind) {
    if (item.category === "workflow") kinds.delete("workflow-script");
    kinds.add(item.installableKind);
  }
  return [...kinds];
}
