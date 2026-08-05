import { Download } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { AgentProgressRow } from "../components/AgentProgressRow";
import { DownloadProgress } from "../components/DownloadProgress";
import { LogDisclosure } from "../components/LogDisclosure";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";
import { profileAgentIdForDesktop } from "../state/desktopSetup";
import { useWizard } from "../state/WizardContext";
import type { AgentInstallResult, InstallRequest } from "../types/api";

export function ActivationPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, refreshStatus } = useWizard();
  const { progress, resetProgress } = useTaskCenter();
  const started = useRef(false);
  const [retrying, setRetrying] = useState<string | null>(null);
  const [runtimeDownloading, setRuntimeDownloading] = useState(false);
  const isDesktop = state.setupKind === "desktop";
  const desktop = isDesktop ? state.status?.desktopAgent : undefined;
  const selectedNames = useMemo(
    () => Object.fromEntries([
      ...(state.status?.catalog.map((agent) => [agent.id, agent.name] as const) || []),
      ...(desktop ? [[desktop.id, desktop.name] as const] : []),
    ]),
    [desktop, state.status],
  );
  const runtimeDownloads = useMemo(() => {
    const byId = new Map((state.status?.runtimes ?? []).map((runtime) => [runtime.id, runtime]));
    const ids = new Set<string>();
    for (const agentId of state.selectedAgentIds) {
      const runtimeId = state.status?.capabilities.missingRuntime?.[agentId];
      if (runtimeId) ids.add(runtimeId);
    }
    for (const runtimeId of Object.keys(progress)) ids.add(runtimeId);
    return [...ids].map((id) => ({ id, runtime: byId.get(id) }));
  }, [progress, state.selectedAgentIds, state.status]);

  const clearRuntimeProgress = useCallback(() => {
    for (const { id } of runtimeDownloads) resetProgress(id);
  }, [resetProgress, runtimeDownloads]);

  const requestFor = useCallback(
    (agents: string[], profileAgents?: string[]): InstallRequest => ({
      agents,
      profile_agents: profileAgents,
      provider: state.provider,
      api_base_url: "",
      // Provider credentials are resolved server-side from the saved Provider.
      api_key: "",
      // SetupGuard guarantees a model before this page can render.
      model: state.model,
      profile_id: state.profileId,
      profile_label: state.profileLabel,
      configure: true,
      install_agent: true,
      skip_test: false,
    }),
    [state.model, state.profileId, state.profileLabel, state.provider],
  );

  const installDesktop = useCallback(async (): Promise<{ results: AgentInstallResult[]; log: string; next: string }> => {
    if (!desktop) throw new Error(t("找不到桌面 Agent"));
    const owner = profileAgentIdForDesktop(desktop);
    const profileID = state.profileId || `${owner}-${state.provider}`.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
    const profileLabel = state.profileLabel || `${desktop.name} · ${state.provider}`;
    const profile = await api.saveProfile({
      id: profileID,
      label: profileLabel,
      provider: state.provider,
      apiBaseUrl: "",
      apiKey: "",
      model: state.model,
      configMode: "provider",
      protocol: state.status?.catalog.find((item) => item.id === owner)?.protocol || "",
    });
    const installed = desktop.installed ? undefined : await api.installDesktopAgent();
    const configured = await api.configureDesktopAgent(desktop.id, profile.id);
    return {
      results: [{
        agent: desktop.id,
        status: "configured",
        installed: installed?.app.installed ?? desktop.installed,
        version: installed?.app.version || desktop.version || undefined,
        message: configured.message,
        retryable: false,
      }],
      log: configured.message,
      next: configured.restart || "",
    };
  }, [desktop, state.profileId, state.profileLabel, state.provider, state.model, t]);

  const activate = useCallback(async () => {
    clearRuntimeProgress();
    setRuntimeDownloading(runtimeDownloads.length > 0);
    dispatch({ type: "ACTIVATION_LOADING", agentIds: state.selectedAgentIds });
    try {
      const response = isDesktop
        ? await installDesktop().then((result) => ({ ...result, ok: true, probe: null }))
        : await api.install(requestFor(state.selectedAgentIds));
      dispatch({
        type: "ACTIVATION_RESULT",
        results: response.results,
        log: response.log,
        probe: response.probe,
        next: response.next,
        replaceAgents: state.selectedAgentIds,
      });
      if (response.ok) {
        await refreshStatus();
      }
    } catch (error) {
      dispatch({ type: "ACTIVATION_FAILED", message: describeError(error, t("激活失败")).message });
    } finally {
      setRuntimeDownloading(false);
    }
  }, [clearRuntimeProgress, dispatch, installDesktop, isDesktop, refreshStatus, requestFor, runtimeDownloads.length, state.selectedAgentIds, t]);

  useEffect(
    () =>
      api.onInstallOutput((output) => {
        dispatch({ type: "ACTIVATION_OUTPUT", output });
        if (output.kind === "progress") setRuntimeDownloading(true);
        if (output.kind === "command") setRuntimeDownloading(false);
      }),
    [dispatch],
  );

  useEffect(() => {
    // Runs only for an explicit request from the review page. Returning here
    // via browser back keeps activationRequested false, so a mount alone never
    // replays the install; the ref guards StrictMode's doubled effect.
    if (state.activationRequested && !started.current) {
      started.current = true;
      void activate();
    }
  }, [activate, state.activationRequested]);

  const retry = async (agentId: string) => {
    clearRuntimeProgress();
    setRuntimeDownloading(runtimeDownloads.length > 0);
    setRetrying(agentId);
    try {
      const response = isDesktop
        ? await installDesktop().then((result) => ({ ...result, ok: true, probe: null }))
        : await api.install(requestFor([agentId], state.selectedAgentIds));
      dispatch({
        type: "ACTIVATION_RESULT",
        results: response.results,
        log: response.log,
        probe: response.probe,
        next: response.next,
        replaceAgents: [agentId],
      });
      const otherFailures = state.activationResults.filter((item) => item.agent !== agentId && item.status === "failed");
      if (response.ok && !otherFailures.length) {
        await refreshStatus();
      }
    } catch (error) {
      dispatch({ type: "ACTIVATION_FAILED", message: describeError(error, t("重试失败")).message });
    } finally {
      setRuntimeDownloading(false);
      setRetrying(null);
    }
  };

  useEffect(() => {
    if (state.activationState !== "loading" && !retrying) clearRuntimeProgress();
  }, [clearRuntimeProgress, retrying, state.activationState]);

  const allDone = state.activationState === "success";
  const runtimeDownloadActive = runtimeDownloading && (state.activationState === "loading" || retrying !== null);
  return (
    <PageScaffold
      title={state.activationState === "loading" ? t("正在安装") : allDone ? t("安装完成") : t("需要处理部分问题")}
      description={state.activationState === "loading" ? t("安装请求同步执行，完成后将显示每个 Agent 的最终状态。") : t("每个 Agent 的结果彼此独立，失败项可以单独重试。")}
      stepper
      onBack={state.activationState === "loading" ? undefined : () => navigate("/setup/review")}
      primaryLabel={allDone ? t("进入总览") : undefined}
      onPrimary={allDone ? () => navigate("/overview") : undefined}
      footerNote={state.activationState === "loading" ? t("请保持此窗口打开") : undefined}
    >
      {runtimeDownloadActive && runtimeDownloads.length ? (
        <section className="runtime-download-card" aria-live="polite">
          <div className="runtime-download-heading">
            <Download size={18} aria-hidden="true" />
            <div>
              <strong>{t("运行时下载")}</strong>
              <span>
                {runtimeDownloads
                  .map(({ id, runtime }) => `${runtime?.name ?? id} ${runtime?.lockedVersion ?? ""}`.trim())
                  .join("、")}
              </span>
            </div>
          </div>
          {runtimeDownloads.map(({ id }) => <DownloadProgress key={id} target={id} pending />)}
        </section>
      ) : null}
      <div className="progress-list">
        {state.selectedAgentIds.map((agentId) => {
          const result = state.activationResults.find((item) => item.agent === agentId);
          return (
            <AgentProgressRow
              key={agentId}
              name={selectedNames[agentId] || agentId}
              result={result}
              loading={state.activationState === "loading" || retrying === agentId}
              // One retry at a time: concurrent retries each observe a stale
              // snapshot of the other rows and would skip the final cleanup.
              onRetry={result?.status === "failed" && result.retryable && !retrying ? () => void retry(agentId) : undefined}
            />
          );
        })}
      </div>
      {state.activationProbe && !state.activationProbe.ok ? <div className="notice notice-warning">{state.activationProbe.message}</div> : null}
      {/* The launch commands belong to the moment installation finishes, not to
          the overview a user opens every day. */}
      {allDone && state.activationNext ? (
        <section className="next-command-section">
          <h2>{t("下一步命令")}</h2>
          <pre>{state.activationNext}</pre>
        </section>
      ) : null}
      <LogDisclosure log={state.activationLog} open={state.activationState === "loading" || retrying !== null} />
    </PageScaffold>
  );
}
