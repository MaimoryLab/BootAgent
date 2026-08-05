import { Download, RefreshCw, TriangleAlert } from "lucide-react";
import { useMemo, useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";
import type { AgentStatus, RuntimeStatus } from "../types/api";
import { DownloadProgress } from "./DownloadProgress";

interface RuntimePromptProps {
  runtimes: RuntimeStatus[];
  /** Agent id -> runtime id, as reported by capabilities.missingRuntime. */
  missingRuntime: Record<string, string>;
  selectedAgentIds: string[];
  agents: Record<string, AgentStatus>;
  onInstalled: () => void | Promise<void>;
}

/**
 * Prompts for the runtimes the current selection needs. Activation installs a
 * missing runtime on its own, so this is an early, explicit offer rather than a
 * gate: nothing here blocks continuing.
 */
export function RuntimePrompt({ runtimes, missingRuntime, selectedAgentIds, agents, onInstalled }: RuntimePromptProps) {
  const { t } = useI18n();
  const { resetProgress } = useTaskCenter();
  const [pending, setPending] = useState("");
  const [failure, setFailure] = useState("");

  const required = useMemo(() => {
    const byId = new Map(runtimes.map((runtime) => [runtime.id, runtime]));
    const needed = new Map<string, RuntimeStatus>();
    for (const agentId of selectedAgentIds) {
      // An Agent that is already installed does not need its package manager.
      if (agents[agentId]?.installed) continue;
      const runtimeId = missingRuntime[agentId];
      if (!runtimeId) continue;
      const runtime = byId.get(runtimeId);
      if (runtime && !runtime.installed && runtime.supported) needed.set(runtimeId, runtime);
    }
    return [...needed.values()];
  }, [agents, missingRuntime, runtimes, selectedAgentIds]);

  if (!required.length) return null;

  const install = async (runtimeId: string) => {
    setPending(runtimeId);
    setFailure("");
    // A retry must not open on the previous attempt's finished bar.
    resetProgress(runtimeId);
    try {
      await api.installRuntime(runtimeId);
      await onInstalled();
    } catch (error) {
      setFailure(describeError(error, t("运行时安装失败")).message);
    } finally {
      setPending("");
      resetProgress(runtimeId);
    }
  };

  return (
    <div className="runtime-prompt">
      <div className="notice notice-warning runtime-prompt-heading">
        <TriangleAlert size={16} aria-hidden="true" />
        <strong>{t("需要先安装运行时")}</strong>
      </div>
      <div className="runtime-prompt-body">
        <span>{t("所选 Agent 通过 {runtimes} 安装，本机还没有。现在安装，或在激活时自动安装。", { runtimes: required.map((runtime) => runtime.name).join("、") })}</span>
        <div className="runtime-prompt-actions">
          {required.map((runtime) => (
            <button
              key={runtime.id}
              className="button button-secondary"
              type="button"
              onClick={() => void install(runtime.id)}
              disabled={Boolean(pending)}
            >
              {pending === runtime.id ? <RefreshCw size={15} className="spin" /> : <Download size={15} />}
              {pending === runtime.id
                ? t("安装中")
                : t("安装 {name} {version}", { name: runtime.name, version: runtime.lockedVersion })}
            </button>
          ))}
        </div>
        {pending ? <DownloadProgress target={pending} pending /> : null}
        {failure ? <span className="runtime-prompt-error">{failure}</span> : null}
      </div>
    </div>
  );
}
