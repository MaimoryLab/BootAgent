import { AppWindow, Download, Play, Plus, RefreshCw, SlidersHorizontal, TriangleAlert } from "lucide-react";
import { useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";
import type { DesktopAgentStatus, ProfileSummary } from "../types/api";
import { DownloadProgress } from "./DownloadProgress";
import { AgentIcon } from "./icons/agents";

interface DesktopAppSectionProps {
  app: DesktopAgentStatus;
  onChanged: () => void | Promise<void>;
  onSetup?: () => void;
  onConfigure?: () => void;
  profile?: ProfileSummary;
  providerName?: string;
  model?: string;
  /** The overview passes false so uninstalled apps are not rendered there. */
  showUninstalled?: boolean;
}

type Action = "install" | "open" | "installer";

export function DesktopAppSection({ app: desktopApp, onChanged, onSetup, onConfigure, profile, providerName, model, showUninstalled = true }: DesktopAppSectionProps) {
  const { t } = useI18n();
  const { resetProgress } = useTaskCenter();
  const [pending, setPending] = useState<Action | "">("");
  const [failure, setFailure] = useState("");
  const [notice, setNotice] = useState("");

  if (!desktopApp?.supported || (!showUninstalled && !desktopApp.installed)) return null;

  const run = async (action: Action) => {
    const downloading = action === "install" || action === "installer";
    setPending(action);
    setFailure("");
    setNotice("");
    if (downloading) resetProgress(desktopApp.id);
    try {
      const result = action === "install"
        ? await api.installDesktopAgent()
        : action === "installer"
          ? await api.openDesktopAgentInstaller()
          : null;
      if (action === "open") {
        await api.openDesktopAgent();
        setNotice(t("{name} 已打开", { name: desktopApp.name }));
      } else if (result?.status === "installed") {
        setNotice(t("{name} 安装完成", { name: desktopApp.name }));
      } else if (result?.status === "already-installed") {
        setNotice(t("{name} 已安装", { name: desktopApp.name }));
      } else {
        setNotice(t("官方安装器已启动"));
      }
      await onChanged();
    } catch (error) {
      setFailure(describeError(error, t("{name} 操作失败", { name: desktopApp.name })).message);
    } finally {
      setPending("");
      if (downloading) resetProgress(desktopApp.id);
    }
  };

  return (
    <section className="overview-section desktop-app-section">
      <div className="section-heading">
        <div>
          <h2>{t("桌面 Agent")}</h2>
          <p>{t("共 {count} 个", { count: desktopApp.installed ? 1 : 0 })}</p>
        </div>
      </div>
      {failure ? <div className="notice notice-error desktop-app-notice"><TriangleAlert size={15} aria-hidden="true" />{failure}</div> : null}
      {notice ? <div className="notice notice-success desktop-app-notice"><AppWindow size={15} aria-hidden="true" />{notice}</div> : null}
      {desktopApp.inspectionUnavailable ? (
        <div className="notice notice-warning desktop-app-notice">
          <TriangleAlert size={15} aria-hidden="true" />
          {desktopApp.installed ? t("已检测到应用，但版本信息不可用") : t("应用状态检测不可用")}
        </div>
      ) : null}
      <div className={`desktop-app-row${desktopApp.installed ? "" : " is-uninstalled"}`}>
        {!desktopApp.installed ? (
          <div className="uninstalled-agent-action">
            <AppWindow size={28} aria-hidden="true" />
            <span>{t("按引导安装桌面 Agent")}</span>
            <button className="button button-primary" type="button" aria-label={t("安装")} onClick={() => onSetup ? onSetup() : void run("install")} disabled={Boolean(pending)}>
              {pending === "install" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Plus size={16} />}
              {pending === "install" ? t("安装中") : t("安装桌面 Agent")}
            </button>
          </div>
        ) : null}
        <div className="desktop-app-identity">
          <span className="desktop-app-icon"><AgentIcon agentId="codex" size={20} /></span>
          <span>
            <strong>{desktopApp.name}</strong>
          </span>
        </div>
        <div className="desktop-app-fact">
          <small>Provider</small>
          <span title={profile?.provider || undefined}>{providerName || profile?.provider || t("未绑定")}</span>
        </div>
        <div className="desktop-app-fact">
          <small>Profile</small>
          <span title={profile?.id || desktopApp.profileId || undefined}>{profile?.label || desktopApp.profileId || t("未绑定")}</span>
        </div>
        <div className="desktop-app-fact">
          <small>{t("模型")}</small>
          <span title={model || profile?.model || undefined}>{model || profile?.model || t("未记录")}</span>
        </div>
        <div className="desktop-app-fact">
          <small>{t("版本")}</small>
          <span>{desktopApp.version || t("未知")}</span>
        </div>
        <div className="desktop-app-actions">
          {desktopApp.installed ? (
            <>
              {onConfigure ? (
                <button className="button button-secondary" type="button" onClick={onConfigure} disabled={Boolean(pending)}>
                  <SlidersHorizontal size={15} />
                  {t("编辑配置")}
                </button>
              ) : null}
              <button className="button button-secondary" type="button" onClick={() => void run("installer")} disabled={Boolean(pending)}>
                {pending === "installer" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Download size={15} aria-hidden="true" />}
                {t("更新")}
              </button>
              <button className="button button-secondary" type="button" onClick={() => void run("open")} disabled={Boolean(pending)}>
                {pending === "open" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Play size={15} aria-hidden="true" />}
                {t("启动")}
              </button>
            </>
          ) : (
            <button className="button button-primary" type="button" onClick={() => onSetup ? onSetup() : void run("install")} disabled={Boolean(pending)}>
              {pending === "install" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Download size={15} aria-hidden="true" />}
              {pending === "install" ? t("安装中") : t("安装桌面 Agent")}
            </button>
          )}
        </div>
        {desktopApp.configPath ? (
          <p className="desktop-app-note" title={desktopApp.configPath}>
            {desktopApp.configSharedWith
              ? t("与 {name} 共用配置文件 {path}；安装和启动不会改动配置", { name: desktopApp.configSharedWith, path: desktopApp.configPath })
              : t("配置文件：{path}", { path: desktopApp.configPath })}
          </p>
        ) : null}
        {pending === "install" || pending === "installer" ? <DownloadProgress target={desktopApp.id} pending /> : null}
      </div>
    </section>
  );
}
