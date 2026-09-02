/** Runtime marketplace catalog with a static baseline and source adapters. */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api } from "../backend/api";
import type { MarketplaceDynamicResult, MarketplaceSourceStatus } from "../types/api";
import type { MarketplaceItem } from "../types/marketplace";
import { recordMarketplaceEvent } from "../utils/marketplace-telemetry";

export { normalizeShowcaseSkill, type ShowcaseSkill } from "./marketplace-source-adapters";

const PAGE_LIMIT = 50;
const DEFAULT_SOURCES = ["skillhub", "mcpservers"];
const DYNAMIC_SOURCES = new Set(DEFAULT_SOURCES);
const REFRESH_INTERVAL_MS = 10 * 60 * 1000;
// Local facets (scene, kind and API-key requirement) are applied after a
// remote page arrives. If a page contains no matching cards, the container may
// still be too short to emit another scroll event. Fill a bounded number of
// pages automatically, then leave further work to an intentional user scroll.
const MAX_AUTO_FILL_PAGES = 8;

interface CatalogState {
  items: MarketplaceItem[];
  live: boolean;
  version: string;
  loading: boolean;
  refreshing: boolean;
  lastUpdated: string;
  sources: MarketplaceSourceStatus[];
}

export interface MarketplaceCatalogOptions {
  query?: string;
  /** Local facet. It is intentionally not sent to a remote adapter. */
  category?: string;
  sources?: string[];
  /** Stable identity for local kind/scene facets; never sent upstream. */
  facetKey?: string;
  /** Detail pages can opt out of the background refresh timer. */
  autoRefresh?: boolean;
}

export interface MarketplaceCatalogState extends CatalogState {
  refresh: () => void;
}

const EMPTY_CATALOG: CatalogState = {
  items: [],
  live: false,
  version: "",
  loading: true,
  refreshing: false,
  lastUpdated: "",
  sources: [],
};

let snapshotResolved: Pick<CatalogState, "items" | "version"> | null = null;
let snapshotPending: Promise<Pick<CatalogState, "items" | "version">> | null = null;

/**
 * Keep the embedded catalog first and append remote records after it. A source
 * can return the same tool under a new id, so source/name is a second, stable
 * dedupe key. This helper is exported for focused unit tests.
 */
export function mergeMarketplaceItems(snapshot: MarketplaceItem[], dynamic: MarketplaceItem[]): MarketplaceItem[] {
  const seenIDs = new Set<string>();
  const seenSourceNames = new Set<string>();
  const items = [...snapshot, ...dynamic].filter((item) => {
    if (!item.id || seenIDs.has(item.id)) return false;
    const sourceName = `${item.source ?? ""}:${item.name.trim().toLocaleLowerCase()}`;
    if (sourceName !== ":" && seenSourceNames.has(sourceName)) return false;
    seenIDs.add(item.id);
    if (sourceName !== ":") seenSourceNames.add(sourceName);
    return true;
  });
  const featured = items.find((item) => item.id === "github-maimorylab-codeoff");
  return featured ? [featured, ...items.filter((item) => item !== featured)] : items;
}

function ensureCatalog(): Promise<Pick<CatalogState, "items" | "version">> {
  if (!snapshotPending) {
    snapshotPending = api.marketplaceCatalog().then((catalog) => {
      const snapshot = { items: (catalog.items ?? []) as MarketplaceItem[], version: catalog.version ?? "" };
      snapshotResolved = snapshot;
      return snapshot;
    }).catch(() => {
      // A transient bridge failure must not poison the process-wide promise.
      // Keep a previously resolved snapshot when available and allow the next
      // mounted page to retry the local catalog call.
      snapshotPending = null;
      return snapshotResolved ?? { items: [], version: "" };
    });
  }
  return snapshotPending;
}

function sourceList(values?: string[]): string[] {
  const selected = [...new Set((values ?? []).map((value) => value.trim()).filter(Boolean))];
  return selected.length ? selected : DEFAULT_SOURCES;
}

function queryID(generation: number): string {
  // This value is only a response correlation token, never an identifier or
  // credential. Avoid randomness so it is deterministic in browser tests.
  return `marketplace-q${generation}`;
}

function validResponse(response: MarketplaceDynamicResult, expectedQueryID: string): boolean {
  return !response.query_id || response.query_id === expectedQueryID;
}

function latestTimestamp(statuses: MarketplaceSourceStatus[], fallback = ""): string {
  return statuses.map((status) => status.fetched_at ?? "").filter(Boolean).sort().at(-1) ?? fallback;
}

function flattenSourceItems(sourceItems: Map<string, MarketplaceItem[]>): MarketplaceItem[] {
  return [...sourceItems.values()].flat();
}

export function useMarketplaceCatalog(options: MarketplaceCatalogOptions = {}): MarketplaceCatalogState {
  const query = (options.query ?? "").trim();
  const category = (options.category ?? "").trim();
  const facetKey = (options.facetKey ?? "").trim();
  const selectedSources = useMemo(() => sourceList(options.sources), [options.sources?.join(",")]);
  // Static source filters (GitHub, npm, etc.) are satisfied by the embedded
  // snapshot. Only adapters implemented by the Go service receive network
  // requests; an unsupported source must not show a misleading "unavailable"
  // status or reset a dynamic cursor.
  const remoteSources = useMemo(
    () => selectedSources.filter((source) => DYNAMIC_SOURCES.has(source)),
    [selectedSources],
  );
  // Category is a local facet, but part of the session identity so a newly
  // selected facet can ask adapters for cards that were not in the old page.
  const sessionKey = `${query}\u0000${category}\u0000${selectedSources.join(",")}\u0000${facetKey}`;
  const autoRefresh = options.autoRefresh !== false;
  const [state, setState] = useState<CatalogState>(() => ({
    ...(snapshotResolved ? { ...EMPTY_CATALOG, ...snapshotResolved, loading: false } : EMPTY_CATALOG),
  }));
  const [refreshToken, setRefreshToken] = useState(0);
  const generationRef = useRef(0);
  const previousSessionRef = useRef<string | null>(null);
  const previousRefreshTokenRef = useRef(0);

  const refresh = useCallback(() => setRefreshToken((value) => value + 1), []);

  useEffect(() => {
    let disposed = false;
    const generation = ++generationRef.current;
    const expectedQueryID = queryID(generation);
    const sameSession = previousSessionRef.current === sessionKey;
    // Only an explicit refresh of the same session bypasses the adapter ETag.
    // A later search or facet change should use the normal cache policy.
    const forceRefresh = sameSession && refreshToken > previousRefreshTokenRef.current;
    previousSessionRef.current = sessionKey;
    previousRefreshTokenRef.current = refreshToken;
    const cursors = new Map<string, { offset: number; hasMore: boolean }>(
      remoteSources.map((source) => [source, { offset: 0, hasMore: true }]),
    );
    const sourceItems = new Map<string, MarketplaceItem[]>();
    const statuses = new Map<string, MarketplaceSourceStatus>();
    let loadingMore = false;
    let initialLoaded = false;
    let autoFillPages = 0;
    let autoFillTimer: number | null = null;
    let snapshot: Pick<CatalogState, "items" | "version"> = { items: [], version: "" };

    const isCurrent = () => !disposed && generationRef.current === generation;
    const requestPage = (source: string, offset: number, forceRefresh: boolean) => api.marketplaceDiscoverSources({
      source,
      query: query || undefined,
      limit: PAGE_LIMIT,
      offset,
      force_refresh: forceRefresh,
      query_id: expectedQueryID,
    });

    const publish = (loading: boolean, refreshing: boolean) => {
      if (!isCurrent()) return;
      const sourceStatuses = remoteSources.map((source) => statuses.get(source)).filter(Boolean) as MarketplaceSourceStatus[];
      const dynamic = flattenSourceItems(sourceItems);
      setState((current) => ({
        ...current,
        items: mergeMarketplaceItems(snapshot.items, dynamic),
        version: snapshot.version,
        live: sourceStatuses.some((status) => status.state === "live"),
        loading,
        refreshing,
        lastUpdated: latestTimestamp(sourceStatuses, current.lastUpdated),
        sources: sourceStatuses,
      }));
    };

    function scheduleAutoFill() {
      if (!isCurrent() || autoFillPages >= MAX_AUTO_FILL_PAGES || autoFillTimer !== null) return;
      const currentContainer = document.querySelector<HTMLElement>(".page-body.marketplace-page");
      if (!currentContainer || currentContainer.scrollHeight > currentContainer.clientHeight + 640) return;
      if (!remoteSources.some((source) => cursors.get(source)?.hasMore)) return;
      autoFillTimer = window.setTimeout(() => {
        autoFillTimer = null;
        if (!isCurrent() || loadingMore || autoFillPages >= MAX_AUTO_FILL_PAGES) return;
        if (!remoteSources.some((source) => cursors.get(source)?.hasMore)) return;
        autoFillPages += 1;
        void loadMore();
      }, 0);
    }

    const loadMore = async () => {
      if (!isCurrent() || loadingMore || !initialLoaded) return;
      const pendingSources = remoteSources.filter((source) => cursors.get(source)?.hasMore);
      if (!pendingSources.length) return;
      loadingMore = true;
      try {
        const responses = await Promise.all(pendingSources.map(async (source) => {
          const offset = cursors.get(source)?.offset ?? 0;
          try {
            return { source, result: await requestPage(source, offset, false) };
          } catch (error) {
            return { source, error };
          }
        }));
        if (!isCurrent()) return;
        for (const response of responses) {
          if (response.result && validResponse(response.result, expectedQueryID)) {
            const items = (response.result.items ?? []) as MarketplaceItem[];
            sourceItems.set(response.source, [...(sourceItems.get(response.source) ?? []), ...items]);
            for (const status of response.result.sources ?? []) statuses.set(status.id, status);
            const before = cursors.get(response.source)?.offset ?? 0;
            const next = response.result.next_offset ?? before + items.length;
            // A local facet can remove every card from a remote page while the
            // source cursor still advances. Keep walking until the adapter says
            // there is no next page; otherwise a match on page two is hidden.
            if (next <= before) {
              const currentStatus = statuses.get(response.source);
              statuses.set(response.source, {
                id: response.source,
                state: "unavailable",
                item_count: currentStatus?.item_count ?? sourceItems.get(response.source)?.length ?? 0,
                total: currentStatus?.total ?? 0,
                has_more: false,
                next_offset: before,
                error: "request failed",
              });
              cursors.set(response.source, { offset: before, hasMore: false });
            } else {
              cursors.set(response.source, { offset: next, hasMore: Boolean(response.result.has_more) });
            }
          } else {
            // Keep the current offset. A transient later-page failure should
            // not make the next retry start at zero and duplicate cards.
            const current = cursors.get(response.source) ?? { offset: 0, hasMore: false };
            const previousStatus = statuses.get(response.source);
            statuses.set(response.source, {
              id: response.source,
              state: "unavailable",
              item_count: previousStatus?.item_count ?? sourceItems.get(response.source)?.length ?? 0,
              total: previousStatus?.total ?? 0,
              has_more: false,
              next_offset: current.offset,
              error: "request failed",
            });
            cursors.set(response.source, { ...current, hasMore: false });
          }
        }
        publish(false, false);
      } finally {
        loadingMore = false;
        scheduleAutoFill();
      }
    };

    const loadInitial = async () => {
      snapshot = await ensureCatalog();
      if (!isCurrent()) return;
      const baselineIDs = new Set(snapshot.items.map((item) => item.id));
      if (sameSession) {
        // Preserve already visible dynamic cards while a refresh is pending;
        // successful source responses below replace only their own slice.
        for (const item of state.items) {
          if (item.source && remoteSources.includes(item.source) && !baselineIDs.has(item.id)) {
            sourceItems.set(item.source, [...(sourceItems.get(item.source) ?? []), item]);
          }
        }
        for (const status of state.sources) {
          if (remoteSources.includes(status.id)) statuses.set(status.id, status);
        }
      }
      if (!sameSession) {
        setState((current) => ({ ...current, items: snapshot.items, version: snapshot.version, loading: true, refreshing: false, live: false, sources: [] }));
      } else {
        setState((current) => ({ ...current, loading: current.items.length === 0, refreshing: true }));
      }
      const responses = await Promise.all(remoteSources.map(async (source) => {
        try {
          return { source, result: await requestPage(source, 0, forceRefresh) };
        } catch (error) {
          return { source, error };
        }
      }));
      if (!isCurrent()) return;
      for (const response of responses) {
        if (response.result && validResponse(response.result, expectedQueryID)) {
          const items = (response.result.items ?? []) as MarketplaceItem[];
          sourceItems.set(response.source, items);
          for (const status of response.result.sources ?? []) {
            statuses.set(status.id, status);
            recordMarketplaceEvent("source_refresh", status.id);
          }
          const before = cursors.get(response.source)?.offset ?? 0;
          const next = response.result.next_offset ?? before + items.length;
          cursors.set(response.source, { offset: next, hasMore: Boolean(response.result.has_more) && next > before });
        } else {
          if (!sameSession) sourceItems.set(response.source, []);
          const previousStatus = statuses.get(response.source);
          statuses.set(response.source, {
            id: response.source,
            state: "unavailable",
            item_count: previousStatus?.item_count ?? sourceItems.get(response.source)?.length ?? 0,
            total: previousStatus?.total ?? 0,
            has_more: false,
            next_offset: previousStatus?.next_offset ?? 0,
            error: "request failed",
          });
          cursors.set(response.source, { offset: 0, hasMore: false });
        }
      }
      initialLoaded = true;
      publish(false, false);
      // A short result set has no scroll event to trigger pagination. Fill one
      // viewport-sized gap, then let the normal scroll handler take over.
      const currentContainer = document.querySelector<HTMLElement>(".page-body.marketplace-page");
      if (isCurrent() && currentContainer && currentContainer.scrollHeight <= currentContainer.clientHeight + 640) {
        scheduleAutoFill();
      }
    };

    const container = document.querySelector<HTMLElement>(".page-body.marketplace-page");
    const onScroll = () => {
      if (container && container.scrollTop + container.clientHeight >= container.scrollHeight - 640) void loadMore();
    };
    container?.addEventListener("scroll", onScroll, { passive: true });
    void loadInitial();
    return () => {
      disposed = true;
      if (autoFillTimer !== null) window.clearTimeout(autoFillTimer);
      container?.removeEventListener("scroll", onScroll);
    };
  }, [sessionKey, refreshToken, query, remoteSources, refresh]);

  useEffect(() => {
    if (!autoRefresh) return;
    const timer = window.setInterval(refresh, REFRESH_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [autoRefresh, refresh]);

  return { ...state, refresh };
}
