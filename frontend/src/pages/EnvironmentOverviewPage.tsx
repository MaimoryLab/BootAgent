import { PackageOpen, Plus, RefreshCw } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { AgentManageRow } from "../components/AgentManageRow";
import { PageScaffold } from "../components/PageScaffold";
import { RuntimeSection } from "../components/RuntimeSection";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";

export function EnvironmentOverviewPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { state, dispatch, refreshStatus } = useWizard();
  const status = state.status;
  // A fresh run each time: without the reset, the second install would inherit
  // the first one's Agent, model and log.
  const startSetup = () => {
    dispatch({ type: "START_SETUP" });
    navigate("/setup/agents");
  };

  if (state.statusState === "loading" && !status) {
    return (
      <PageScaffold title={t("环境总览")}>
        <div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div>
      </PageScaffold>
    );
  }

  if (!status) {
    return (
      <PageScaffold title={t("环境总览")} description={t("本机已安装 Agent 及其当前配置。")}>
        <div className="empty-overview">
          <PackageOpen size={28} />
          <strong>{t("无法读取环境状态")}</strong>
          <span>{state.statusError || t("请刷新后重试。")}</span>
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
      title={t("环境总览")}
      description={t("本机已安装 Agent 及其当前 Provider、Profile 与模型。")}
      secondaryAction={
        <>
          <button
            className="button button-secondary"
            type="button"
            onClick={() => void refreshStatus()}
            disabled={state.statusState === "loading"}
          >
            <RefreshCw size={15} className={state.statusState === "loading" ? "spin" : ""} />
            {t("刷新状态")}
          </button>
          {installed.length ? (
            <button className="button button-secondary" type="button" onClick={startSetup}>
              <Plus size={15} />
              {t("安装 Agent")}
            </button>
          ) : null}
        </>
      }
    >
      {installed.length ? (
        <section className="overview-section">
          <div className="section-heading">
            <div><h2>{t("已安装 Agent")}</h2><p>{t("共 {count} 个", { count: installed.length })}</p></div>
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
          <strong>{t("尚未安装任何 Agent")}</strong>
          <span>{t("按引导安装第一个 Agent，OneAgent 会写入模型服务配置。")}</span>
          <button className="button button-primary" type="button" onClick={startSetup}>
            <Plus size={16} />
            {t("安装 Agent")}
          </button>
        </div>
      )}

      <RuntimeSection runtimes={status.runtimes ?? []} onInstalled={refreshStatus} />
    </PageScaffold>
  );
}
