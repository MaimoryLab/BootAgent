import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { MarketplaceDynamicResult } from "../types/api";
import type { MarketplaceItem } from "../types/marketplace";
import { mergeMarketplaceItems, useMarketplaceCatalog } from "./useMarketplaceCatalog";

const item = (id: string, source: string, name = id): MarketplaceItem => ({
  id,
  category: source === "mcpservers" ? "mcp-server" : "skill",
  type: "installable",
  installableKind: source === "mcpservers" ? "mcp" : "skill",
  icon: "Puzzle",
  iconColor: "oklch(60% 0.16 75)",
  name,
  description: name,
  source: source as MarketplaceItem["source"],
});

function response(source: string, offset: number, items: MarketplaceItem[], hasMore = false, queryID = "") : MarketplaceDynamicResult {
  return {
    items,
    sources: [{ id: source, state: "live", item_count: items.length, total: offset + items.length + (hasMore ? 1 : 0), has_more: hasMore, next_offset: offset + items.length, fetched_at: new Date().toISOString() }],
    stale: false,
    total: offset + items.length + (hasMore ? 1 : 0),
    has_more: hasMore,
    next_offset: offset + items.length,
    query_id: queryID || undefined,
  };
}

afterEach(() => {
  vi.restoreAllMocks();
  document.querySelectorAll(".page-body.marketplace-page").forEach((node) => node.remove());
});

describe("mergeMarketplaceItems", () => {
  it("keeps the static baseline first and removes id and source/name duplicates", () => {
    const staticItem = item("static", "skillhub", "Same name");
    const result = mergeMarketplaceItems(
      [staticItem],
      [item("static", "skillhub", "Same name"), item("remote", "skillhub", "Same name"), item("other", "mcpservers")],
    );
    expect(result.map(({ id }) => id)).toEqual(["static", "other"]);
  });
});

describe("useMarketplaceCatalog", () => {
  it("keeps independent cursors for each selected source", async () => {
    const calls: Array<{ source?: string; offset?: number }> = [];
    vi.spyOn(api, "marketplaceCatalog").mockResolvedValue({ version: "v1", builtAt: "now", items: [] });
    vi.spyOn(api, "marketplaceDiscoverSources").mockImplementation(async (options) => {
      options ??= {};
      calls.push(options);
      const source = options.source ?? "";
      const offset = options.offset ?? 0;
      if (offset === 0) return response(source, 0, [item(`${source}-first`, source)], true, options.query_id);
      return response(source, offset, [item(`${source}-second`, source)], false, options.query_id);
    });

    const container = document.createElement("div");
    container.className = "page-body marketplace-page";
    Object.defineProperties(container, { clientHeight: { value: 600 }, scrollHeight: { value: 2000 }, scrollTop: { value: 1500, writable: true } });
    document.body.append(container);
    const { result } = renderHook(() => useMarketplaceCatalog({ autoRefresh: false }));
    await waitFor(() => expect(result.current.items).toHaveLength(2));

    await act(async () => {
      container.dispatchEvent(new Event("scroll"));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    await waitFor(() => expect(result.current.items).toHaveLength(4));
    expect(calls.filter((call) => call.offset === 0).map((call) => call.source).sort()).toEqual(["mcpservers", "skillhub"]);
    expect(calls.filter((call) => call.offset !== 0).map((call) => `${call.source}:${call.offset}`).sort()).toEqual(["mcpservers:1", "skillhub:1"]);
  });

  it("drops a late response from an older query session", async () => {
    let resolveOld: ((value: MarketplaceDynamicResult) => void) | undefined;
    const oldResult = new Promise<MarketplaceDynamicResult>((resolve) => { resolveOld = resolve; });
    vi.spyOn(api, "marketplaceCatalog").mockResolvedValue({ version: "v1", builtAt: "now", items: [] });
    vi.spyOn(api, "marketplaceDiscoverSources").mockImplementation((options) => {
      options ??= {};
      if (options.query === "old") return oldResult;
      return Promise.resolve(response(options.source ?? "skillhub", 0, [item("new", "skillhub")], false, options.query_id));
    });

    const { result, rerender } = renderHook(({ query }) => useMarketplaceCatalog({ query, autoRefresh: false }), { initialProps: { query: "old" } });
    rerender({ query: "new" });
    await waitFor(() => expect(result.current.items.map(({ id }) => id)).toContain("new"));
    resolveOld?.(response("skillhub", 0, [item("old", "skillhub")], false, "marketplace-q1"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(result.current.items.map(({ id }) => id)).toEqual(["new"]);
  });

  it("forces a network refresh while retaining the current cards", async () => {
    const deferred: { resolve?: (value: MarketplaceDynamicResult) => void } = {};
    let call = 0;
    vi.spyOn(api, "marketplaceCatalog").mockResolvedValue({ version: "v1", builtAt: "now", items: [] });
    vi.spyOn(api, "marketplaceDiscoverSources").mockImplementation((options) => {
      options ??= {};
      call += 1;
      if (call === 2) return new Promise<MarketplaceDynamicResult>((resolve) => { deferred.resolve = resolve; });
      return Promise.resolve(response(options.source ?? "skillhub", 0, [item("first", "skillhub")], false, options.query_id));
    });

    const { result } = renderHook(() => useMarketplaceCatalog({ autoRefresh: false, sources: ["skillhub"] }));
    await waitFor(() => expect(result.current.items.map(({ id }) => id)).toContain("first"));
    act(() => result.current.refresh());
    await waitFor(() => expect(call).toBeGreaterThan(1));
    expect(result.current.items.map(({ id }) => id)).toContain("first");
    const pendingCall = vi.mocked(api.marketplaceDiscoverSources).mock.calls[1][0]!;
    expect(pendingCall.force_refresh).toBe(true);
    deferred.resolve?.(response("skillhub", 0, [item("second", "skillhub")], false, pendingCall.query_id));
    await waitFor(() => expect(result.current.items.map(({ id }) => id)).toContain("second"));
  });

  it("does not carry the force-refresh flag into a new search session", async () => {
    const forceFlags: boolean[] = [];
    vi.spyOn(api, "marketplaceCatalog").mockResolvedValue({ version: "v1", builtAt: "now", items: [] });
    vi.spyOn(api, "marketplaceDiscoverSources").mockImplementation(async (options) => {
      options ??= {};
      forceFlags.push(Boolean(options.force_refresh));
      return response(options.source ?? "skillhub", 0, [item(options.query ? "searched" : "initial", "skillhub")], false, options.query_id);
    });
    const { result, rerender } = renderHook(({ query }) => useMarketplaceCatalog({ query, autoRefresh: false, sources: ["skillhub"] }), { initialProps: { query: "" } });
    await waitFor(() => expect(result.current.items.map(({ id }) => id)).toContain("initial"));
    act(() => result.current.refresh());
    await waitFor(() => expect(forceFlags).toHaveLength(2));
    rerender({ query: "searched" });
    await waitFor(() => expect(result.current.items.map(({ id }) => id)).toContain("searched"));
    expect(forceFlags).toEqual([false, true, false]);
  });

  it("continues after a locally filtered page with no cards", async () => {
    const calls: number[] = [];
    vi.spyOn(api, "marketplaceCatalog").mockResolvedValue({ version: "v1", builtAt: "now", items: [] });
    vi.spyOn(api, "marketplaceDiscoverSources").mockImplementation(async (options) => {
      options ??= {};
      const offset = options.offset ?? 0;
      calls.push(offset);
      if (offset === 0) {
        return {
          items: [],
          sources: [{ id: "skillhub", state: "live", item_count: 0, total: 2, has_more: true, next_offset: 50 }],
          stale: false,
          total: 2,
          has_more: true,
          next_offset: 50,
          query_id: options.query_id,
        };
      }
      return response("skillhub", offset, [item("found-after-filter", "skillhub")], false, options.query_id);
    });

    const container = document.createElement("div");
    container.className = "page-body marketplace-page";
    Object.defineProperties(container, { clientHeight: { value: 600 }, scrollHeight: { value: 2000 }, scrollTop: { value: 1500, writable: true } });
    document.body.append(container);
    const { result } = renderHook(() => useMarketplaceCatalog({ autoRefresh: false, sources: ["skillhub"], category: "skill", facetKey: "scene=coding" }));
    await waitFor(() => expect(calls).toContain(0));
    await act(async () => {
      container.dispatchEvent(new Event("scroll"));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    await waitFor(() => expect(result.current.items.map(({ id }) => id)).toContain("found-after-filter"));
    expect(calls).toEqual([0, 50]);
  });

  it("auto-fills a short filtered viewport until a later page has a match", async () => {
    const calls: number[] = [];
    let hasVisibleCard = false;
    vi.spyOn(api, "marketplaceCatalog").mockResolvedValue({ version: "v1", builtAt: "now", items: [] });
    vi.spyOn(api, "marketplaceDiscoverSources").mockImplementation(async (options) => {
      options ??= {};
      const offset = options.offset ?? 0;
      calls.push(offset);
      if (offset === 0) {
        return {
          items: [],
          sources: [{ id: "skillhub", state: "live", item_count: 0, total: 100, has_more: true, next_offset: 50 }],
          stale: false,
          total: 100,
          has_more: true,
          next_offset: 50,
          query_id: options.query_id,
        };
      }
      hasVisibleCard = true;
      return response("skillhub", offset, [item("found-by-facet", "skillhub")], false, options.query_id);
    });

    const container = document.createElement("div");
    container.className = "page-body marketplace-page";
    Object.defineProperties(container, {
      clientHeight: { value: 600 },
      scrollHeight: { get: () => hasVisibleCard ? 2000 : 0 },
      scrollTop: { value: 0, writable: true },
    });
    document.body.append(container);

    const { result } = renderHook(() => useMarketplaceCatalog({
      autoRefresh: false,
      sources: ["skillhub"],
      category: "skill",
      facetKey: "scene=coding",
    }));
    await waitFor(() => expect(result.current.items.map(({ id }) => id)).toContain("found-by-facet"));
    expect(calls).toEqual([0, 50]);
  });
});
