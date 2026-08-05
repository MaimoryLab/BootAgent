import { PackageOpen, Plus, RefreshCw, Terminal } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { AgentManageRow } from "../components/AgentManageRow";
import { DesktopAppSection } from "../components/DesktopAppSection";
import { PageScaffold } from "../components/PageScaffold";
import { RuntimeSection } from "../components/RuntimeSection";
import { useI18n } from "../i18n";
import { profileAgentIdForDesktop } from "../state/desktopSetup";
import { useWizard } from "../state/WizardContext";
import type { AgentStatus } from "../types/api";

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
  const editDesktopProfile = () => {
    if (status?.desktopAgent?.id) navigate(`/agents/${status.desktopAgent.id}`);
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
  const desktopInstalled = Boolean(status.desktopAgent?.installed);
  const profiles = new Map(status.profiles.map((profile) => [profile.id, profile]));
  const profileForAgent = (agentId: string, agent: AgentStatus) => {
    if (agent.profileId) return profiles.get(agent.profileId);
    const active = status.activeProfile ? profiles.get(status.activeProfile) : undefined;
    const protocol = status.catalog.find((item) => item.id === agentId)?.protocol;
    return active?.protocol === protocol ? active : undefined;
  };
  const desktopBinding = status.agents[profileAgentIdForDesktop(status.desktopAgent)];
  const desktopProfileID = status.desktopAgent.profileId || desktopBinding?.profileId;
  const desktopProfile = desktopProfileID
    ? status.profiles.find((profile) => profile.id === desktopProfileID)
    : undefined;
  const desktopProviderName = desktopProfile
    ? status.providers[desktopProfile.provider]?.name || desktopProfile.provider
    : desktopBinding?.provider
      ? status.providers[desktopBinding.provider]?.name || desktopBinding.provider
      : undefined;
  const desktopModel = desktopProfile?.model || desktopBinding?.model || undefined;

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
          {installed.length || desktopInstalled ? (
            <button className="button button-secondary" type="button" onClick={startSetup}>
              <Plus size={15} />
              {t("安装命令行 Agent")}
            </button>
          ) : null}
        </>
      }
    >
      {installed.length ? (
        <section className="overview-section">
          <div className="section-heading">
            <div><h2>{t("命令行 Agent")}</h2><p>{t("共 {count} 个", { count: installed.length })}</p></div>
          </div>
          <div className="agent-manage-list">
            {installed.map((item) => {
              const agent = status.agents[item.id];
              if (!agent) return null;
              const profile = profileForAgent(item.id, agent);
              return <AgentManageRow
                key={item.id}
                agentId={item.id}
                catalog={item}
                status={agent}
                providers={status.providers}
                profileName={profile?.label || profile?.id || ""}
                profile={profile}
                onChanged={refreshStatus}
              />;
            })}
          </div>
        </section>
      ) : (
        <section className="overview-section">
          <div className="section-heading">
            <div><h2>{t("命令行 Agent")}</h2><p>{t("共 {count} 个", { count: 0 })}</p></div>
          </div>
          <div className="uninstalled-agent-action">
            <span className="visually-hidden">{t("尚未安装任何 Agent")}</span>
            <Terminal size={28} aria-hidden="true" />
            <span>{t("按引导安装命令行 Agent")}</span>
            <button className="button button-primary" type="button" aria-label={t("安装 Agent")} onClick={startSetup}>
              <Plus size={16} />
              {t("安装命令行 Agent")}
            </button>
          </div>
        </section>
      )}

      <DesktopAppSection
        app={status.desktopAgent}
        profile={desktopProfile}
        providerName={desktopProviderName}
        model={desktopModel}
        onChanged={refreshStatus}
        onConfigure={editDesktopProfile}
        showUninstalled
      />
      <RuntimeSection runtimes={status.runtimes ?? []} onInstalled={refreshStatus} />
    </PageScaffold>
  );
}
