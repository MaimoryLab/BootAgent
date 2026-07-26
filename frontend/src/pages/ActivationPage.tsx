import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api } from "../api/client";
import { AgentProgressRow } from "../components/AgentProgressRow";
import { LogDisclosure } from "../components/LogDisclosure";
import { PageScaffold } from "../components/PageScaffold";
import { useWizard } from "../state/WizardContext";
import type { InstallRequest } from "../types/api";

export function ActivationPage() {
  const navigate = useNavigate();
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
      api_base_url: state.provider === "custom" ? state.customBaseUrl : "",
      api_key: state.configMode === "provider" ? secret.keyRef.current : "",
      model: state.model || "gpt-4.1",
      configure: state.configMode === "provider",
      install_agent: state.installMissingAgents,
      skip_test: state.configMode !== "provider",
      locked_version: true,
    }),
    [secret.keyRef, state.configMode, state.customBaseUrl, state.installMissingAgents, state.model, state.provider],
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
      dispatch({ type: "ACTIVATION_FAILED", message: error instanceof Error ? error.message : "激活失败" });
    }
  }, [dispatch, refreshStatus, requestFor, secret, state.selectedAgentIds]);

  useEffect(() => {
    if (!started.current) {
      started.current = true;
      void activate();
    }
  }, [activate]);

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
      dispatch({ type: "ACTIVATION_FAILED", message: error instanceof Error ? error.message : "重试失败" });
    } finally {
      setRetrying(null);
    }
  };

  const allDone = state.activationState === "success";
  return (
    <PageScaffold
      title={state.activationState === "loading" ? "正在激活" : allDone ? "激活完成" : "需要处理部分问题"}
      description={state.activationState === "loading" ? "安装请求同步执行，完成后将显示每个 Agent 的最终状态。" : "每个 Agent 的结果彼此独立，失败项可以单独重试。"}
      onBack={state.activationState === "loading" ? undefined : () => navigate("/setup/review")}
      primaryLabel={allDone ? "进入总览" : undefined}
      onPrimary={allDone ? () => navigate("/overview") : undefined}
      footerNote={state.activationState === "loading" ? "请保持此窗口打开" : undefined}
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
              onRetry={result?.status === "failed" && result.retryable ? () => void retry(agentId) : undefined}
            />
          );
        })}
      </div>
      {state.activationProbe && !state.activationProbe.ok ? <div className="notice notice-warning">{state.activationProbe.message}</div> : null}
      <LogDisclosure log={state.activationLog} />
    </PageScaffold>
  );
}
