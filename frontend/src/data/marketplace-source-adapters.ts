import { api } from "../backend/api";
import type { MarketplaceItem } from "../types/marketplace";
import { mapSkillhubEntry, type SkillhubEntry } from "./skillhub-adapter";

const SHOWCASE_URL = "https://api.skillhub.cn/api/v1/showcase/hot";
const FETCH_TIMEOUT_MS = 8000;

interface ShowcaseSubCategory { key?: string; name?: string }

export interface ShowcaseSkill {
  slug?: string;
  name?: string;
  description?: string;
  description_zh?: string;
  iconUrl?: string;
  category?: string;
  subCategories?: (ShowcaseSubCategory | string)[];
  stars?: number;
  downloads?: number;
  score?: number;
  labels?: { requires_api_key?: string | boolean };
  namespace?: { canonicalName?: string };
  homepage?: string;
}

export function normalizeShowcaseSkill(raw: ShowcaseSkill): SkillhubEntry | null {
  if (!raw || typeof raw.slug !== "string" || !raw.slug || typeof raw.name !== "string" || !raw.name) return null;
  const requires = raw.labels?.requires_api_key;
  return {
    id: raw.slug,
    name: raw.name,
    description: raw.description_zh || raw.description || "",
    descriptionEn: raw.description || undefined,
    iconUrl: raw.iconUrl ?? "",
    category: raw.category ?? "",
    subCategories: (raw.subCategories ?? [])
      .map((value) => typeof value === "string" ? value : value?.key ?? "")
      .filter((value): value is string => value.length > 0),
    stars: raw.stars ?? 0,
    downloads: raw.downloads ?? 0,
    score: raw.score ?? 0,
    requiresApiKey: requires === true || requires === "true",
    namespace: raw.namespace?.canonicalName ?? raw.slug,
    homepage: raw.homepage,
  };
}

async function fetchShowcasePayload(): Promise<unknown> {
  try {
    return JSON.parse(await api.marketplaceShowcase()) as unknown;
  } catch {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
    try {
      const response = await fetch(SHOWCASE_URL, { signal: controller.signal });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      return await response.json();
    } finally {
      clearTimeout(timer);
    }
  }
}

export async function loadSkillhub(): Promise<MarketplaceItem[]> {
  const payload = await fetchShowcasePayload();
  const skills = Array.isArray((payload as { skills?: unknown })?.skills)
    ? (payload as { skills: ShowcaseSkill[] }).skills
    : [];
  const items = skills.map(normalizeShowcaseSkill).filter((item): item is SkillhubEntry => item !== null).map(mapSkillhubEntry);
  if (items.length === 0) throw new Error("empty showcase payload");
  return items;
}
