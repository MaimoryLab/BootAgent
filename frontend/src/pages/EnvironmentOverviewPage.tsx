import { ArrowRight, RefreshCw, RotateCcw } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { EnvironmentSummary } from "../components/EnvironmentSummary";
import { PageScaffold } from "../components/PageScaffold";
import { StatusBadge } from "../components/StatusBadge";
import { useWizard } from "../state/WizardContext";

export function EnvironmentOverviewPage() {
  const navigate = useNavigate();
  const { state, dispatch, refreshStatus } = useWizard();
  const environment = state.status?.environment;

  if (state.statusState === "loading" && !state.status) {
    return <PageScaffold title="环境总览"><div className="loading-block"><span className="spinner" />正在读取环境摘要</div></PageScaffold>;
  }

  if (!environment) {
    return (
      <PageScaffold
        title="环境总览"
        description="当前还没有可用的 OneAgent 环境摘要。"
        primaryLabel="开始激活"
        onPrimary={() => navigate("/setup/agents")}
      >
        <div className="empty-overview">
          <RotateCcw size={28} />
          <strong>尚未完成环境激活</strong>
          <span>{state.status?.environmentError || "选择 Agent 并完成一次配置后，这里会显示长期环境状态。"}</span>
        </div>
      </PageScaffold>
    );
  }

  return (
    <PageScaffold
      title="环境总览"
      description="查看已激活的 Provider、模型、Agent 和本机配置状态。"
      primaryLabel="重新配置"
      onPrimary={() => {
        dispatch({ type: "RESET_SETUP" });
        navigate("/setup/agents");
      }}
      secondaryAction={
        <button className="button button-secondary" type="button" onClick={() => void refreshStatus()} disabled={state.statusState === "loading"}>
          <RefreshCw size={15} className={state.statusState === "loading" ? "spin" : ""} />
          刷新状态
        </button>
      }
    >
      <EnvironmentSummary environment={environment} probe={state.activationProbe} />
      <section className="overview-section">
        <div className="section-heading">
          <div>
            <h2>Agent</h2>
            <p>当前环境摘要中的开发工具。</p>
          </div>
        </div>
        <div className="overview-agent-list">
          {environment.agent_ids.map((agentId) => {
            const catalog = state.status?.catalog.find((agent) => agent.id === agentId);
            const agent = state.status?.agents[agentId];
            return (
              <div className="overview-agent-row" key={agentId}>
                <span>
                  <strong>{catalog?.name || agentId}</strong>
                  <small>{agent?.config || (catalog?.guideOnly ? "官方引导" : "未发现配置路径")}</small>
                </span>
                <StatusBadge tone={agent?.installed ? "success" : catalog?.guideOnly ? "neutral" : "warning"}>
                  {agent?.installed ? "已安装" : catalog?.guideOnly ? "仅引导" : "待安装"}
                </StatusBadge>
              </div>
            );
          })}
        </div>
      </section>
      {state.activationNext ? (
        <section className="next-command-section">
          <h2>下一步命令</h2>
          <pre>{state.activationNext}</pre>
          <ArrowRight size={17} aria-hidden="true" />
        </section>
      ) : null}
    </PageScaffold>
  );
}
