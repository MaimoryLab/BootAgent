import type {
  MarketplaceKind,
  MarketplaceScene,
  MarketplaceSource,
} from "../types/marketplace";

// ── filter state (exported so page and detail can share the type) ─────────────

export interface FilterState {
  kinds: Set<MarketplaceKind>;
  sources: Set<MarketplaceSource>;
  scenes: Set<MarketplaceScene>;
  requiresApiKey: boolean | null;
}

export const EMPTY_FILTERS: FilterState = {
  kinds: new Set(),
  sources: new Set(),
  scenes: new Set(),
  requiresApiKey: null,
};

export function hasActiveFilters(f: FilterState): boolean {
  return (
    f.kinds.size > 0 ||
    f.sources.size > 0 ||
    f.scenes.size > 0 ||
    f.requiresApiKey !== null
  );
}
