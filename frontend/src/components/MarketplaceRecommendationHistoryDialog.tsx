import { Clock3, ExternalLink, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { useI18n } from "../i18n";
import type { MarketplaceRecommendationHistory } from "../types/api";
import { ModalDialog } from "./ModalDialog";

export function MarketplaceRecommendationHistoryDialog({ onDismiss }: { onDismiss: () => void }) {
  const { t } = useI18n();
  const [records, setRecords] = useState<MarketplaceRecommendationHistory[]>([]);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    void api.listRecommendationHistory().then(setRecords).catch((error) => setFailure(describeFailure(error, t("无法读取推荐历史"), t).message));
  }, [t]);

  const remove = async (id: string) => {
    try { await api.deleteRecommendationHistory(id); setRecords((current) => current.filter((record) => record.id !== id)); }
    catch (error) { setFailure(describeFailure(error, t("无法删除推荐历史"), t).message); }
  };
  const clear = async () => {
    try { await api.clearRecommendationHistory(); setRecords([]); }
    catch (error) { setFailure(describeFailure(error, t("无法清空推荐历史"), t).message); }
  };

  return (
    <ModalDialog className="mcp-modal marketplace-history-dialog" label={t("推荐历史")} onDismiss={onDismiss}>
      <header><div><h2><Clock3 size={18} />{t("推荐历史")}</h2><p>{t("推荐记录仅保存在本机")}</p></div><button className="icon-button" type="button" onClick={onDismiss} title={t("关闭")} aria-label={t("关闭")}><X size={18} /></button></header>
      {failure ? <p className="settings-field-error" role="alert">{failure}</p> : null}
      {records.length === 0 && !failure ? <p className="marketplace-recommend-status">{t("暂无推荐历史")}</p> : null}
      <ul className="marketplace-history-list">
        {records.map((record) => (
          <li key={record.id}>
            <div><strong>{record.need}</strong><small>{new Date(record.created_at).toLocaleString()} · {(record.results ?? []).length} {t("个结果")}</small></div>
            <div className="marketplace-history-actions">
              {(record.results ?? []).slice(0, 3).map((result) => <Link key={result.item_id} to={`/marketplace/${encodeURIComponent(result.item_id)}`} onClick={onDismiss} title={result.name}><ExternalLink size={14} aria-hidden="true" /></Link>)}
              <button className="icon-button" type="button" onClick={() => void remove(record.id)} title={t("删除")} aria-label={`${t("删除")} ${record.need}`}><Trash2 size={14} /></button>
            </div>
          </li>
        ))}
      </ul>
      {records.length > 0 ? <footer><button className="button button-secondary" type="button" onClick={onDismiss}>{t("关闭")}</button><button className="button button-danger" type="button" onClick={() => void clear()}><Trash2 size={14} />{t("清空历史")}</button></footer> : null}
    </ModalDialog>
  );
}
