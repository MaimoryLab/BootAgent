import { useI18n } from "../i18n";
import { AgentIcon } from "./icons/agents";

interface AgentTargetGroupProps {
  /** Eligible Agent ids, already ordered by the backend scan. */
  agents: string[];
  /** Agents currently targeted; anything not listed renders dimmed. */
  selected: string[];
  onToggle: (agent: string, checked: boolean) => void;
  disabled?: boolean;
  /** Display names from the catalog; an id without one shows the id itself. */
  labels?: Record<string, string>;
}

/**
 * One toggle button per eligible Agent, identified by its mark instead of a
 * text checkbox. Replaces the stacked `.mcp-targets` label column so a row's
 * distribution reads horizontally at a constant height. Selection semantics
 * are unchanged: toggling edits the page's draft state, and nothing is
 * written until the page-level Apply.
 */
export function AgentTargetGroup({ agents, selected, onToggle, disabled = false, labels = {} }: AgentTargetGroupProps) {
  const { t } = useI18n();
  return (
    <div className="agent-target-group" role="group" aria-label={t("选择目标 Agent")}>
      {agents.map((agent) => {
        const on = selected.includes(agent);
        const label = labels[agent] || agent;
        return (
          <button
            key={agent}
            type="button"
            className={`agent-target-toggle${on ? " is-on" : ""}`}
            aria-pressed={on}
            aria-label={label}
            title={on ? t("{name}：已选择", { name: label }) : t("{name}：未选择", { name: label })}
            disabled={disabled}
            onClick={() => onToggle(agent, !on)}
          >
            <AgentIcon agentId={agent} size={16} />
          </button>
        );
      })}
    </div>
  );
}
