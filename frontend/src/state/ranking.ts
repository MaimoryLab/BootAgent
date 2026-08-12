import type { AgentCatalogItem, ProfileSummary, StatusResponse } from "../types/api";

/**
 * Display prominence, shared by the overview and the setup selection page.
 *
 * Rank says how widely an Agent is used; it is deliberately independent of
 * whether OneAgent can configure it. Ordering by catalog group instead put Kilo
 * and Aider at the top of the page while more widely used Agents sat inside a
 * disclosure, which misrepresented what a machine actually runs.
 */

/** Agents at or above this rank lead a page; the rest go behind a disclosure. */
export const PRIMARY_RANK_LIMIT = 6;

/**
 * Sorted by rank, ties broken by id.
 *
 * The server already sorts, but the response type does not promise an order, so
 * a layout that depended on it would break silently the day that changed.
 */
export function byRank(catalog: readonly AgentCatalogItem[] | undefined): AgentCatalogItem[] {
  return [...(catalog ?? [])].sort((a, b) => a.rank - b.rank || a.id.localeCompare(b.id));
}

/** Ranked catalog split into the leading rows and the ones behind a disclosure. */
export function splitByRank(catalog: readonly AgentCatalogItem[] | undefined): {
  primary: AgentCatalogItem[];
  secondary: AgentCatalogItem[];
} {
  const ranked = byRank(catalog);
  return {
    primary: ranked.filter((item) => item.rank <= PRIMARY_RANK_LIMIT),
    secondary: ranked.filter((item) => item.rank > PRIMARY_RANK_LIMIT),
  };
}

/** User-defined Providers sort ahead of built-in ones.
 *
 *  Read from the DTO rather than a hardcoded ID list. The list used to name
 *  ["ppio", "novita"] here, which silently mis-sorted every Provider added to
 *  providers.lock.json afterwards: an unlisted built-in counted as user-defined
 *  and jumped the queue, so the first Provider matching a protocol was no longer
 *  the intended one. The backend already answers this question -- `custom` is
 *  `!builtIn` in provider.Store.Public, omitted for built-ins -- so deriving it
 *  keeps the two from drifting apart. */
const isCustom = (provider: StatusResponse["providers"][string]) => provider.custom === true;

export function byProviderCreatedAt(providers: StatusResponse["providers"]): Array<[string, StatusResponse["providers"][string]]> {
  return Object.entries(providers).sort(([, first], [, second]) =>
    isCustom(first) !== isCustom(second)
      ? (isCustom(first) ? -1 : 1)
      : (second.created_at || "").localeCompare(first.created_at || "") || first.name.localeCompare(second.name),
  );
}

export function byProfileCreatedAt(profiles: readonly ProfileSummary[]): ProfileSummary[] {
  return [...profiles].sort((a, b) => (b.createdAt || "").localeCompare(a.createdAt || ""));
}
