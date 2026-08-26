import type { MarketplaceIconName, MarketplaceItem } from "../types/marketplace";

const TRUSTED_ICON_HOSTS = new Set(["github.com", "www.github.com", "icons.duckduckgo.com"]);

function isHttps(value: string): boolean {
  try {
    return new URL(value).protocol === "https:";
  } catch {
    return false;
  }
}

function githubAvatar(repositoryUrl?: string): string | undefined {
  if (!repositoryUrl || !isHttps(repositoryUrl)) return undefined;
  try {
    const url = new URL(repositoryUrl);
    if (url.hostname !== "github.com") return undefined;
    const owner = url.pathname.split("/").filter(Boolean)[0];
    return owner ? `https://github.com/${encodeURIComponent(owner)}.png?size=64` : undefined;
  } catch {
    return undefined;
  }
}

function favicon(urlValue?: string): string | undefined {
  if (!urlValue || !isHttps(urlValue)) return undefined;
  try {
    const hostname = new URL(urlValue).hostname;
    if (!hostname || hostname === "localhost" || hostname.endsWith(".local")) return undefined;
    return `https://icons.duckduckgo.com/ip3/${encodeURIComponent(hostname)}.ico`;
  } catch {
    return undefined;
  }
}

/**
 * Returns remote icon candidates in descending reliability order. The caller
 * must still handle image errors and fall back to the local Lucide token.
 */
export function marketplaceIconCandidates(item: Pick<MarketplaceItem, "iconUrl" | "repositoryUrl" | "externalUrl" | "sourceUrl" | "documentationUrl">): string[] {
  const candidates = [
    item.iconUrl,
    githubAvatar(item.repositoryUrl),
    favicon(item.externalUrl),
    favicon(item.documentationUrl),
    favicon(item.sourceUrl),
  ].filter((value): value is string => Boolean(value));
  return [...new Set(candidates)].filter((value) => {
    try {
      return TRUSTED_ICON_HOSTS.has(new URL(value).hostname) || value.startsWith("https://raw.githubusercontent.com/") || value.startsWith("https://cloudcache.tencent-cloud.com/");
    } catch {
      return false;
    }
  });
}

/** Derives the first stable icon URL for adapters that build catalog entries. */
export function marketplaceIconUrl(input: Pick<MarketplaceItem, "repositoryUrl" | "externalUrl" | "sourceUrl" | "documentationUrl">): string | undefined {
  return marketplaceIconCandidates({ ...input, iconUrl: undefined })[0];
}

/** Every card has a local icon token even if all remote candidates fail. */
export function marketplaceIconFallback(icon: MarketplaceIconName): MarketplaceIconName {
  return icon;
}
