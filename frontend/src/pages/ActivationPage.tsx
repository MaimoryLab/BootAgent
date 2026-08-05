import { Download } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { AgentProgressRow } from "../components/AgentProgressRow";
import { DownloadProgress } from "../components/DownloadProgress";
import { LogDisclosure } from "../components/LogDisclosure";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { taskKey, useTaskCenter, useTaskRoute } from "../state/TaskCenterContext";
import { desktopProtocol, profileAgentIdForDesktop, selectedDesktopApp } from "../state/desktopSetup";
import { useWizard } from "../state/WizardContext";
import type { AgentInstallResult, InstallRequest } from "../types/api";

export function ActivationPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, refreshStatus } = useWizard();
  const { tasks, startTask, finishTask, taskFor } = useTaskCenter();
  const route = useTaskRoute();
  const started = useRef(false);
  const [retrying, setRetrying] = useState<string | null>(null);
  const isDesktop = state.setupKind === "desktop";
  const desktop = isDesktop && state.status ? selectedDesktopApp(state.status, state.selectedAgentIds) : undefined;
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
    for (const task of tasks) {
      if (task.kind === "download" && task.state === "running") ids.add(task.target);
    }
    return [...ids].map((id) => ({ id, runtime: byId.get(id) }));
  }, [state.selectedAgentIds, state.status, tasks]);

  const requestFor = useCallback(
    (agents: string[]): InstallRequest => ({
      agents,
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

  const startActivationTasks = useCallback((agentIds: string[]) => {
    const targets = isDesktop && desktop ? [desktop.id] : agentIds;
    const startedAgents: string[] = [];
    for (const target of targets) {
      const id = taskKey("install", target);
      if (!startTask({
        id,
        kind: "install",
        target,
        title: t("安装 {name}", { name: selectedNames[target] || target }),
        route,
      })) {
        for (const started of startedAgents) finishTask(started, { kind: "failure", message: t("任务正在运行") });
        return null;
      }
      startedAgents.push(id);
    }

    const byID = new Map((state.status?.runtimes ?? []).map((runtime) => [runtime.id, runtime]));
    const runtimeIDs = new Set<string>();
    for (const agentId of agentIds) {
      const runtimeID = state.status?.capabilities.missingRuntime?.[agentId];
      if (runtimeID) runtimeIDs.add(runtimeID);
    }
    const ownedRuntimes: string[] = [];
    for (const runtimeID of runtimeIDs) {
      const id = taskKey("download", runtimeID);
      if (taskFor(id)?.state === "running") continue;
      if (startTask({
        id,
        kind: "download",
        target: runtimeID,
        title: t("安装 {name} {version}", { name: byID.get(runtimeID)?.name || runtimeID, version: byID.get(runtimeID)?.lockedVersion || "" }),
        route,
        progressTarget: runtimeID,
      })) ownedRuntimes.push(id);
    }
    return { agents: startedAgents, runtimes: ownedRuntimes };
  }, [desktop, finishTask, isDesktop, route, selectedNames, startTask, state.status, t, taskFor]);

  const finishActivationTasks = useCallback((
    started: { agents: string[]; runtimes: string[] },
    results: AgentInstallResult[],
    ok: boolean,
    fallback: string,
  ) => {
    for (const id of started.agents) {
      const target = id.slice("install:".length);
      const result = results.find((item) => item.agent === target);
      finishTask(id, !result || result.status === "failed"
        ? { kind: "failure", message: result?.message || fallback }
        : { kind: "success", message: result?.message || t("安装完成") });
    }
    for (const id of started.runtimes) finishTask(id, ok ? { kind: "success", message: t("安装完成") } : { kind: "failure", message: fallback });
  }, [finishTask, t]);

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
      protocol: desktopProtocol(desktop),
    });
    const installed = desktop.installed ? undefined : await api.installDesktopAgent(desktop.id);
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
    const startedTasks = startActivationTasks(state.selectedAgentIds);
    if (!startedTasks) {
      dispatch({ type: "ACTIVATION_FAILED", message: t("任务正在运行") });
      return;
    }
    dispatch({ type: "ACTIVATION_LOADING", agentIds: state.selectedAgentIds });
    try {
      const response = isDesktop
        ? await installDesktop().then((result) => ({ ...result, ok: true, probe: null }))
        : await api.install(requestFor(state.selectedAgentIds));
      dispatch({
        type: "ACTIVATION_RESULT",
        ok: response.ok,
        results: response.results,
        log: response.log,
        probe: response.probe,
        next: response.next,
        replaceAgents: state.selectedAgentIds,
      });
      finishActivationTasks(startedTasks, response.results, response.ok, t("激活失败"));
      if (response.ok) {
        await refreshStatus();
      }
    } catch (error) {
      const message = describeError(error, t("激活失败")).message;
      finishActivationTasks(startedTasks, [], false, message);
      dispatch({ type: "ACTIVATION_FAILED", message });
    }
  }, [dispatch, finishActivationTasks, installDesktop, isDesktop, refreshStatus, requestFor, startActivationTasks, state.selectedAgentIds, t]);

  useEffect(
    () =>
      api.onInstallOutput((output) => {
        dispatch({ type: "ACTIVATION_OUTPUT", output });
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
    const startedTasks = startActivationTasks([agentId]);
    if (!startedTasks) return;
    setRetrying(agentId);
    try {
      const response = isDesktop
        ? await installDesktop().then((result) => ({ ...result, ok: true, probe: null }))
        : await api.install(requestFor([agentId]));
      dispatch({
        type: "ACTIVATION_RESULT",
        ok: response.ok,
        results: response.results,
        log: response.log,
        probe: response.probe,
        next: response.next,
        replaceAgents: [agentId],
      });
      finishActivationTasks(startedTasks, response.results, response.ok, t("重试失败"));
      const otherFailures = state.activationResults.filter((item) => item.agent !== agentId && item.status === "failed");
      if (response.ok && !otherFailures.length) {
        await refreshStatus();
      }
    } catch (error) {
      const message = describeError(error, t("重试失败")).message;
      finishActivationTasks(startedTasks, [], false, message);
      dispatch({ type: "ACTIVATION_FAILED", message });
    } finally {
      setRetrying(null);
    }
  };

  // The task card is also a recovery path after another setup run replaced the
  // wizard draft. Render the durable task directly instead of bouncing through
  // the setup guards with an empty selection.
  const restoredTask = tasks.find((task) => task.kind === "install" && task.route.split("?", 1)[0] === "/setup/activation");
  if (!state.selectedAgentIds.length && restoredTask) {
    const loading = restoredTask.state === "running";
    return (
      <PageScaffold
        title={loading ? t("正在安装") : restoredTask.state === "success" ? t("安装完成") : t("需要处理部分问题")}
        description={restoredTask.message || t("每个 Agent 的结果彼此独立，失败项可以单独重试。")}
        primaryLabel={t("进入总览")}
        onPrimary={() => navigate("/overview")}
        footerNote={loading ? t("请保持此窗口打开") : undefined}
      >
        <div className="progress-list">
          <AgentProgressRow
            name={restoredTask.title}
            result={restoredTask.state === "failure" ? { agent: restoredTask.target, status: "failed", message: restoredTask.message, retryable: true } : restoredTask.state === "success" ? { agent: restoredTask.target, status: "configured", message: restoredTask.message, retryable: false } : undefined}
            loading={loading}
          />
        </div>
      </PageScaffold>
    );
  }

  const allDone = state.activationState === "success";
  const runtimeDownloadActive = runtimeDownloads.some(({ id }) => taskFor(taskKey("download", id))?.state === "running")
    && (state.activationState === "loading" || retrying !== null);
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
