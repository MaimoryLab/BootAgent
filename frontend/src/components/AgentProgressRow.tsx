import { AlertTriangle, CheckCircle2, Circle, LoaderCircle, RotateCcw } from "lucide-react";

import type { AgentInstallResult } from "../types/api";
import { StatusBadge } from "./StatusBadge";

export function AgentProgressRow({
  name,
  result,
  loading,
  onRetry,
}: {
  name: string;
  result?: AgentInstallResult;
  loading: boolean;
  onRetry?: () => void;
}) {
  const failed = result?.status === "failed";
  const complete = result && !failed && result.status !== "skipped";
  return (
    <div className="progress-row">
      <span className={`progress-icon${failed ? " is-failed" : complete ? " is-complete" : ""}`} aria-hidden="true">
        {loading ? <LoaderCircle size={18} className="spin" /> : failed ? <AlertTriangle size={18} /> : complete ? <CheckCircle2 size={18} /> : <Circle size={18} />}
      </span>
      <span className="progress-copy">
        <strong>{name}</strong>
        <span>{loading ? "正在处理" : result?.message || result?.status || "等待执行"}</span>
      </span>
      {failed && onRetry ? (
        <button className="button button-compact" type="button" onClick={onRetry}>
          <RotateCcw size={15} />
          重试
        </button>
      ) : complete ? (
        <StatusBadge tone="success">已完成</StatusBadge>
      ) : result?.status === "guide-only" ? (
        <StatusBadge tone="neutral">仅引导</StatusBadge>
      ) : null}
    </div>
  );
}
