import { useI18n, type TranslationKey } from "../i18n";

const LABELS: Record<string, TranslationKey> = {
  skill: "Skill", mcp: "MCP", plugin: "插件", "agent-product": "独立 AI 产品",
  "prompt-template": "提示词模板", "workflow-script": "工作流", content: "内容", "external-link": "外部工具",
};

/** Neutral classification badge; status colours are reserved for state. */
export function MarketplaceKindBadge({ kind }: { kind: string }) {
  const { t } = useI18n();
  return <span className="marketplace-kind-badge" title={LABELS[kind] ? t(LABELS[kind]) : kind}>{LABELS[kind] ? t(LABELS[kind]) : kind}</span>;
}
