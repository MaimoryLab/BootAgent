/** Runtime marketplace catalog with per-adapter snapshot fallbacks. */
import { useEffect, useState } from "react";

import { api } from "../backend/api";
import type { MarketplaceItem } from "../types/marketplace";
import type { MarketplaceDynamicResult, MarketplaceSourceStatus } from "../types/api";
import { recordMarketplaceEvent } from "../utils/marketplace-telemetry";

export { normalizeShowcaseSkill, type ShowcaseSkill } from "./marketplace-source-adapters";

interface CatalogState {
  items: MarketplaceItem[];
  live: boolean;
  version: string;
  loading: boolean;
  sources: MarketplaceSourceStatus[];
}

const EMPTY_CATALOG: CatalogState = { items: [], live: false, version: "", loading: true, sources: [] };
let resolved: CatalogState | null = null;
let pending: Promise<CatalogState> | null = null;

function mergeSkillhub(snapshot: MarketplaceItem[], live: MarketplaceItem[]): MarketplaceItem[] {
  const seen = new Set<string>();
  const seenSourceNames = new Set<string>();
  // Keep the bundled snapshot as an offline baseline. A failed or partial
  // remote refresh must never make an entire source disappear from the UI.
  // Static catalog is the stable, curated baseline. Dynamic adapter results
  // are appended afterwards and may only fill gaps; this keeps card order
  // predictable and prevents a refresh from moving existing tools around.
  const items = [...snapshot, ...live].filter((item) => {
    if (seen.has(item.id)) return false;
    const sourceName = `${item.source ?? ""}:${item.name.trim().toLocaleLowerCase()}`;
    if (sourceName !== ":" && seenSourceNames.has(sourceName)) return false;
    seen.add(item.id);
    if (sourceName !== ":") seenSourceNames.add(sourceName);
    return true;
  });
  const featured = items.find((item) => item.id === "github-maimorylab-codeoff");
  return featured ? [featured, ...items.filter((item) => item !== featured)] : items;
}

function ensureCatalog(): Promise<CatalogState> {
  if (!pending) {
    pending = api.marketplaceCatalog().then((catalog) => {
      const state: CatalogState = { items: catalog.items, live: false, version: catalog.version, loading: false, sources: [] };
      resolved = state;
      return state;
    }).catch(() => EMPTY_CATALOG);
  }
  return pending!;
}

export function useMarketplaceCatalog(options: { query?: string; category?: string; sources?: string[] } = {}): CatalogState {
  const [state, setState] = useState<CatalogState>(() => resolved ?? EMPTY_CATALOG);

  useEffect(() => {
    let cancelled = false;
    let offset = 0;
    let hasMore = true;
    let loadingMore = false;
    // Categories are local facets over the current result set. Sending them to
    // the adapter would replace the set and make a filter appear to add cards.
    const request = { query: options.query, limit: 50 };
    const loadMore = async () => {
      if (cancelled || loadingMore || !hasMore) return;
      loadingMore = true;
      try {
        const dynamic = await api.marketplaceDiscoverSources({ ...request, offset });
        const more = (dynamic.items ?? []) as MarketplaceItem[];
        if (more.length === 0) { hasMore = false; return; }
        offset = dynamic.next_offset ?? (offset + 50);
        hasMore = dynamic.has_more;
        offset = dynamic.next_offset ?? more.length;
        setState((current) => ({ ...current, items: mergeSkillhub(current.items, more), sources: dynamic.sources ?? current.sources, live: !dynamic.stale }));
      } finally {
        loadingMore = false;
      }
    };
    const onScroll = () => {
      if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 640) void loadMore();
    };
    void ensureCatalog().then((snapshot) => {
      if (cancelled) return;
      setState({ ...snapshot, loading: true });
      void api.marketplaceDiscoverSources({ query: options.query, limit: 50, offset: 0 }).then((dynamic) => {
        for (const source of dynamic.sources ?? []) recordMarketplaceEvent("source_refresh", source.id);
        const live = (dynamic.items ?? []) as MarketplaceItem[];
        const next = { items: mergeSkillhub(snapshot.items, live), live: !dynamic.stale, version: snapshot.version, loading: false, sources: dynamic.sources ?? [] };
        resolved = next;
        if (!cancelled) setState(next);
        hasMore = dynamic.has_more;
        window.addEventListener("scroll", onScroll, { passive: true });
      }).catch(() => {
        if (!cancelled) setState({ ...snapshot, loading: false });
      });
    });
    return () => { cancelled = true; window.removeEventListener("scroll", onScroll); };
  }, [options.query]);

  return state;
}
