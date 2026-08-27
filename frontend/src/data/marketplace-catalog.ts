/**
 * Marketplace catalog — real data sources only.
 *
 * - skillhubItems: top-50 hot skills from the skillhub CLI rankings API
 *   (frontend/src/data/skillhub-hot.json, refresh via `skillhub skill rankings`)
 * - mcpserversItems: priority MCP servers scraped from mcpservers.org
 *   (frontend/src/data/mcpservers-catalog.json)
 *
 * The earlier hand-written mock entries (external tools like Claude Code /
 * Cursor, sample prompts, news links) were removed on request — every card
 * now reflects a real, installable listing.
 */

import type { MarketplaceCatalog } from "../types/marketplace";
import { bundledMarketplaceItems } from "./marketplace-source-adapters";

export const STATIC_CATALOG: MarketplaceCatalog = {
  version: "2.1.0",
  builtAt: "2026-08-25T00:00:00Z",
  items: bundledMarketplaceItems,
};
