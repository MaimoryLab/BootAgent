import { AppWindow, CheckCircle2 } from "lucide-react";
import { useEffect } from "react";
import { useNavigate } from "react-router-dom";

import { PageScaffold } from "../components/PageScaffold";
import { StatusBadge } from "../components/StatusBadge";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import { desktopApps, desktopProtocol } from "../state/desktopSetup";

export function DesktopAgentSelectionPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch } = useWizard();
  const apps = state.status ? desktopApps(state.status) : [];
  const app = apps.find((candidate) => candidate.id === state.selectedAgentIds[0]) || apps[0];
  const selected = state.selectedAgentIds[0] === app?.id;
  const continueSetup = () => {
    if (!app || !state.status) return;
    const protocol = desktopProtocol(state.status, app);
    const hasProfile = Boolean(protocol && state.status?.profiles.some((profile) => profile.protocol === protocol));
    dispatch({ type: "SET_PROFILE_STEP_SKIPPED", value: !hasProfile });
    navigate(hasProfile ? "/setup/profile" : "/setup/provider");
  };

  useEffect(() => {
    if (state.setupKind !== "desktop") dispatch({ type: "START_DESKTOP_SETUP" });
  }, [dispatch, state.setupKind]);

  if (!app) {
    return (
      <PageScaffold title={t("选择 Agent")}>
        <div className="empty-overview">{t("无法读取环境状态")}</div>
      </PageScaffold>
    );
  }

  return (
    <PageScaffold
      title={t("选择 Agent")}
      description={t("选择要安装的桌面 Agent，每次安装一个。")}
      stepper
      primaryLabel={t("继续")}
      onPrimary={continueSetup}
      primaryDisabled={!selected || !app.supported}
      footerNote={selected ? app.name : t("选择一个 Agent")}
    >
      <section className="content-section desktop-agent-choice">
        <div className="section-heading">
          <div>
            <h2>{t("桌面 Agent")}</h2>
            <p>{t("安装应用后，会把选定的 Profile 应用到对应的配置。")}</p>
          </div>
          <AppWindow size={20} aria-hidden="true" />
        </div>
        {apps.map((candidate) => {
          const isSelected = state.selectedAgentIds[0] === candidate.id;
          return (
            <div key={candidate.id}>
              <label className={`agent-row${isSelected ? " is-selected" : ""}${!candidate.supported ? " is-disabled" : ""}`}>
                <input
                  type="radio"
                  name="desktop-agent-choice"
                  checked={isSelected}
                  disabled={!candidate.supported}
                  onChange={() => dispatch({ type: "SELECT_AGENT", agentId: candidate.id })}
                  aria-label={t("选择 {name}", { name: candidate.name })}
                />
                <span className="desktop-app-icon"><AppWindow size={20} aria-hidden="true" /></span>
                <span className="agent-copy">
                  <span className="agent-name-line"><strong>{candidate.name}</strong></span>
                  <span>{candidate.installed ? t("已安装，可直接应用 Profile") : t("安装官方桌面应用")}</span>
                  {candidate.configSharedWith ? <small>{t("与 {name} 共用配置", { name: candidate.configSharedWith })}</small> : null}
                </span>
                <StatusBadge tone={candidate.installed ? "success" : candidate.supported ? "warning" : "neutral"}>
                  {candidate.installed ? t("已安装") : candidate.supported ? t("待安装") : t("不支持")}
                </StatusBadge>
              </label>
              {isSelected && candidate.installed ? <p className="desktop-agent-ready"><CheckCircle2 size={15} />{t("检测到本机已有此应用")}</p> : null}
            </div>
          );
        })}
      </section>
    </PageScaffold>
  );
}
