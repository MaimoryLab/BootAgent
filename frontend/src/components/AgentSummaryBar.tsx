import { useI18n } from "../i18n";
import { AgentIcon } from "./icons/agents";

interface AgentSummaryBarProps {
  /** Eligible Agent ids, in the same order the row toggles use. */
  agents: string[];
  /** Per-agent count of rows currently targeting it. */
  counts: Record<string, number>;
  /** Number of rows the counts are measured against (deleted rows excluded). */
  total: number;
  /** Bulk-edits the draft for every counted row; nothing is written until Apply. */
  onToggleAll: (agent: string, enabled: boolean) => void;
  disabled?: boolean;
  labels?: Record<string, string>;
}

/**
 * Per-agent tallies over the visible rows, doubling as tri-state bulk
 * toggles: none / partial (aria-checked="mixed") / all. Clicking targets
 * every counted row at once, still inside the page's draft model.
 */
export function AgentSummaryBar({ agents, counts, total, onToggleAll, disabled = false, labels = {} }: AgentSummaryBarProps) {
  const { t } = useI18n();
  if (!agents.length) return null;
  return (
    <div className="agent-summary-bar">
      {agents.map((agent) => {
        const count = counts[agent] ?? 0;
        const all = total > 0 && count >= total;
        const partial = count > 0 && !all;
        const label = labels[agent] || agent;
        const action = all ? t("为全部条目取消 {name}", { name: label }) : t("为全部条目选择 {name}", { name: label });
        return (
          <button
            key={agent}
            type="button"
            role="checkbox"
            aria-checked={partial ? "mixed" : all}
            aria-label={action}
            title={action}
            className={`agent-summary-chip${all ? " is-all" : partial ? " is-partial" : ""}`}
            disabled={disabled || total === 0}
            onClick={() => onToggleAll(agent, !all)}
          >
            <AgentIcon agentId={agent} size={14} />
            <span className="agent-summary-count">{count}</span>
          </button>
        );
      })}
    </div>
  );
}
