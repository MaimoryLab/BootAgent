import { ArrowUpRight } from "lucide-react";

import { useI18n } from "../i18n";
import type { AgentCatalogItem, AgentStatus } from "../types/api";
import { AgentIcon, agentTagline } from "./icons/agents";
import { StatusBadge } from "./StatusBadge";

/**
 * A widely-used Agent that OneAgent does not install or configure.
 *
 * These sit in the main list rather than a footnote, because the list is judged
 * by whether the tools people actually use appear on it. What differs is the
 * action: the row states how the Agent is obtained instead of offering a form.
 *
 * Cursor and Kiro are installed as applications and authenticate through their
 * own accounts; OpenClaw and Hermes run as resident gateways, which ADR-002 and
 * ADR-003 place outside this product's scope. Neither is a licensing problem —
 * both are MIT — so the row says what it is, not that it is unavailable.
 */
export function GuideOnlyRow({
  agentId,
  catalog,
  status,
}: {
  agentId: string;
  catalog: AgentCatalogItem;
  status: AgentStatus | undefined;
}) {
  const { t } = useI18n();
  const detected = Boolean(status?.installed);
  return (
    <div className="agent-manage-row is-guide">
      <span className="agent-icon" title={agentTagline(agentId, t) || undefined}>
        <AgentIcon agentId={agentId} size={18} />
      </span>
      <span className="agent-manage-identity">
        <strong>{catalog.name}</strong>
        <span className="agent-manage-target">
          {catalog.platformNote || t("按官方方式安装与登录")}
        </span>
      </span>
      <StatusBadge tone={detected ? "success" : "neutral"}>
        {detected ? t("已检测到") : t("官方安装")}
      </StatusBadge>
      <span className="agent-manage-cta is-static">
        {t("官方文档")}
        <ArrowUpRight size={14} aria-hidden="true" />
      </span>
    </div>
  );
}
