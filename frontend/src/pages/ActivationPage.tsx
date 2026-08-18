import { Download } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure, isCancellationError } from "../backend/api";
import { AgentProgressRow } from "../components/AgentProgressRow";
import { DownloadProgress } from "../components/DownloadProgress";
import { LogDisclosure } from "../components/LogDisclosure";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { desktopProtocol, profileAgentIdForDesktop, selectedDesktopApp } from "../state/desktopSetup";
import { installTaskRoute, taskCanceller, taskKey, useTaskCenter, useTaskRoute, type TaskCanceller } from "../state/TaskCenterContext";
import { useWizard } from "../state/WizardContext";
import type { AgentInstallResult, InstallRequest } from "../types/api";

export function ActivationPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, refreshStatus } = useWizard();
  const { tasks, startTask, finishTask, setTaskCanceller, taskFor } = useTaskCenter();
  const route = useTaskRoute();
  const started = useRef(false);
  const [retrying, setRetrying] = useState<string | null>(null);
  // Shown when a run could not start at all. Local state rather than the wizard:
  // it describes this visit to the page, not the outcome of an install.
  const [blockedMessage, setBlockedMessage] = useState("");
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
    const officialScriptAgents = new Set(
      (state.status?.catalog ?? [])
        .filter((agent) => agent.packageManager === "official-script")
        .map((agent) => agent.id),
    );
    const selectedTargets = isDesktop && desktop ? [desktop.id] : agentIds;
    const targets = selectedTargets.filter((target) => !officialScriptAgents.has(target));
    const group = `activation:${Date.now()}:${targets.join(",")}`;
    const startedAgents: string[] = [];
    for (const target of targets) {
      const id = taskKey("install", target);
      if (!startTask({
        id,
        kind: "install",
        target,
        title: t("安装 {name}", { name: selectedNames[target] || target }),
        route: installTaskRoute(target),
        group,
      })) {
        // The rollback names the Agent that is actually blocked. Reusing the
        // collision message here reported "this task is already running" against
        // Agents that were merely rolled back, which pointed the user at the wrong
        // row -- and at a task that was never theirs.
        const blockedBy = selectedNames[target] || target;
        for (const started of startedAgents) {
          finishTask(started, { kind: "failure", message: t("{name} 有任务正在运行，本次未开始", { name: blockedBy }) });
        }
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
        group,
      })) ownedRuntimes.push(id);
    }
    return { agents: startedAgents, runtimes: ownedRuntimes };
  }, [desktop, finishTask, isDesktop, route, selectedNames, startTask, state.status, t, taskFor]);

  const registerActivationCanceller = useCallback((started: { agents: string[]; runtimes: string[] }, cancel?: TaskCanceller) => {
    for (const id of [...started.agents, ...started.runtimes]) setTaskCanceller(id, cancel);
  }, [setTaskCanceller]);

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

  const installDesktop = useCallback(async (register: (cancel?: TaskCanceller) => void): Promise<{ results: AgentInstallResult[]; log: string; next: string }> => {
    if (!desktop) throw new Error(t("找不到桌面 Agent"));
    const owner = profileAgentIdForDesktop(desktop);
    const profileID = state.profileId || `${owner}-${state.provider}`.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
    const profileLabel = state.profileLabel || `${desktop.name} · ${state.provider}`;
    const profileRequest = api.saveProfile({
      id: profileID,
      label: profileLabel,
      provider: state.provider,
      apiBaseUrl: "",
      apiKey: "",
      model: state.model,
      configMode: "provider",
      protocol: desktopProtocol(desktop),
    });
    register(taskCanceller(profileRequest));
    const { profile } = await profileRequest;
    const installRequest = desktop.installed || desktop.manualInstall ? undefined : api.installDesktopAgent(desktop.id);
    if (installRequest) register(taskCanceller(installRequest));
    const installed = installRequest ? await installRequest : undefined;
    const configureRequest = api.configureDesktopAgent(desktop.id, profile.id);
    register(taskCanceller(configureRequest));
    const configured = await configureRequest;
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
      setBlockedMessage(t("已有安装任务正在运行，请在任务中心查看后重试"));
      // Names where to look. "任务正在运行" alone said a task existed without
      // saying which, and the task centre is the only place to find it.
      dispatch({ type: "ACTIVATION_FAILED", message: t("已有安装任务正在运行，请在任务中心查看后重试") });
      return;
    }
    dispatch({ type: "ACTIVATION_LOADING", agentIds: state.selectedAgentIds });
    const firstAgent = startedTasks.agents[0]?.slice("install:".length);
    if (firstAgent) navigate(installTaskRoute(firstAgent));
    try {
      let response;
      if (isDesktop) {
        response = await installDesktop((cancel) => registerActivationCanceller(startedTasks, cancel))
          .then((result) => ({ ...result, ok: true, probe: null }));
      } else {
        const request = api.install(requestFor(state.selectedAgentIds));
        registerActivationCanceller(startedTasks, taskCanceller(request));
        response = await request;
      }
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
      // Refreshed whenever any Agent got as far as being configured, not only on a
      // wholly successful run. An Agent that really did install inside a run that
      // was not `ok` stayed reported as missing until some later refresh, so the
      // overview showed the wrong thing with no explanation.
      if (response.ok || response.results.some((result) => result.status !== "failed" && result.status !== "skipped")) {
        await refreshStatus();
      }
    } catch (error) {
      const message = isCancellationError(error) ? t("已取消") : describeFailure(error, t("激活失败"), t).message;
      finishActivationTasks(startedTasks, [], false, message);
      dispatch({ type: "ACTIVATION_FAILED", message });
    }
  }, [dispatch, finishActivationTasks, installDesktop, isDesktop, navigate, refreshStatus, registerActivationCanceller, requestFor, startActivationTasks, state.selectedAgentIds, t]);

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

  /**
   * The runtime a failed row is missing, when the app can install it.
   *
   * Gated on the error code rather than on message text: PREREQUISITE_MISSING
   * also covers manifest problems no download would fix, so the runtime has to be
   * one capabilities.missingRuntime actually names and that is not installed yet.
   */
  const missingRuntimeFor = (agentId: string, result?: AgentInstallResult) => {
    if (result?.status !== "failed" || result.error_code !== "PREREQUISITE_MISSING") return undefined;
    const runtimeId = state.status?.capabilities.missingRuntime?.[agentId];
    if (!runtimeId) return undefined;
    const runtime = state.status?.runtimes.find((item) => item.id === runtimeId);
    return runtime && !runtime.installed && runtime.supported ? runtime : undefined;
  };

  const installMissingRuntime = async (runtimeId: string, name: string) => {
    const id = taskKey("download", runtimeId);
    if (!startTask({ id, kind: "download", target: runtimeId, title: t("安装 {name}", { name }), route, progressTarget: runtimeId })) return;
    try {
      const request = api.installRuntime(runtimeId);
      setTaskCanceller(id, taskCanceller(request));
      await request;
      finishTask(id, { kind: "success", message: t("安装完成") });
      await refreshStatus();
    } catch (error) {
      finishTask(id, { kind: "failure", message: describeFailure(error, t("运行时安装失败"), t).message });
    }
  };

  const retry = async (agentId: string) => {
    setBlockedMessage("");
    const startedTasks = startActivationTasks([agentId]);
    // Same silent dead end as the first run: without this a blocked retry did
    // nothing and said nothing.
    if (!startedTasks) {
      setBlockedMessage(t("已有安装任务正在运行，请在任务中心查看后重试"));
      return;
    }
    setRetrying(agentId);
    try {
      let response;
      if (isDesktop) {
        response = await installDesktop((cancel) => registerActivationCanceller(startedTasks, cancel))
          .then((result) => ({ ...result, ok: true, probe: null }));
      } else {
        const request = api.install(requestFor([agentId]));
        registerActivationCanceller(startedTasks, taskCanceller(request));
        response = await request;
      }
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
      // Unconditional, for the same reason as the first run: a successful retry
      // changes what is on disk whether or not other rows are still failing, and
      // suppressing the refresh left the overview describing the old state.
      if (response.ok) {
        await refreshStatus();
      }
    } catch (error) {
      const message = isCancellationError(error) ? t("已取消") : describeFailure(error, t("重试失败"), t).message;
      finishActivationTasks(startedTasks, [], false, message);
      dispatch({ type: "ACTIVATION_FAILED", message });
    } finally {
      setRetrying(null);
    }
  };

  // The task card is also a recovery path after another setup run replaced the
  // wizard draft. Render the durable task directly instead of bouncing through
  // the setup guards with an empty selection.
  const restoredTask = tasks.find((task) => task.kind === "install" && task.route.startsWith("/tasks/install/"));
  if (!state.selectedAgentIds.length && restoredTask) {
    const loading = restoredTask.state === "running";
    const cancelled = restoredTask.state === "cancelled";
    return (
      <PageScaffold
        title={loading ? t("正在安装") : restoredTask.state === "success" ? t("安装完成") : cancelled ? t("已取消") : t("需要处理部分问题")}
        description={restoredTask.message || (cancelled ? t("已取消") : t("每个 Agent 的结果彼此独立，失败项可以单独重试"))}
        primaryLabel={t("进入总览")}
        onPrimary={() => navigate("/overview")}
        footerNote={loading ? t("请保持此窗口打开") : undefined}
      >
        <div className="progress-list">
          <AgentProgressRow
            name={restoredTask.title}
            result={restoredTask.state === "failure" ? { agent: restoredTask.target, status: "failed", message: restoredTask.message, retryable: true } : restoredTask.state === "success" ? { agent: restoredTask.target, status: "configured", message: restoredTask.message, retryable: false } : cancelled ? { agent: restoredTask.target, status: "skipped", message: t("已取消"), retryable: false } : undefined}
            loading={loading}
          />
        </div>
      </PageScaffold>
    );
  }

  const taskTarget = (agentId: string) => isDesktop && desktop ? desktop.id : agentId;
  const activationCancelled = state.selectedAgentIds.some((agentId) => taskFor(taskKey("install", taskTarget(agentId)))?.state === "cancelled");
  const activationLoading = state.activationState === "loading" && !activationCancelled;
  const allDone = state.activationState === "success" && !activationCancelled;
  // A run where one Agent succeeded and another failed unretryably had no forward
  // button and no retry button: the only exit was back to the review step, even
  // though the Agent that succeeded is configured and usable. Any finished row is
  // enough to offer the overview.
  const anyConfigured = !activationLoading && state.activationResults.some(
    (result) => result.status !== "failed" && result.status !== "skipped",
  );
  const runtimeDownloadActive = runtimeDownloads.some(({ id }) => taskFor(taskKey("download", id))?.state === "running")
    && (state.activationState === "loading" || retrying !== null);
  return (
    <PageScaffold
      title={activationLoading ? t("正在安装") : allDone ? t("安装完成") : activationCancelled ? t("已取消") : t("需要处理部分问题")}
      // A cancel can land between steps, so it says what is guaranteed rather than
      // just "cancelled": runtime extraction is atomic (staging plus rename), and
      // re-running is safe because every write is idempotent.
      description={activationLoading
        ? t("安装请求同步执行，完成后将显示每个 Agent 的最终状态")
        : activationCancelled
          ? t("已取消。已完成的部分保留在本机，重新运行是安全的")
          : t("每个 Agent 的结果彼此独立，失败项可以单独重试")}
      stepper
      onBack={activationLoading ? undefined : () => navigate("/setup/review")}
      primaryLabel={allDone || anyConfigured ? t("进入总览") : undefined}
      onPrimary={allDone || anyConfigured ? () => navigate("/overview") : undefined}
      footerNote={activationLoading ? t("请保持此窗口打开") : undefined}
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
          const cancelled = taskFor(taskKey("install", taskTarget(agentId)))?.state === "cancelled";
          const result = cancelled
            ? { agent: agentId, status: "skipped" as const, message: t("已取消"), retryable: false }
            : state.activationResults.find((item) => item.agent === agentId);
          const missingRuntime = missingRuntimeFor(agentId, result);
          const runtimeBusy = Boolean(missingRuntime && taskFor(taskKey("download", missingRuntime.id))?.state === "running");
          return (
            <AgentProgressRow
              key={agentId}
              name={selectedNames[agentId] || agentId}
              result={result}
              loading={!cancelled && (state.activationState === "loading" || retrying === agentId)}
              // One retry at a time: concurrent retries each observe a stale
              // snapshot of the other rows and would skip the final cleanup.
              onRetry={result?.status === "failed" && result.retryable && !retrying ? () => void retry(agentId) : undefined}
              onInstallRuntime={missingRuntime && !runtimeBusy && !retrying
                ? () => void installMissingRuntime(missingRuntime.id, missingRuntime.name)
                : undefined}
              runtimeName={missingRuntime?.name}
            />
          );
        })}
      </div>
      {state.activationProbe && !state.activationProbe.ok ? <div className="notice notice-warning">{state.activationProbe.message}</div> : null}
      {/* A run that never started produces no rows, and ACTIVATION_FAILED only
          appends to the log -- which sits behind a collapsed disclosure. So a
          collision with another install left the page showing nothing at all. */}
      {blockedMessage ? <div className="notice notice-error">{blockedMessage}</div> : null}
      {/* The launch commands belong to the moment installation finishes, not to
          the overview a user opens every day. Shown for a partial run too: the
          commands belong to the Agents that succeeded, and hiding them because a
          different Agent failed withheld the next step for work that was done. */}
      {(allDone || anyConfigured) && state.activationNext ? (
        <section className="next-command-section">
          <h2>{t("下一步命令")}</h2>
          <pre>{state.activationNext}</pre>
        </section>
      ) : null}
      <LogDisclosure log={state.activationLog} open={state.activationState === "loading" || retrying !== null} />
    </PageScaffold>
  );
}
