import type { Translate } from "../i18n";
import type { FeedbackInput } from "./FeedbackContext";

/**
 * The receipt for saving a Provider or Profile.
 *
 * A save also rewrites every Agent already using what was saved, and both
 * editors used to report that as a sentence written into the page above or
 * below the list -- "Codex 重新应用失败：…" -- which moved the list. This says the
 * same thing as one receipt (docs/ui-interaction-plan.md §3.3): a short line
 * naming the outcome, the Agents and the backend's reasons behind 详情.
 */
export function saveReceipt({
  key,
  name,
  result,
  nameOf,
  t,
  separator,
}: {
  key: string;
  /** What was saved, as the user named it. */
  name: string;
  result: { reapplied?: string[] | null; failures?: Record<string, string | undefined> | null };
  nameOf: (agentId: string) => string;
  t: Translate;
  separator: string;
}): FeedbackInput {
  const failures = Object.entries(result.failures ?? {});
  if (failures.length) {
    return {
      key,
      tone: "failure",
      message: t("已保存，{count} 个 Agent 未能更新", { count: failures.length }),
      detail: failures.map(([agentId, message]) => (message ? `${nameOf(agentId)}: ${message}` : nameOf(agentId))).join("\n"),
    };
  }
  const reapplied = result.reapplied ?? [];
  if (reapplied.length) {
    return {
      key,
      tone: "success",
      message: t("已保存，{count} 个 Agent 已更新", { count: reapplied.length }),
      detail: reapplied.map(nameOf).join(separator),
    };
  }
  return { key, tone: "success", message: t("已保存「{name}」", { name }) };
}
