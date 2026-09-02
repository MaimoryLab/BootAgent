/** Local-only marketplace counters. Nothing is sent off-device. */
const STORAGE_KEY = "bootagent.marketplace.telemetry.v1";
type MarketplaceEvent = "source_refresh" | "item_open" | "filter_change" | "install_prompt_copy";

export function recordMarketplaceEvent(event: MarketplaceEvent, source?: string): void {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    const data = raw ? JSON.parse(raw) as Record<string, number> : {};
    const key = source ? `${event}:${source}` : event;
    data[key] = (data[key] ?? 0) + 1;
    localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
  } catch {
    // Telemetry must never affect marketplace behavior, including private mode.
  }
}
