import { RefreshCw, RotateCcw } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { AgentManageRow, isBehind } from "../components/AgentManageRow";
import { PageScaffold } from "../components/PageScaffold";
import { StatusBadge } from "../components/StatusBadge";
import { useWizard } from "../state/WizardContext";

export function EnvironmentOverviewPage() {
  const navigate = useNavigate();
  const { state, dispatch, refreshStatus } = useWizard();
  const [showGuideOnly, setShowGuideOnly] = useState(false);
  const status = state.status;

  if (state.statusState === "loading" && !status) {
    return (
      <PageScaffold title="环境总览">
        <div className="loading-block"><span className="spinner" />正在读取环境状态</div>
      </PageScaffold>
    );
  }

  const catalogById = new Map(status?.catalog.map((item) => [item.id, item]) ?? []);
  const managed = (status?.catalog ?? []).filter((item) => item.configMode === "auto");
  const guideOnly = (status?.catalog ?? []).filter((item) => item.configMode !== "auto");

  // Either kind of evidence counts as "configured". A per-Agent binding covers
  // an Agent set up through "oneagent agent set", which has no environment
  // profile; the profile covers a wizard run whose bindings have not been
  // re-read yet. Requiring only bindings would report a freshly finished wizard
  // as "nothing configured".
  const configuredCount = managed.filter((item) => status?.agents[item.id]?.provider).length;
  const hasAnyConfiguration = Boolean(status?.environment) || configuredCount > 0;

  if (!status || !hasAnyConfiguration) {
    return (
      <PageScaffold
        title="环境总览"
        description="还没有 Agent 指向任何模型服务。"
        primaryLabel="开始配置"
        onPrimary={() => navigate("/setup/agents")}
      >
        <div className="empty-overview">
          <RotateCcw size={28} />
          <strong>尚未配置任何 Agent</strong>
          <span>{status?.environmentError || "完成一次配置后，这里会列出每个 Agent 指向的 Provider 与模型，并可随时单独调整。"}</span>
        </div>
      </PageScaffold>
    );
  }

  // Behind means older than the locked version, not merely different: a user
  // who upgraded an Agent themselves is ahead, and counting that as an update
  // would nag them to downgrade.
  const behind = managed.filter((item) => {
    const agent = status.agents[item.id];
    return Boolean(
      agent?.installed && agent.version && agent.lockedVersion && isBehind(agent.version, agent.lockedVersion),
    );
  });
  const unconfigured = managed.filter((item) => status.agents[item.id]?.installed && !status.agents[item.id]?.provider);

  return (
    <PageScaffold
      title="环境总览"
      titleBadge={behind.length ? `${behind.length} 项可更新` : ""}
      primaryLabel="新建配置"
      onPrimary={() => {
        dispatch({ type: "RESET_SETUP" });
        navigate("/setup/agents");
      }}
      secondaryAction={
        <button
          className="button button-secondary"
          type="button"
          onClick={() => void refreshStatus()}
          disabled={state.statusState === "loading"}
        >
          <RefreshCw size={15} className={state.statusState === "loading" ? "spin" : ""} />
          刷新状态
        </button>
      }
    >
      <section className="overview-section">
        <div className="agent-manage-list">
          {managed.map((item) => {
            const agent = status.agents[item.id];
            if (!agent) return null;
            return (
              <AgentManageRow
                key={item.id}
                agentId={item.id}
                catalog={catalogById.get(item.id)}
                status={agent}
                providers={status.providers}
                onActivated={() => void refreshStatus()}
              />
            );
          })}
        </div>
      </section>

      <section className="overview-section">
        <button
          className="disclosure-trigger"
          type="button"
          aria-expanded={showGuideOnly}
          onClick={() => setShowGuideOnly((value) => !value)}
        >
          仅引导的 Agent（{guideOnly.length}）
        </button>
        {showGuideOnly ? (
          <div className="overview-agent-list">
            {guideOnly.map((item) => (
              <div className="overview-agent-row" key={item.id}>
                <span>
                  <strong>{item.name}</strong>
                  <small>{item.platformNote || "按官方文档手动配置"}</small>
                </span>
                <StatusBadge tone="neutral">仅引导</StatusBadge>
              </div>
            ))}
          </div>
        ) : null}
      </section>

    </PageScaffold>
  );
}
