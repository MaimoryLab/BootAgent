import { Bot, Box, Code2, MonitorCog, Network, TerminalSquare } from "lucide-react";

import type { AgentCatalogItem, AgentStatus } from "../types/api";
import { StatusBadge } from "./StatusBadge";

const groupIcons = {
  auto: Code2,
  gateway: Network,
  platform: MonitorCog,
  ide: Box,
} as const;

interface AgentRowProps {
  agent: AgentCatalogItem;
  status?: AgentStatus;
  selected: boolean;
  onToggle: () => void;
}

export function AgentRow({ agent, status, selected, onToggle }: AgentRowProps) {
  const Icon = groupIcons[agent.group] || Bot;
  const supported = agent.platforms.length > 0;
  const statusLabel = status?.installed ? "已安装" : agent.guideOnly ? "仅引导" : "待安装";
  const statusTone = status?.installed ? "success" : agent.guideOnly ? "neutral" : "warning";

  return (
    <label className={`agent-row${selected ? " is-selected" : ""}${!supported ? " is-disabled" : ""}`}>
      <input
        type="checkbox"
        checked={selected}
        onChange={onToggle}
        disabled={!supported}
        aria-label={`选择 ${agent.name}`}
      />
      <span className="agent-icon" aria-hidden="true">
        {agent.id === "claude-code" ? <TerminalSquare size={20} /> : <Icon size={20} />}
      </span>
      <span className="agent-copy">
        <span className="agent-name-line">
          <strong>{agent.name}</strong>
          {agent.lockedVersion ? <span className="agent-version">v{agent.lockedVersion}</span> : null}
        </span>
        <span>{agent.guideOnly ? "显示官方安装与配置步骤" : "支持检测、安装与初始化配置"}</span>
        {agent.platformNote ? <small>{agent.platformNote}</small> : null}
      </span>
      <StatusBadge tone={statusTone}>{statusLabel}</StatusBadge>
    </label>
  );
}
