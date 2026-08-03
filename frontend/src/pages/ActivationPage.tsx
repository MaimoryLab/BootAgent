import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { AgentProgressRow } from "../components/AgentProgressRow";
import { LogDisclosure } from "../components/LogDisclosure";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import type { InstallRequest } from "../types/api";

export function ActivationPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, secret, refreshStatus } = useWizard();
  const started = useRef(false);
  const [retrying, setRetrying] = useState<string | null>(null);
  const selectedNames = useMemo(
    () => Object.fromEntries(state.status?.catalog.map((agent) => [agent.id, agent.name]) || []),
    [state.status],
  );

  const requestFor = useCallback(
    (agents: string[], profileAgents?: string[]): InstallRequest => ({
      agents,
      profile_agents: profileAgents,
      provider: state.provider,
      api_base_url: "",
      api_key: secret.keyRef.current,
      // SetupGuard guarantees a model before this page can render.
      model: state.model,
      profile_id: state.profileId,
      profile_label: state.profileLabel,
      configure: true,
      install_agent: state.installMissingAgents,
      skip_test: false,
    }),
    [secret.keyRef, state.installMissingAgents, state.model, state.profileId, state.profileLabel, state.provider],
  );

  const activate = useCallback(async () => {
    dispatch({ type: "ACTIVATION_LOADING", agentIds: state.selectedAgentIds });
    try {
      const response = await api.install(requestFor(state.selectedAgentIds));
      dispatch({
        type: "ACTIVATION_RESULT",
        results: response.results,
        log: response.log,
        probe: response.probe,
        next: response.next,
        replaceAgents: state.selectedAgentIds,
      });
      if (response.ok) {
        secret.clearApiKey();
        await refreshStatus();
      }
    } catch (error) {
      dispatch({ type: "ACTIVATION_FAILED", message: describeError(error, t("激活失败")).message });
    }
  }, [dispatch, refreshStatus, requestFor, secret, state.selectedAgentIds, t]);

  useEffect(() => api.onInstallOutput((output) => dispatch({ type: "ACTIVATION_OUTPUT", output })), [dispatch]);

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
    setRetrying(agentId);
    try {
      const response = await api.install(requestFor([agentId], state.selectedAgentIds));
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
        secret.clearApiKey();
        await refreshStatus();
      }
    } catch (error) {
      dispatch({ type: "ACTIVATION_FAILED", message: describeError(error, t("重试失败")).message });
    } finally {
      setRetrying(null);
    }
  };

  const allDone = state.activationState === "success";
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
