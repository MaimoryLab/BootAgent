import { AppWindow, CheckCircle2, Download, RefreshCw, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { DownloadProgress } from "../components/DownloadProgress";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { desktopProfileUsable, desktopProfiles, profileAgentIdForDesktop } from "../state/desktopSetup";
import { useTaskCenter } from "../state/TaskCenterContext";
import { useWizard } from "../state/WizardContext";

type RunState = "idle" | "running" | "success" | "error";

export function DesktopInstallPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, refreshStatus } = useWizard();
  const { resetProgress } = useTaskCenter();
  const [runState, setRunState] = useState<RunState>("idle");
  const [failure, setFailure] = useState("");
  const [message, setMessage] = useState("");
  const status = state.status;
  const app = status?.desktopAgent;
  const profile = app && status
    ? desktopProfiles(status, app).find((item) => item.id === state.desktopProfileId && desktopProfileUsable(status, item))
    : undefined;

  if (!app || !profile || !state.selectedAgentIds.includes(app.id)) {
    return (
      <PageScaffold title={t("安装桌面 Agent")} primaryLabel={t("返回")} onPrimary={() => navigate("/setup/desktop/profile")}>
        <div className="empty-overview">{t("请先选择一个 Profile")}</div>
      </PageScaffold>
    );
  }

  const install = async () => {
    if (runState === "running") return;
    setRunState("running");
    setFailure("");
    setMessage("");
    resetProgress(app.id);
    try {
      const result = app.installed ? undefined : await api.installDesktopAgent();
      const configured = await api.configureDesktopAgent(app.id, profile.id);
      setMessage(configured.message || result?.message || t("应用完成"));
      setRunState("success");
      await refreshStatus();
    } catch (error) {
      setFailure(describeError(error, t("桌面 Agent 安装失败")).message);
      setRunState("error");
    } finally {
      resetProgress(app.id);
    }
  };

  const shared = profileAgentIdForDesktop(app) !== app.id;
  const done = runState === "success";

  return (
    <PageScaffold
      title={done ? t("安装完成") : t("确认安装")}
      description={done ? t("桌面 Agent 已安装并应用 Profile。") : t("确认安装桌面 Agent，并应用选定的 Profile。")}
      stepper
      backLabel={t("返回")}
      onBack={() => navigate("/setup/desktop/profile")}
      primaryLabel={done ? t("进入总览") : runState === "running" ? t("安装中") : t("安装")}
      onPrimary={() => (done ? navigate("/overview") : void install())}
      primaryDisabled={runState === "running"}
      footerNote={profile.label}
    >
      {failure ? <div className="notice notice-error"><TriangleAlert size={15} />{failure}</div> : null}
      {message ? <div className="notice notice-success"><CheckCircle2 size={15} />{message}</div> : null}
      <section className="review-grid desktop-install-summary">
        <div className="review-group">
          <h2>{t("安装目标")}</h2>
          <div className="review-row"><span><AppWindow size={15} />{app.name}</span><strong>{app.installed ? t("已安装，将重新检查") : t("待安装")}</strong></div>
        </div>
        <div className="review-group">
          <h2>{t("配置模板")}</h2>
          <div className="review-row"><span>{profile.label}</span><strong>{profile.provider} · {profile.model}</strong></div>
          <p className="profile-key-hint">{shared ? t("该 Profile 会写入 Codex，共享给 ChatGPT Desktop。") : t("该 Profile 只属于这个桌面 Agent。")}</p>
        </div>
      </section>
      {runState === "running" && !app.installed ? (
        <DownloadProgress target={app.id} pending />
      ) : runState === "running" ? (
        <div className="desktop-install-callout"><RefreshCw size={17} className="spin" />{t("应用中")}</div>
      ) : runState === "idle" || runState === "error" ? (
        <div className="desktop-install-callout"><Download size={17} />{t("安装过程可能需要几分钟，请保持窗口打开。")}</div>
      ) : null}
      {runState === "error" ? <button className="button button-secondary" type="button" onClick={() => void install()}><RefreshCw size={15} />{t("重试")}</button> : null}
    </PageScaffold>
  );
}
