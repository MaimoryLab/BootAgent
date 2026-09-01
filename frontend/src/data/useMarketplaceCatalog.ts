/** Runtime marketplace catalog with per-adapter snapshot fallbacks. */
import { useEffect, useState } from "react";

import { api } from "../backend/api";
import type { MarketplaceItem } from "../types/marketplace";
import { loadSkillhub } from "./marketplace-source-adapters";

export { normalizeShowcaseSkill, type ShowcaseSkill } from "./marketplace-source-adapters";

interface CatalogState {
  items: MarketplaceItem[];
  live: boolean;
  version: string;
}

const EMPTY_CATALOG: CatalogState = { items: [], live: false, version: "" };
let resolved: CatalogState | null = null;
let pending: Promise<CatalogState> | null = null;

function mergeSkillhub(snapshot: MarketplaceItem[], live: MarketplaceItem[]): MarketplaceItem[] {
  const items = [...live, ...snapshot.filter((item) => item.source !== "skillhub")];
  const featured = items.find((item) => item.id === "github-maimorylab-codeoff");
  return featured ? [featured, ...items.filter((item) => item !== featured)] : items;
}

function ensureCatalog(): Promise<CatalogState> {
  if (!pending) {
    pending = api.marketplaceCatalog().then(async (catalog) => {
      try {
        return { items: mergeSkillhub(catalog.items, await loadSkillhub()), live: true, version: catalog.version };
      } catch {
        return { items: catalog.items, live: false, version: catalog.version };
      }
    }).catch(() => EMPTY_CATALOG).then((state) => (resolved = state));
  }
  return pending;
}

export function useMarketplaceCatalog(): CatalogState {
  const [state, setState] = useState<CatalogState>(() => resolved ?? EMPTY_CATALOG);

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
