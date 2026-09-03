import { AppWindow, Download, History, Play, Plus, RefreshCw, SlidersHorizontal, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";

import { api, describeFailure } from "../backend/api";
import { useI18n } from "../i18n";
import { converterProfileName } from "../state/conversion";
import { useConversationMigration } from "../hooks/useConversationMigration";
import { taskCanceller, taskKey, useTaskCenter, useTaskRoute } from "../state/TaskCenterContext";
import type { DesktopAgentStatus, ProfileSummary } from "../types/api";
import { DownloadProgress } from "./DownloadProgress";
import { EditionTag } from "./EditionTag";
import { AgentActionMenu, type AgentActionMenuItem } from "./AgentActionMenu";
import { AgentIcon } from "./icons/agents";

interface DesktopAppSectionProps {
  app: DesktopAgentStatus;
  onChanged: () => void | Promise<void>;
  onSetup?: (agentId: string) => void;
  onConfigure?: () => void;
  profile?: ProfileSummary;
  providerName?: string;
  model?: string;
  /** The overview passes false so uninstalled apps are not rendered there. */
  showUninstalled?: boolean;
  showHeading?: boolean;
}

type Action = "install" | "open";

/**
 * How long "{name} 已打开" stays on screen.
 *
 * Long enough to be read, short enough that it does not become furniture. This
 * only bounds the launch confirmation: an install outcome lives in the Task
 * Center until the user dismisses it, and a failure stays until the next
 * attempt, because both carry something still worth acting on.
 */
const LAUNCH_NOTICE_MS = 4000;

export function DesktopAppSection({ app: desktopApp, onChanged, onSetup, onConfigure, profile, providerName, model, showUninstalled = true, showHeading = true }: DesktopAppSectionProps) {
  const { t } = useI18n();
  const profileName = profile ? converterProfileName(profile.id, profile.label || profile.id, t) : desktopApp.profileId || "";
  const { startTask, finishTask, setTaskCanceller, taskFor } = useTaskCenter();
  const route = useTaskRoute();
  const migration = useConversationMigration();
  const [pending, setPending] = useState<Action | "">("");
  const [localNotice, setLocalNotice] = useState("");
  const [localFailure, setLocalFailure] = useState("");

  if (!desktopApp?.supported || (!showUninstalled && !desktopApp.installed)) return null;

  // A download outlives this component: the user can navigate away while it
  // runs, which unmounts the row and drops any local state. The in-flight flag
  // and the outcome therefore live in the Task Center provider above route content,
  // so both the bar and the final verdict survive the round trip.
  const installTaskID = taskKey("install", desktopApp.id);
  const installTask = taskFor(installTaskID);
  const downloading = installTask?.state === "running";
  const busy = Boolean(pending) || downloading;
  const notice = localNotice || (installTask?.state === "success" ? installTask.message : "");
  const failure = localFailure || (!localNotice && installTask?.state === "failure" ? installTask.message : "");

  // Launching an app says everything it has to say the moment it appears, so it
  // retires itself. Previously nothing cleared it: it sat there until the user
  // launched again or navigated away, which unmounted the row.
  useEffect(() => {
    if (!localNotice) return;
    const timer = setTimeout(() => setLocalNotice(""), LAUNCH_NOTICE_MS);
    return () => clearTimeout(timer);
  }, [localNotice]);

  const run = async (action: Action) => {
    const downloads = action === "install";
    if (downloads && !startTask({
      id: installTaskID,
      kind: "install",
      target: desktopApp.id,
      title: t("安装 {name}", { name: desktopApp.name }),
      route,
      progressTarget: desktopApp.id,
    })) return;
    setPending(action);
    setLocalNotice("");
    setLocalFailure("");
    // Opening the app is not a download, so it gets no shared bar. Terminal
    // install cards stay in the center until the user dismisses them.
    try {
      const request = action === "install" ? api.installDesktopAgent(desktopApp.id) : null;
      if (request) setTaskCanceller(installTaskID, taskCanceller(request));
      const result = request ? await request : null;
      let message: string;
      if (action === "open") {
        await api.openDesktopAgent(desktopApp.id);
        message = t("{name} 已打开", { name: desktopApp.name });
      } else if (result?.status === "installed") {
        message = t("{name} 安装完成", { name: desktopApp.name });
      } else if (result?.status === "already-installed") {
        message = t("{name} 已安装", { name: desktopApp.name });
      } else {
        message = t("官方安装器已启动");
      }
      // Recorded in the provider, so it still reaches the user if this row
      // unmounted while the install was running.
      if (downloads) finishTask(installTaskID, { kind: "success", message });
      else setLocalNotice(message);
      await onChanged();
    } catch (error) {
      const message = describeFailure(error, t("{name} 操作失败", { name: desktopApp.name }), t).message;
      if (downloads) finishTask(installTaskID, { kind: "failure", message });
      else setLocalFailure(message);
    } finally {
      setPending("");
    }
  };
  const menuItems: AgentActionMenuItem[] = desktopApp.installed ? [
    ...(desktopApp.id === "chatgpt-desktop" ? [{
      id: "migration",
      label: migration.running ? t("迁移中") : t("迁移对话"),
      icon: History,
      onSelect: migration.run,
      disabled: busy || migration.running,
    }] : []),
    {
      id: "refresh",
      label: t("刷新状态"),
      icon: RefreshCw,
      onSelect: onChanged,
      disabled: busy || migration.running,
    },
  ] : [];

  return (
    <section className={`overview-section desktop-app-section${showHeading ? "" : " desktop-app-item"}`}>
      {showHeading ? (
        <div className="section-heading">
          <div>
            <h2>{t("桌面 Agent")}</h2>
          </div>
        </div>
      ) : null}
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
            <span>{desktopApp.manualInstall ? t("配置完成后前往官网自行安装") : t("按引导安装桌面 Agent")}</span>
            {!desktopApp.manualInstall ? (
              <button className="button button-primary" type="button" aria-label={t("安装")} onClick={() => onSetup ? onSetup(desktopApp.id) : void run("install")} disabled={busy}>
                {pending === "install" || downloading ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Plus size={16} />}
                {pending === "install" || downloading ? t("安装中") : t("安装桌面 Agent")}
              </button>
            ) : null}
          </div>
        ) : null}
        <div className="desktop-app-summary">
          <div className="desktop-app-identity">
            {/* The Agent's own id, never a literal. This rendered agentId="codex"
                for every desktop Agent, so WorkBuddy -- a different vendor's
                product -- displayed OpenAI's mark. A literal also bypasses
                AgentIcon's fallback, which is what handles an Agent that has no
                mark of its own. */}
            <span className="desktop-app-icon"><AgentIcon agentId={desktopApp.id} size={20} /></span>
            <span>
              <strong>{desktopApp.name}</strong>
              <EditionTag edition={desktopApp.edition} />
            </span>
          </div>
          {/* Same token strip as the CLI rows. Four labelled fact columns made
              this card twice the height of the rows beneath it for the same
              four values. */}
          <div className="desktop-app-meta">
            <span className={`agent-manage-pill${profileName ? "" : " is-muted"}`} title={t("配置模版")}>
              {profileName || t("无配置模版")}
            </span>
            <span className={`agent-manage-pill${providerName || profile?.provider ? "" : " is-muted"}`} title={t("模型服务")}>
              {providerName || profile?.provider ? <i aria-hidden="true" /> : null}
              {providerName || profile?.provider || t("无模型服务")}
            </span>
            {model || profile?.model ? (
              <span className="agent-manage-pill agent-manage-model" title={t("模型")}>
                {model || profile?.model}
              </span>
            ) : null}
            {desktopApp.version ? (
              <span className="agent-manage-pill agent-manage-version" title={t("版本")}>
                {desktopApp.version}
              </span>
            ) : null}
          </div>
        </div>
        <div className="desktop-app-actions">
          {desktopApp.installed ? (
            <>
              <AgentActionMenu label={t("{name} 更多操作", { name: desktopApp.name })} items={menuItems} />
              {onConfigure ? (
                <button className="button button-secondary" type="button" onClick={onConfigure} disabled={busy}>
                  <SlidersHorizontal size={15} />
                  {t("配置")}
                </button>
              ) : null}
              <button className="button button-primary" type="button" onClick={() => void run("open")} disabled={busy}>
                {pending === "open" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Play size={15} aria-hidden="true" />}
                {t("启动")}
              </button>
            </>
          ) : (
            <button className="button button-primary" type="button" onClick={() => desktopApp.manualInstall ? onSetup?.(desktopApp.id) : onSetup ? onSetup(desktopApp.id) : void run("install")} disabled={busy || (desktopApp.manualInstall && !onSetup)}>
              {desktopApp.manualInstall ? <SlidersHorizontal size={15} /> : pending === "install" || downloading ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Download size={15} aria-hidden="true" />}
              {desktopApp.manualInstall ? t("配置") : pending === "install" || downloading ? t("安装中") : t("安装桌面 Agent")}
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
        {/* No local flag in the condition: DownloadProgress reads the shared
            in-flight state itself, so navigating away and back keeps the bar. */}
        <DownloadProgress target={desktopApp.id} />
      </div>
    </section>
  );
}
