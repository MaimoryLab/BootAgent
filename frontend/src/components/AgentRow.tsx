import { useI18n } from "../i18n";
import type { AgentCatalogItem, AgentStatus } from "../types/api";
import { AgentIcon, agentTagline } from "./icons/agents";
import { StatusBadge } from "./StatusBadge";

interface AgentRowProps {
  agent: AgentCatalogItem;
  status?: AgentStatus;
  selected: boolean;
  onToggle: () => void;
}

export function AgentRow({ agent, status, selected, onToggle }: AgentRowProps) {
  const { t } = useI18n();
  const supported = agent.platforms.length > 0;
  const statusLabel = status?.installed ? t("已安装") : agent.guideOnly ? t("仅引导") : t("待安装");
  const statusTone = status?.installed ? "success" : agent.guideOnly ? "neutral" : "warning";

  return (
    <label className={`agent-row${selected ? " is-selected" : ""}${!supported ? " is-disabled" : ""}`}>
      <input
        type="checkbox"
        checked={selected}
        onChange={onToggle}
        disabled={!supported}
        aria-label={t("选择 {name}", { name: agent.name })}
      />
      <span className="agent-icon" title={agentTagline(agent.id, t) || undefined}>
        <AgentIcon agentId={agent.id} size={20} />
      </span>
      <span className="agent-copy">
        <span className="agent-name-line">
          <strong>{agent.name}</strong>
          {agent.lockedVersion ? <span className="agent-version">v{agent.lockedVersion}</span> : null}
        </span>
        <span>{agent.guideOnly ? t("显示官方安装与配置步骤") : t("支持检测、安装与初始化配置")}</span>
        {agent.platformNote ? <small>{agent.platformNote}</small> : null}
      </span>
      <StatusBadge tone={statusTone}>{statusLabel}</StatusBadge>
    </label>
  );
}
