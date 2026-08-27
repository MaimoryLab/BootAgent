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
  version: "2.2.0",
  builtAt: "2026-08-27T00:00:00Z",
  items: bundledMarketplaceItems,
};
