import { AppWindow, Download, History, Play, Plus, RefreshCw, SlidersHorizontal, TriangleAlert } from "lucide-react";
import { useState } from "react";

import { api, describeFailure } from "../backend/api";
import { useI18n } from "../i18n";
import { converterProfileName, isConverterID } from "../state/conversion";
import { useFeedback } from "../state/FeedbackContext";
import { useConversationMigration } from "../hooks/useConversationMigration";
import { taskCanceller, taskKey, useTaskCenter, useTaskRoute } from "../state/TaskCenterContext";
import type { DesktopAgentStatus, ProfileSummary } from "../types/api";
import { ActionButton } from "./ActionButton";
import { DownloadProgress } from "./DownloadProgress";
import { EditionTag } from "./EditionTag";
import { AgentActionMenu, type AgentActionMenuItem } from "./AgentActionMenu";
import { DetailsDisclosure } from "./DetailsDisclosure";
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

export function DesktopAppSection({ app: desktopApp, onChanged, onSetup, onConfigure, profile, providerName, model, showUninstalled = true, showHeading = true }: DesktopAppSectionProps) {
  const { t } = useI18n();
  // The overview passes the model service behind the adapter for an app in
  // compatibility mode; the app itself is still bound to its converter Profile.
  const viaConversion = Boolean(profile && desktopApp.profileId && isConverterID(desktopApp.profileId) && profile.id !== desktopApp.profileId);
  const profileName = profile
    ? viaConversion ? t("{name}（兼容模式）", { name: profile.label || profile.id }) : converterProfileName(profile.id, profile.label || profile.id, t)
    : desktopApp.profileId || "";
  const { startTask, finishTask, setTaskCanceller, taskFor } = useTaskCenter();
  const route = useTaskRoute();
  const migration = useConversationMigration();
  const feedback = useFeedback();
  const [installing, setInstalling] = useState(false);
  const [opening, setOpening] = useState(false);
  // The last failure to open the app, kept so the row can still name it after
  // its receipt is closed. Cleared by the next open that succeeds.
  const [openFailure, setOpenFailure] = useState<unknown>(null);

  if (!desktopApp?.supported || (!showUninstalled && !desktopApp.installed)) return null;

  // A download outlives this component: the user can navigate away while it
  // runs, which unmounts the row and drops any local state. The in-flight flag
  // and the outcome therefore live in the Task Center provider above route content,
  // so both the bar and the final verdict survive the round trip.
  const installTaskID = taskKey("install", desktopApp.id);
  const installTask = taskFor(installTaskID);
  const downloading = installTask?.state === "running";
  const busy = installing || opening || downloading;
  const openKey = `open:${desktopApp.id}`;
  // An install outcome lives in the Task Center; the row keeps only a marker for
  // a failure, which reopens its reason as a receipt. Neither is written into
  // the card, where it pushed the rows below it down.
  const failureMarker = openFailure
    ? { label: t("启动失败"), reopen: () => feedback.failure(openFailure, t("无法打开 {name}", { name: desktopApp.name }), { key: openKey }) }
    : installTask?.state === "failure"
      ? { label: t("安装失败"), reopen: () => feedback.show({ key: installTaskID, tone: "failure", message: t("无法安装 {name}", { name: desktopApp.name }), detail: installTask.message || undefined }) }
      : null;

  const install = async () => {
    if (!startTask({
      id: installTaskID,
      kind: "install",
      target: desktopApp.id,
      title: t("安装 {name}", { name: desktopApp.name }),
      route,
      progressTarget: desktopApp.id,
    })) return;
    setInstalling(true);
    try {
      const request = api.installDesktopAgent(desktopApp.id);
      setTaskCanceller(installTaskID, taskCanceller(request));
      const result = await request;
      const message = result?.status === "installed"
        ? t("{name} 安装完成", { name: desktopApp.name })
        : result?.status === "already-installed"
          ? t("{name} 已安装", { name: desktopApp.name })
          : t("官方安装器已启动");
      // Recorded in the provider, so it still reaches the user if this row
      // unmounted while the install was running.
      finishTask(installTaskID, { kind: "success", message });
      await onChanged();
    } catch (error) {
      finishTask(installTaskID, { kind: "failure", message: describeFailure(error, t("{name} 操作失败", { name: desktopApp.name }), t).message });
    } finally {
      setInstalling(false);
    }
  };

  // Opening the app is not a download, so it gets no task and no shared bar:
  // the button confirms it in place, and a failure goes to a receipt.
  const open = async () => {
    setOpening(true);
    try {
      await api.openDesktopAgent(desktopApp.id);
      setOpenFailure(null);
    } catch (error) {
      setOpenFailure(error);
      throw error;
    } finally {
      setOpening(false);
    }
    // Not awaited: a slow status refresh would hold the button on its spinner
    // after the app is already on screen.
    void onChanged();
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
      {desktopApp.inspectionUnavailable ? (
        <div className="notice notice-warning desktop-app-notice">
          <TriangleAlert size={15} aria-hidden="true" />
          <span>{desktopApp.installed ? t("读不到这个应用的版本，不影响启动和使用。") : t("暂时无法检查这个应用是否已安装。")}</span>
        </div>
      ) : null}
      <div className={`desktop-app-row${desktopApp.installed ? "" : " is-uninstalled"}`}>
        {!desktopApp.installed ? (
          <div className="uninstalled-agent-action">
            <AppWindow size={28} aria-hidden="true" />
            <span>{desktopApp.manualInstall ? t("配置完成后前往官网自行安装") : t("按引导安装桌面 Agent")}</span>
            {!desktopApp.manualInstall ? (
              <button className="button button-primary" type="button" aria-label={t("安装")} onClick={() => onSetup ? onSetup(desktopApp.id) : void install()} disabled={busy}>
                {installing || downloading ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Plus size={16} />}
                {installing || downloading ? t("安装中") : t("安装桌面 Agent")}
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
            {failureMarker ? (
              <button className="agent-manage-state is-error" type="button" onClick={failureMarker.reopen} title={t("查看原因")}>
                {failureMarker.label}
              </button>
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
              <ActionButton
                icon={Play}
                label={t("启动")}
                doneLabel={t("已打开")}
                failureMessage={t("无法打开 {name}", { name: desktopApp.name })}
                feedbackKey={openKey}
                onAction={open}
                disabled={busy && !opening}
              />
            </>
          ) : (
            <button className="button button-primary" type="button" onClick={() => desktopApp.manualInstall ? onSetup?.(desktopApp.id) : onSetup ? onSetup(desktopApp.id) : void install()} disabled={busy || (desktopApp.manualInstall && !onSetup)}>
              {desktopApp.manualInstall ? <SlidersHorizontal size={15} /> : installing || downloading ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Download size={15} aria-hidden="true" />}
              {desktopApp.manualInstall ? t("配置") : installing || downloading ? t("安装中") : t("安装桌面 Agent")}
            </button>
          )}
        </div>
        {/* File locations are for whoever wants to look; using the app needs none of them. */}
        <DetailsDisclosure
          className="desktop-app-details"
          rows={[
            { label: t("设置文件"), value: desktopApp.configPath, code: true },
            { label: t("共用设置"), value: desktopApp.configSharedWith ? t("和 {name} 用同一份设置，安装和启动不会改动它", { name: desktopApp.configSharedWith }) : "", wide: true },
            { label: t("程序位置"), value: desktopApp.installed ? desktopApp.path : "", code: true },
          ]}
        />
        {/* No local flag in the condition: DownloadProgress reads the shared
            in-flight state itself, so navigating away and back keeps the bar. */}
        <DownloadProgress target={desktopApp.id} />
      </div>
    </section>
  );
}
