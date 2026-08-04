import { AppWindow, Download, RefreshCw, TriangleAlert } from "lucide-react";
import { useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";
import type { DesktopAgentStatus } from "../types/api";
import { DownloadProgress } from "./DownloadProgress";
import { StatusBadge } from "./StatusBadge";

interface DesktopAppSectionProps {
  app: DesktopAgentStatus;
  onChanged: () => void | Promise<void>;
}

type Action = "install" | "open" | "installer";

export function DesktopAppSection({ app: desktopApp, onChanged }: DesktopAppSectionProps) {
  const { t } = useI18n();
  const { resetProgress } = useTaskCenter();
  const [pending, setPending] = useState<Action | "">("");
  const [failure, setFailure] = useState("");
  const [notice, setNotice] = useState("");

  if (!desktopApp?.supported) return null;

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
      <div className="desktop-app-row">
        <div className="desktop-app-identity">
          <span className="desktop-app-icon"><AppWindow size={20} aria-hidden="true" /></span>
          <span>
            <strong>{desktopApp.name}</strong>
            <small>{desktopApp.version ? t("版本 {version}", { version: desktopApp.version }) : t("版本未知")}</small>
          </span>
        </div>
        <div className="desktop-app-fact">
          <small>{t("状态")}</small>
          <StatusBadge tone={desktopApp.installed ? "success" : "warning"}>
            {desktopApp.installed ? t("已安装") : t("未安装")}
          </StatusBadge>
        </div>
        {desktopApp.path ? (
          <div className="desktop-app-fact desktop-app-path">
            <small>{t("位置")}</small>
            <span title={desktopApp.path}>{desktopApp.path}</span>
          </div>
        ) : null}
        <div className="desktop-app-actions">
          {desktopApp.installed ? (
            <>
              <button className="button button-secondary" type="button" onClick={() => void run("open")} disabled={Boolean(pending)}>
                {pending === "open" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <AppWindow size={15} aria-hidden="true" />}
                {t("打开")}
              </button>
              <button className="button button-secondary" type="button" onClick={() => void run("installer")} disabled={Boolean(pending)}>
                {pending === "installer" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Download size={15} aria-hidden="true" />}
                {t("更新或重新安装")}
              </button>
            </>
          ) : (
            <button className="button button-primary" type="button" onClick={() => void run("install")} disabled={Boolean(pending)}>
              {pending === "install" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Download size={15} aria-hidden="true" />}
              {pending === "install" ? t("安装中") : t("安装")}
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
