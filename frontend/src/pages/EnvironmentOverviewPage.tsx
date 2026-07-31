import { PackageOpen, RefreshCw } from "lucide-react";

import { AgentManageRow } from "../components/AgentManageRow";
import { PageScaffold } from "../components/PageScaffold";
import { useWizard } from "../state/WizardContext";

export function EnvironmentOverviewPage() {
  const { state, refreshStatus } = useWizard();
  const status = state.status;

  if (state.statusState === "loading" && !status) {
    return (
      <PageScaffold title="环境总览">
        <div className="loading-block"><span className="spinner" />正在读取环境状态</div>
      </PageScaffold>
    );
  }

  if (!status) {
    return (
      <PageScaffold title="环境总览" description="本机已安装 Agent 及其当前配置。">
        <div className="empty-overview">
          <PackageOpen size={28} />
          <strong>无法读取环境状态</strong>
          <span>{state.statusError || "请刷新后重试。"}</span>
        </div>
      </PageScaffold>
    );
  }

  const installed = [...status.catalog]
    .sort((first, second) => first.rank - second.rank)
    .filter((item) => status.agents[item.id]?.installed);
  const profiles = new Map(status.profiles.map((profile) => [profile.id, profile.label]));

  return (
    <PageScaffold
      title="环境总览"
      description="本机已安装 Agent 及其当前 Provider、Profile 与模型。"
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
      {installed.length ? (
        <section className="overview-section">
          <div className="section-heading">
            <div><h2>已安装 Agent</h2><p>共 {installed.length} 个</p></div>
          </div>
          <div className="agent-manage-list">
            {installed.map((item) => {
              const agent = status.agents[item.id];
              if (!agent) return null;
              return <AgentManageRow
                key={item.id}
                agentId={item.id}
                catalog={item}
                status={agent}
                providers={status.providers}
                profileName={agent.profileId ? profiles.get(agent.profileId) || agent.profileId : ""}
              />;
            })}
          </div>
        </section>
      ) : (
        <div className="empty-overview">
          <PackageOpen size={28} />
          <strong>尚未安装任何 Agent</strong>
          <span>在配置模板中创建 Profile 并应用后，已安装的 Agent 会显示在这里。</span>
        </div>
      )}
    </PageScaffold>
  );
}
