import type { FeedbackInput } from "./FeedbackContext";

/** One line of an MCP or Skill apply result: an Agent, or none for a change rejected before any Agent. */
export interface ApplyItem {
  agent?: string;
  error?: string;
}

/**
 * The receipt for applying MCP or Skill changes.
 *
 * Both services answer with one line per Agent touched and report failures in
 * those lines rather than by throwing, so a partly failed apply used to look
 * exactly like a successful one: the button did nothing visible either way
 * (docs/ui-interaction-plan.md I-08). This counts the failed lines into one
 * receipt and puts each Agent's reason behind 详情.
 */
export function applyReceipt({
  key,
  results,
  success,
  failure,
  nameOf,
}: {
  key: string;
  results: ApplyItem[] | null | undefined;
  success: Pick<FeedbackInput, "message" | "detail">;
  /** The headline for `count` failed lines. */
  failure: (count: number) => string;
  nameOf: (agentId: string) => string;
}): FeedbackInput {
  const failed = (results ?? []).filter((item) => item.error);
  if (!failed.length) return { key, tone: "success", ...success };
  return {
    key,
    tone: "failure",
    message: failure(failed.length),
    detail: failed.map((item) => (item.agent ? `${nameOf(item.agent)}: ${item.error}` : item.error)).join("\n"),
  };
}
