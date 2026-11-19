import { Copy, ExternalLink, Play, RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, failureLine, isCancellationError } from "../backend/api";
import { useI18n } from "../i18n";
import type { FirstTaskResult } from "../types/api";
import { copyToClipboard } from "../utils/clipboard";
import { LogDisclosure } from "./LogDisclosure";

/**
 * Offers one small, real task to an Agent that has just been installed: it
 * writes a page in a temporary directory of its own. Rendered only for Agents
 * the backend can drive unattended, so it is absent rather than disabled for
 * the rest.
 *
 * Nothing is sent until the button is pressed: the run is a billed request.
 */
export function FirstTaskSection({ agentId }: { agentId: string }) {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  // Empty until the backend confirms it can drive this Agent, and then its name.
  const [agentName, setAgentName] = useState("");
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<FirstTaskResult | null>(null);
  const [failure, setFailure] = useState("");
  const [copied, setCopied] = useState(false);
  const request = useRef<ReturnType<typeof api.runFirstTask> | null>(null);

  useEffect(() => {
    let active = true;
    void api.firstTaskAgents()
      .then((agents) => { if (active) setAgentName(agents.find((agent) => agent.id === agentId)?.name ?? ""); })
      .catch(() => {});
    return () => { active = false; };
  }, [agentId]);
  // Leaving the page ends the run: nobody would see its result, and it would
  // keep spending on the user's account.
  useEffect(() => () => { void request.current?.cancel?.(); }, []);

  if (!agentName) return null;

  const run = async () => {
    setRunning(true);
    setResult(null);
    setFailure("");
    setCopied(false);
    const pending = api.runFirstTask(agentId, locale);
    request.current = pending;
    try {
      setResult(await pending);
    } catch (error) {
      if (!isCancellationError(error)) setFailure(failureLine(error, t("小任务没能运行"), t));
    } finally {
      request.current = null;
      setRunning(false);
    }
  };

  const files = result?.files ?? [];
  const problem = failure
    || (result && !result.ok
      ? files.length
        ? t("{agent} 没有正常结束，下面的文件可能不完整", { agent: agentName })
        : t("{agent} 没有写出任何文件", { agent: agentName })
      : "");
  const finished = Boolean(result || failure);

  return (
    <section className="first-task-section">
      <h2>{t("跑一个小任务试试")}</h2>
      <p>{t("让 {agent} 在一个临时目录里写一个介绍页，只会写入这个目录。这是真实的模型调用，由模型服务按它的价格计费", { agent: agentName })}</p>
      {result?.ok ? (
        <div className="notice notice-success" role="status">
          <span>{t("{agent} 写好了，文件在下面这个目录", { agent: agentName })}</span>
        </div>
      ) : null}
      {problem ? <div className="notice notice-error" role="alert"><span>{problem}</span></div> : null}
      {result?.directory ? (
        <div className="first-task-output">
          <code>{result.directory}</code>
          <button className="button button-secondary" type="button" onClick={() => void copyToClipboard(result.directory).then(setCopied)}>
            <Copy size={15} aria-hidden="true" />
            {copied ? t("已复制") : t("复制路径")}
          </button>
          <ul aria-label={t("生成的文件")}>
            {files.map((file) => <li key={file}>{file}</li>)}
          </ul>
        </div>
      ) : null}
      <div className="first-task-actions">
        {running ? (
          <>
            <button className="button button-primary" type="button" disabled>
              <RefreshCw size={15} className="spin" aria-hidden="true" />
              {t("正在运行，通常需要一两分钟")}
            </button>
            <button className="button button-secondary" type="button" onClick={() => void request.current?.cancel?.()}>{t("取消")}</button>
          </>
        ) : !finished ? (
          <button className="button button-primary" type="button" onClick={() => void run()}>
            <Play size={15} aria-hidden="true" />
            {t("开始")}
          </button>
        ) : problem ? (
          <>
            <button className="button button-primary" type="button" onClick={() => void run()}>
              <RefreshCw size={15} aria-hidden="true" />
              {t("重试")}
            </button>
            <button className="button button-secondary" type="button" onClick={() => navigate("/providers")}>
              <ExternalLink size={15} aria-hidden="true" />
              {t("检查模型服务")}
            </button>
          </>
        ) : null}
      </div>
      {result?.log ? <LogDisclosure log={result.log} open={!result.ok} label={t("查看运行记录")} /> : null}
    </section>
  );
}
