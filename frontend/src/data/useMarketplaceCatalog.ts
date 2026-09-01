/** Runtime marketplace catalog with per-adapter snapshot fallbacks. */
import { useEffect, useState } from "react";

import type { MarketplaceItem } from "../types/marketplace";
import { bundledMarketplaceItems, loadMarketplaceSources } from "./marketplace-source-adapters";

export { normalizeShowcaseSkill, type ShowcaseSkill } from "./marketplace-source-adapters";

interface CatalogState {
  items: MarketplaceItem[];
  live: boolean;
}

const SNAPSHOT: CatalogState = { items: bundledMarketplaceItems, live: false };
let resolved: CatalogState | null = null;
let pending: Promise<CatalogState> | null = null;

function ensureCatalog(): Promise<CatalogState> {
  if (!pending) {
    pending = loadMarketplaceSources().then((state) => {
      resolved = state;
      return state;
    });
  }
  return pending;
}

export function useMarketplaceCatalog(): CatalogState {
  const [state, setState] = useState<CatalogState>(() => resolved ?? SNAPSHOT);

  useEffect(() => {
    if (resolved) return;
    let cancelled = false;
    void ensureCatalog().then((next) => {
      if (!cancelled) setState(next);
    });
    return () => { cancelled = true; };
  }, []);

  return state;
}
