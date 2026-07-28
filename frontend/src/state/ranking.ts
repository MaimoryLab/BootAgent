import type { AgentCatalogItem } from "../types/api";

/**
 * Display prominence, shared by the overview and the setup selection page.
 *
 * Rank says how widely an Agent is used; it is deliberately independent of
 * whether OneAgent can configure it. Ordering by catalog group instead put Kilo
 * and Aider at the top of the page while Cursor and OpenClaw sat inside a
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
