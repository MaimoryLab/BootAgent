import type { AgentCatalogItem, ProfileSummary, StatusResponse } from "../types/api";

/**
 * Display prominence, shared by the overview and the setup selection page.
 *
 * Rank says how widely an Agent is used; it is deliberately independent of
 * whether BootAgent can configure it. Ordering by catalog group instead put Kilo
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

/**
 * Sorts Providers: user-defined first, newest to oldest, then built-ins in the
 * manifest's `order`.
 *
 * The two halves sort by different keys on purpose. Built-ins used to fall
 * through to `name`, so the list read DeepSeek, Moonshot, Novita, PPIO --
 * alphabetical order presented as a recommendation, which also decided which
 * Provider a protocol matched first. `order` comes from providers.lock.json, so
 * that sequence now lives in the manifest beside the endpoints instead of being
 * implied by vendor names.
 *
 * `created_at` is deliberately not consulted for built-ins: Store.Public stamps
 * it onto any Provider with a saved key, built-in ones included, so comparing it
 * first would let saving a key reorder the built-in list -- and only on the
 * machines where someone had done so.
 */
export function byProviderCreatedAt(providers: StatusResponse["providers"]): Array<[string, StatusResponse["providers"][string]]> {
  return Object.entries(providers).sort(([, first], [, second]) => {
    if (isCustom(first) !== isCustom(second)) return isCustom(first) ? -1 : 1;
    if (isCustom(first)) {
      return (second.created_at || "").localeCompare(first.created_at || "")
        || first.name.localeCompare(second.name);
    }
    // Name still breaks ties: `order` is required of every built-in in the
    // manifest, but this DTO cannot promise that, and an unordered pair must
    // not render in a different sequence on each status refresh.
    return (first.order ?? 0) - (second.order ?? 0) || first.name.localeCompare(second.name);
  });
}

/**
 * The Provider to pre-select out of those that can serve a step, preferring one
 * that already holds a key.
 *
 * Display order and pre-selection are different questions, and answering both
 * with byProviderCreatedAt alone conflated them: Store.Public stamps created_at
 * onto any Provider holding a key, so sorting built-ins by it descending floated
 * the configured one up as a side effect. Ordering built-ins by the manifest is
 * correct for display and took that side effect with it, which left the wizard
 * pre-selecting a keyless Provider and its connection test permanently disabled.
 *
 * Callers pass the candidates they consider usable, already sorted; this only
 * decides which of them to land on.
 */
export function preferProviderWithKey<T extends { has_key?: boolean }>(
  candidates: ReadonlyArray<[string, T]>,
): [string, T] | undefined {
  return candidates.find(([, provider]) => provider.has_key) ?? candidates[0];
}

export function byProfileCreatedAt(profiles: readonly ProfileSummary[]): ProfileSummary[] {
  return [...profiles].sort((a, b) => (b.createdAt || "").localeCompare(a.createdAt || ""));
}
