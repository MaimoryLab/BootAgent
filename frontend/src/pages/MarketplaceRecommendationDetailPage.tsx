import { ArrowLeft, ExternalLink, FileText, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { EmptyState } from "../components/EmptyState";
import { PageScaffold } from "../components/PageScaffold";
import { StatusBadge } from "../components/StatusBadge";
import { useI18n, type TranslationKey } from "../i18n";
import type { MarketplaceRecommendationHistory, MarketplaceRecommendationSnapshot } from "../types/api";
import { useMarketplaceCatalog } from "../data/useMarketplaceCatalog";
import { marketplaceKinds } from "../data/marketplace-taxonomy";

const KIND_LABELS: Record<string, TranslationKey> = {
  skill: "Skill", mcp: "MCP", plugin: "插件", "agent-product": "独立 AI 产品",
  "prompt-template": "提示词模板", "workflow-script": "工作流", content: "内容", "external-link": "外部工具",
};

export function MarketplaceRecommendationDetailPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { historyId = "" } = useParams();
  const { items } = useMarketplaceCatalog();
  const [history, setHistory] = useState<MarketplaceRecommendationHistory | null>(null);
  const [failure, setFailure] = useState("");
  useEffect(() => {
    void api.listRecommendationHistory().then((records) => setHistory(records.find((record) => record.id === historyId) ?? null)).catch((error) => setFailure(describeFailure(error, t("无法读取推荐历史"), t).message));
  }, [historyId, t]);

  const currentByID = useMemo(() => new Map(items.map((item) => [item.id, item])), [items]);
  const remove = async () => {
    if (!history) return;
    try { await api.deleteRecommendationHistory(history.id); navigate("/marketplace"); }
    catch (error) { setFailure(describeFailure(error, t("无法删除推荐历史"), t).message); }
  };

  if (failure) return <PageScaffold title={t("推荐结果")} onBack={() => navigate(-1)}><p className="settings-field-error" role="alert">{failure}</p></PageScaffold>;
  if (!history) return <PageScaffold title={t("推荐结果")} onBack={() => navigate(-1)}><EmptyState icon={FileText} title={t("推荐记录不存在")} hint={t("这条记录可能已被删除")} /></PageScaffold>;

  return (
    <PageScaffold title={t("推荐结果")} description={history.need} onBack={() => navigate(-1)} backLabel={t("返回推荐历史")} footerNote={<span>{new Date(history.created_at).toLocaleString()} · {history.agent_id} · {history.results?.length ?? 0} {t("个结果")}</span>}>
      <div className="marketplace-recommend-detail-header"><span>{t("推荐目录版本")} {history.catalog_version}</span><button className="button button-danger" type="button" onClick={() => void remove()}><Trash2 size={14} />{t("删除记录")}</button></div>
      <ul className="marketplace-recommend-detail-list">
        {(history.results ?? []).map((result: MarketplaceRecommendationSnapshot) => {
          const current = currentByID.get(result.item_id);
          const kind = current ? marketplaceKinds(current) : [result.category];
          return <li key={result.item_id} className="marketplace-recommend-detail-card"><div className="marketplace-recommend-detail-icon"><FileText size={22} /></div><div className="marketplace-recommend-detail-body"><div className="marketplace-recommend-detail-title"><strong>{result.name}</strong>{kind.map((value) => <StatusBadge key={value} tone="neutral">{KIND_LABELS[value] ? t(KIND_LABELS[value]) : value}</StatusBadge>)}</div><p>{result.reason}</p><small>{result.source || t("来源未知")}</small></div>{current ? <Link className="button button-secondary" to={`/marketplace/${encodeURIComponent(result.item_id)}`}><ExternalLink size={14} />{t("查看工具")}</Link> : <span className="marketplace-history-unavailable">{t("当前工具不可用")}</span>}</li>;
        })}
      </ul>
    </PageScaffold>
  );
}
