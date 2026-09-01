/**
 * Marketplace catalog — real data sources only.
 *
 * Each source adapter owns its bundled snapshot and optional live loader.
 * The merged catalog contains installable skills, MCP servers, extensions,
 * GitHub projects, products, prompt templates, and workflow templates.
 */

import type { MarketplaceCatalog } from "../types/marketplace";
import { bundledMarketplaceItems } from "./marketplace-source-adapters";

export const STATIC_CATALOG: MarketplaceCatalog = {
  version: "2.3.0",
  builtAt: "2026-08-27T00:00:00Z",
  items: bundledMarketplaceItems.find((item) => item.id === "github-maimorylab-codeoff")
    ? [bundledMarketplaceItems.find((item) => item.id === "github-maimorylab-codeoff")!, ...bundledMarketplaceItems.filter((item) => item.id !== "github-maimorylab-codeoff")]
    : bundledMarketplaceItems,
};
