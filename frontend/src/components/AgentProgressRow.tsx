import { AlertTriangle, CheckCircle2, Circle, Download, LoaderCircle, RotateCcw } from "lucide-react";

import { useI18n } from "../i18n";
import type { AgentInstallResult } from "../types/api";
import { StatusBadge } from "./StatusBadge";

export function AgentProgressRow({
  name,
  result,
  loading,
  onRetry,
  onInstallRuntime,
  runtimeName,
}: {
  name: string;
  result?: AgentInstallResult;
  loading: boolean;
  onRetry?: () => void;
  /** Offered when this row failed for a missing runtime the app can install. */
  onInstallRuntime?: () => void;
  runtimeName?: string;
}) {
  const { t } = useI18n();
  const failed = result?.status === "failed";
  const complete = result && !failed && result.status !== "skipped";
  const resultStatus = result?.status === "failed"
    ? t("失败")
    : result?.status === "skipped"
      ? t("已跳过")
      : result?.status === "guide-only"
        ? t("仅引导")
        : result?.status === "configured" || result?.status === "installed"
          ? t("已完成")
          : result?.status;
  return (
    <div className="progress-row">
      <span className={`progress-icon${failed ? " is-failed" : complete ? " is-complete" : ""}`} aria-hidden="true">
        {loading ? <LoaderCircle size={18} className="spin" /> : failed ? <AlertTriangle size={18} /> : complete ? <CheckCircle2 size={18} /> : <Circle size={18} />}
      </span>
      <span className="progress-copy">
        <strong>{name}</strong>
        <span>{loading ? t("正在处理") : result?.message || resultStatus || t("等待执行")}</span>
      </span>
      {/* The runtime install comes first: when a row failed because Node or uv is
          missing, installing it is the step that makes a retry worth pressing,
          and the control for it otherwise lives two wizard steps back. */}
      {failed && onInstallRuntime ? (
        <button className="button button-compact" type="button" onClick={onInstallRuntime}>
          <Download size={15} />
          {runtimeName ? t("安装 {name}", { name: runtimeName }) : t("安装运行时")}
        </button>
      ) : null}
      {failed && onRetry ? (
        <button className="button button-compact" type="button" onClick={onRetry}>
          <RotateCcw size={15} />
          {t("重试")}
        </button>
      ) : complete ? (
        <StatusBadge tone="success">{t("已完成")}</StatusBadge>
      ) : result?.status === "guide-only" ? (
        <StatusBadge tone="neutral">{t("仅引导")}</StatusBadge>
      ) : null}
    </div>
  );
}
