import { ArrowUpRight, ShieldCheck, Sparkles, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { useI18n } from "../i18n";
import type { MarketplaceRecommendationAgent } from "../types/api";
import type { MarketplaceItem } from "../types/marketplace";
import { ModalDialog } from "./ModalDialog";
import { SelectField } from "./SelectField";

export function MarketplaceRecommendationDialog({ items, catalogVersion, onDismiss }: {
  items: MarketplaceItem[];
  catalogVersion: string;
  onDismiss: () => void;
}) {
  const { t, locale } = useI18n();
  const [agents, setAgents] = useState<MarketplaceRecommendationAgent[]>([]);
  const [agentID, setAgentID] = useState("");
  const [need, setNeed] = useState("");
  const [loadingAgents, setLoadingAgents] = useState(true);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [recommendations, setRecommendations] = useState<Array<{ item: MarketplaceItem; reason: string }>>([]);

  useEffect(() => {
    let cancelled = false;
    void api.marketplaceRecommendationAgents()
      .then((available) => {
        if (cancelled) return;
        setAgents(available);
        setAgentID(available[0]?.id ?? "");
      })
      .catch((error) => {
        if (!cancelled) setFailure(describeFailure(error, t("无法读取本地 Agent"), t).message);
      })
      .finally(() => {
        if (!cancelled) setLoadingAgents(false);
      });
    return () => { cancelled = true; };
  }, [t]);

  const byID = useMemo(() => new Map(items.map((item) => [item.id, item])), [items]);

  const recommend = async () => {
    const trimmedNeed = need.trim();
    if (!agentID || !trimmedNeed) return;
    setBusy(true);
    setFailure("");
    setRecommendations([]);
    try {
      const result = await api.recommendMarketplace({
        agent_id: agentID,
        need: trimmedNeed,
        locale,
        items: items.map((item) => ({
          id: item.id,
          name: item.name,
          description: locale === "en" && item.descriptionEn ? item.descriptionEn : item.description,
          category: item.category,
          tags: item.tags ?? [],
        })),
      });
      const resolved = (result.recommendations ?? []).flatMap((recommendation) => {
        const item = byID.get(recommendation.item_id);
        return item ? [{ item, reason: recommendation.reason }] : [];
      });
      setRecommendations(resolved);
      if (resolved.length > 0) {
        try {
          await api.saveRecommendationHistory({
            id: "",
            created_at: "",
            agent_id: agentID,
            need: trimmedNeed,
            catalog_version: catalogVersion,
            results: resolved.map(({ item, reason }) => ({
              item_id: item.id,
              name: item.name,
              reason,
              category: item.category,
              source: item.source ?? "",
            })),
          });
        } catch (error) {
          setFailure(describeFailure(error, t("推荐结果已生成，但保存历史失败"), t).message);
        }
      }
    } catch (error) {
      setFailure(describeFailure(error, t("无法生成工具推荐"), t).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <ModalDialog className="mcp-modal marketplace-recommend-dialog" label={t("工具推荐")} onDismiss={onDismiss}>
      <header>
        <div>
          <h2><Sparkles size={18} />{t("工具推荐")}</h2>
        </div>
        <button className="icon-button" type="button" onClick={onDismiss} title={t("关闭")} aria-label={t("关闭")}><X size={18} /></button>
      </header>

      <div className="marketplace-recommend-privacy">
        <ShieldCheck size={16} aria-hidden="true" />
        <span>{t("只发送你的需求和市场公开目录。本地 Agent 可能连接它已配置的模型服务，但不会获得安装或写文件权限。")}</span>
      </div>

      {loadingAgents ? <p className="marketplace-recommend-status" role="status">{t("正在读取本地 Agent")}</p> : null}
      {!loadingAgents && agents.length === 0 ? (
        <div className="marketplace-recommend-empty">
          <p>{t("没有可用于推荐的本地 Agent")}</p>
          <Link to="/overview" onClick={onDismiss}>{t("前往环境总览")}<ArrowUpRight size={14} /></Link>
        </div>
      ) : null}

      {agents.length > 0 ? (
        <>
          <label>
            {t("用于推荐的 Agent")}
            <SelectField
              value={agentID}
              options={agents.map((agent) => ({ value: agent.id, label: agent.name }))}
              onChange={setAgentID}
              label={t("用于推荐的 Agent")}
            />
          </label>
          <label htmlFor="marketplace-recommend-need">
            {t("你想完成什么？")}
            <textarea
              id="marketplace-recommend-need"
              rows={4}
              maxLength={600}
              value={need}
              onChange={(event) => setNeed(event.target.value)}
              placeholder={t("例如：整理大型项目的长期知识，并让多个 Agent 复用")}
            />
          </label>
        </>
      ) : null}

      {failure ? <p className="settings-field-error" role="alert">{failure}</p> : null}
      {recommendations.length > 0 ? (
        <section className="marketplace-recommend-results" aria-labelledby="marketplace-recommend-results-title">
          <h3 id="marketplace-recommend-results-title">{t("推荐结果")}</h3>
          <ul>
            {recommendations.map(({ item, reason }) => (
              <li key={item.id}>
                <Link to={`/marketplace/${encodeURIComponent(item.id)}`} onClick={onDismiss}>
                  <strong>{item.name}</strong>
                  <span>{reason}</span>
                  <ArrowUpRight size={15} aria-hidden="true" />
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <footer>
        <button className="button button-secondary" type="button" onClick={onDismiss}>{t("取消")}</button>
        {agents.length > 0 ? (
          <button className="button button-primary" type="button" onClick={() => void recommend()} disabled={busy || !need.trim() || !agentID}>
            {busy ? <span className="spinner spinner-on-accent" /> : <Sparkles size={15} />}
            {busy ? t("正在推荐") : t("推荐工具")}
          </button>
        ) : null}
      </footer>
    </ModalDialog>
  );
}
