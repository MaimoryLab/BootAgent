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
let livePending: Promise<MarketplaceItem[]> | null = null;

function mergeSkillhub(snapshot: MarketplaceItem[], live: MarketplaceItem[]): MarketplaceItem[] {
  const items = [...live, ...snapshot.filter((item) => item.source !== "skillhub")];
  const featured = items.find((item) => item.id === "github-maimorylab-codeoff");
  return featured ? [featured, ...items.filter((item) => item !== featured)] : items;
}

function ensureCatalog(): Promise<CatalogState> {
  if (!pending) {
    pending = api.marketplaceCatalog().then((catalog) => {
      const state = { items: catalog.items, live: false, version: catalog.version };
      resolved = state;
      return state;
    }).catch(() => EMPTY_CATALOG);
  }
  return pending;
}

function ensureLiveSkillhub(): Promise<MarketplaceItem[]> {
  if (!livePending) livePending = loadSkillhub();
  return livePending;
}

export function useMarketplaceCatalog(): CatalogState {
  const [state, setState] = useState<CatalogState>(() => resolved ?? EMPTY_CATALOG);

  useEffect(() => {
    let cancelled = false;
    void ensureCatalog().then((snapshot) => {
      if (cancelled) return;
      setState(snapshot);
      void ensureLiveSkillhub().then((live) => {
        const next = { items: mergeSkillhub(snapshot.items, live), live: true, version: snapshot.version };
        resolved = next;
        if (!cancelled) setState(next);
      }).catch(() => undefined);
    });
    return () => { cancelled = true; };
  }, []);

  return state;
}
