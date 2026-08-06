import { Download, RefreshCw, TriangleAlert } from "lucide-react";
import { useMemo, useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import { taskCanceller, taskKey, useTaskCenter, useTaskRoute } from "../state/TaskCenterContext";
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
  const { startTask, finishTask, setTaskCanceller, taskFor } = useTaskCenter();
  const route = useTaskRoute();
  const [pending, setPending] = useState("");

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

  // The prompt sits on a wizard step the user can leave mid-download, so the
  // in-flight flag and the failure live in the provider above route content rather
  // than in local state that unmounting would discard.
  const downloading = required.find((runtime) => taskFor(taskKey("download", runtime.id))?.state === "running")?.id ?? "";
  const busy = Boolean(pending) || Boolean(downloading);
  const failure = required
    .map((runtime) => taskFor(taskKey("download", runtime.id)))
    .find((task) => task?.state === "failure")?.message ?? "";

  const install = async (runtimeId: string) => {
    const id = taskKey("download", runtimeId);
    const runtime = required.find((item) => item.id === runtimeId);
    if (!startTask({
      id,
      kind: "download",
      target: runtimeId,
      title: t("安装 {name} {version}", { name: runtime?.name || runtimeId, version: runtime?.lockedVersion || "" }),
      route,
      progressTarget: runtimeId,
    })) return;
    setPending(runtimeId);
    try {
      const request = api.installRuntime(runtimeId);
      setTaskCanceller(id, taskCanceller(request));
      await request;
      finishTask(id, { kind: "success", message: t("安装完成") });
      await onInstalled();
    } catch (error) {
      finishTask(id, { kind: "failure", message: describeError(error, t("运行时安装失败")).message });
    } finally {
      setPending("");
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
              disabled={busy}
            >
              {pending === runtime.id || downloading === runtime.id ? <RefreshCw size={15} className="spin" /> : <Download size={15} />}
              {pending === runtime.id || downloading === runtime.id
                ? t("安装中")
                : t("安装 {name} {version}", { name: runtime.name, version: runtime.lockedVersion })}
            </button>
          ))}
        </div>
        {required.map((runtime) => <DownloadProgress key={runtime.id} target={runtime.id} />)}
        {failure ? <span className="runtime-prompt-error">{failure}</span> : null}
      </div>
    </div>
  );
}
