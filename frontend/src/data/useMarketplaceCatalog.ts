/**
 * Runtime marketplace catalog with a static-snapshot fallback.
 *
 * On first mount anywhere in the app, fetches the skillhub showcase feed and
 * swaps the skillhub portion of the catalog for live data. The mcpservers
 * portion always stays static — mcpservers.org has no JSON API.
 *
 * The feed comes through the Go MarketplaceService proxy: api.skillhub.cn
 * only echoes CORS headers for skillhub's own origins, so a direct browser
 * fetch is blocked in the packaged app. The direct fetch remains as the
 * fallback for environments without a Wails bridge (vite dev in a plain
 * browser, Playwright), where it works whenever CORS happens to allow it.
 *
 * A module-scope promise guarantees the fetch happens at most once per app
 * session even though both the list page and the detail page call this hook.
 * Any failure (network, timeout, bad payload) silently keeps the snapshot.
 */

import { useEffect, useState } from "react";

import { api } from "../backend/api";
import type { MarketplaceItem } from "../types/marketplace";
import { STATIC_CATALOG } from "./marketplace-catalog";
import { mapSkillhubEntry, type SkillhubEntry } from "./skillhub-adapter";

const SHOWCASE_URL = "https://api.skillhub.cn/api/v1/showcase/hot";
const FETCH_TIMEOUT_MS = 8000;

// ── live payload shapes (fields we read; everything optional, data is remote) ─

interface ShowcaseSubCategory {
  key?: string;
  name?: string;
}

export interface ShowcaseSkill {
  slug?: string;
  name?: string;
  /** English description; description_zh carries the Chinese one */
  description?: string;
  description_zh?: string;
  iconUrl?: string;
  category?: string;
  /** Live API sends [{key,name}] objects; the snapshot uses plain strings */
  subCategories?: (ShowcaseSubCategory | string)[];
  stars?: number;
  downloads?: number;
  score?: number;
  /** Live API serialises this flag as the string "true"/"false" */
  labels?: { requires_api_key?: string | boolean };
  namespace?: { canonicalName?: string };
  homepage?: string;
}

/**
 * Normalises one live showcase skill into the snapshot entry shape so both
 * paths share mapSkillhubEntry. Returns null for unusable records.
 */
export function normalizeShowcaseSkill(raw: ShowcaseSkill): SkillhubEntry | null {
  if (!raw || typeof raw.slug !== "string" || !raw.slug || typeof raw.name !== "string" || !raw.name) {
    return null;
  }
  const requires = raw.labels?.requires_api_key;
  return {
    id: raw.slug,
    name: raw.name,
    description: raw.description_zh || raw.description || "",
    descriptionEn: raw.description || undefined,
    iconUrl: raw.iconUrl ?? "",
    category: raw.category ?? "",
    subCategories: (raw.subCategories ?? [])
      .map((sc) => (typeof sc === "string" ? sc : sc?.key ?? ""))
      .filter((key): key is string => key.length > 0),
    stars: raw.stars ?? 0,
    downloads: raw.downloads ?? 0,
    score: raw.score ?? 0,
    requiresApiKey: requires === true || requires === "true",
    namespace: raw.namespace?.canonicalName ?? raw.slug,
    homepage: raw.homepage,
  };
}

// ── module-scope single-flight cache ─────────────────────────────────────────

interface CatalogState {
  items: MarketplaceItem[];
  live: boolean;
}

const SNAPSHOT: CatalogState = { items: STATIC_CATALOG.items, live: false };

let resolved: CatalogState | null = null;
let pending: Promise<CatalogState> | null = null;

/**
 * Reads the showcase feed: Go proxy first, direct fetch second. Throws when
 * both paths fail; the caller turns that into the snapshot.
 */
async function fetchShowcasePayload(): Promise<unknown> {
  try {
    return JSON.parse(await api.marketplaceShowcase()) as unknown;
  } catch {
    // No Wails bridge, or the proxy failed. The direct fetch is CORS-blocked
    // in the packaged app but works from plain-browser dev environments.
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
    try {
      const res = await fetch(SHOWCASE_URL, { signal: controller.signal });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return await res.json();
    } finally {
      clearTimeout(timer);
    }
  }
}

async function fetchLiveCatalog(): Promise<CatalogState> {
  const payload = await fetchShowcasePayload();
  const skills = Array.isArray((payload as { skills?: unknown })?.skills)
    ? ((payload as { skills: ShowcaseSkill[] }).skills)
    : [];
  const liveSkillhub = skills
    .map(normalizeShowcaseSkill)
    .filter((entry): entry is SkillhubEntry => entry !== null)
    .map(mapSkillhubEntry);
  if (liveSkillhub.length === 0) throw new Error("empty showcase payload");
  return {
    items: [
      ...liveSkillhub,
      ...STATIC_CATALOG.items.filter((item) => item.source !== "skillhub"),
    ],
    live: true,
  };
}

function ensureCatalog(): Promise<CatalogState> {
  if (!pending) {
    // Failures cache the snapshot result too: one attempt per app session.
    pending = fetchLiveCatalog()
      .catch(() => SNAPSHOT)
      .then((state) => {
        resolved = state;
        return state;
      });
  }
  return pending;
}

/**
 * Returns the marketplace catalog: the static snapshot immediately, then the
 * live skillhub data once (and if) the showcase fetch succeeds.
 */
export function useMarketplaceCatalog(): CatalogState {
  const [state, setState] = useState<CatalogState>(() => resolved ?? SNAPSHOT);

  useEffect(() => {
    if (resolved) return;
    let cancelled = false;
    void ensureCatalog().then((next) => {
      if (!cancelled) setState(next);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return state;
}
