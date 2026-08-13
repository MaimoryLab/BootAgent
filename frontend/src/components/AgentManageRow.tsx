import { Dialogs } from "@wailsio/runtime";
import { FolderOpen, History, Play, RefreshCw, SlidersHorizontal } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { sourceTranslate, type Translate, useI18n } from "../i18n";
import { taskCanceller, taskKey, updateTaskRoute, useTaskCenter, useTaskRoute } from "../state/TaskCenterContext";
import type { AgentCatalogItem, AgentStatus, ProfileSummary, StatusResponse } from "../types/api";
import { AgentIcon, agentTagline } from "./icons/agents";

type Providers = StatusResponse["providers"];

/**
 * Whether an update can be offered, and the newer version if the registry named
 * one.
 *
 * The npm-managed set used to be a literal in this file, alongside a second
 * literal mapping each Agent to its package name. Both had already drifted:
 * OpenClaw shipped as an npm Agent and was in neither, so it silently lost its
 * update button and its package name in the details. The catalog carries both
 * facts now, from the same agents.lock.json the installer reads.
 */
export function updateOffer(agent: AgentCatalogItem | undefined, status: AgentStatus): { npm: boolean; behind: string } {
  // Not installed means there is nothing to update. `npm update -g` on a package
  // that was never installed exits 0 and does nothing, so offering the button
  // would report success while leaving the Agent missing.
  const npm = agent?.packageManager === "npm" && status.installed;
  if (!npm || !status.version || !status.latestVersion) return { npm: Boolean(npm), behind: "" };
  // Only an older local version is an update. A newer one is normal -- the user
  // upgraded the Agent themselves -- and flagging it would invite a downgrade.
  const behind = compareVersions(status.version, status.latestVersion) < 0 ? status.latestVersion : "";
  return { npm: true, behind };
}

/** -1, 0 or 1 comparing dotted numeric versions; non-numeric parts sort last. */
export function compareVersions(left: string, right: string): number {
  const parse = (value: string) => value.split(/[.+-]/).map((part) => Number.parseInt(part, 10));
  const a = parse(left);
  const b = parse(right);
  for (let i = 0; i < Math.max(a.length, b.length); i += 1) {
    const x = Number.isNaN(a[i]) || a[i] === undefined ? -1 : a[i];
    const y = Number.isNaN(b[i]) || b[i] === undefined ? -1 : b[i];
    if (x !== y) return x < y ? -1 : 1;
  }
  return 0;
}

/** Whether the installed version is older than the locked one. */
export function isBehind(installed: string, locked: string): boolean {
  return compareVersions(installed, locked) < 0;
}

function versionNote(status: AgentStatus, t: Translate = sourceTranslate): { text: string; behind: boolean } | null {
  if (!status.installed || !status.version) return null;
  if (!status.lockedVersion || status.version === status.lockedVersion) {
    // Being current is the normal case and needs no words: the bare version is
    // enough, and five rows each saying "已是最新" is noise.
    return { text: status.version, behind: false };
  }
  // Only an older local version is an update. A newer one is normal — the user
  // upgraded the Agent themselves — and calling it "updatable" would send them
  // to downgrade.
  if (compareVersions(status.version, status.lockedVersion) < 0) {
    // The arrow already says which way this goes.
    return { text: `${status.version} → ${status.lockedVersion}`, behind: true };
  }
  return { text: t("{version}（锁定 {lockedVersion}）", { version: status.version, lockedVersion: status.lockedVersion }), behind: false };
}

/**
 * What to say this Agent currently points at.
 *
 * Two sources: BootAgent's own record of what it wrote, and the Agent's config
 * file as found on disk. Reporting only the first is what made a hand-written
 * configuration render as "未配置" — the file was seen but never read. Where they
 * disagree the file wins, because that is what the Agent will actually use.
 */
export function targetSummary(
  status: AgentStatus,
  providers: Providers,
  t: Translate = sourceTranslate,
): { text: string; note: string } {
  const detected = status.detected;
  if (detected?.unreadable) {
    return { text: t("配置无法解析"), note: detected.unreadable };
  }
  const providerName = status.provider ? providers[status.provider]?.name || status.provider : "";
  // Ours, and the file agrees (or has nothing to add).
  if (status.provider && status.model) {
    const drifted =
      detected && detected.baseUrl && status.baseUrl && detected.baseUrl !== status.baseUrl;
    return {
      text: `${providerName} · ${status.model}`,
      note: drifted ? t("配置文件当前指向 {url}", { url: detected!.baseUrl }) : "",
    };
  }
  // No record of our own, but the file says something.
  if (detected && (detected.baseUrl || detected.model)) {
    const parts = [detected.baseUrl, detected.model].filter(Boolean);
    return {
      text: parts.join(" · "),
      note: detected.managedByBootAgent ? "" : t("检测到的配置，非 BootAgent 写入"),
    };
  }
  return { text: t("未配置"), note: "" };
}

/**
 * One read-only row in the environment overview.
 */
export function AgentManageRow({
  agentId,
  catalog,
  status,
  providers,
  profileName,
  profile,
  onChanged,
  defaultDirectory,
}: {
  agentId: string;
  catalog: AgentCatalogItem | undefined;
  status: AgentStatus;
  providers: Providers;
  profileName: string;
  profile?: ProfileSummary;
  onChanged?: () => void | Promise<void>;
  defaultDirectory?: string;
}) {
  const { t } = useI18n();
  const { startTask, finishTask, setTaskCanceller, taskFor, isTaskRunning } = useTaskCenter();
  const route = useTaskRoute();
  const [launching, setLaunching] = useState(false);
  const [migrating, setMigrating] = useState(false);
  const [localUpdating, setLocalUpdating] = useState(false);
  const [failure, setFailure] = useState("");
  const [notice, setNotice] = useState("");
  const [launchDirectory, setLaunchDirectory] = useState("");
  const [rememberDirectory, setRememberDirectory] = useState(false);
  const [directoryDialog, setDirectoryDialog] = useState(false);
  const updateTaskID = taskKey("update", agentId);
  const updateTask = taskFor(updateTaskID);
  const updating = updateTask?.state === "running" || localUpdating;
  const version = versionNote(status, t);
  const target = targetSummary(status, providers, t);
  const providerId = profile?.provider || status.provider || "";
  const providerName = providerId ? providers[providerId]?.name || providerId : "";
  const profileLabel = profileName || status.profileId || "";
  const model = profile?.model || status.model || status.detected?.model || "";
  const baseUrl = status.baseUrl || status.detected?.baseUrl || "";
  // No "not configured" state here: the Profile and Provider tokens already name
  // whichever piece is absent, so a third word for the same condition only adds
  // a term the user has to map back onto them.
  const updateFailure = updateTask?.state === "failure" ? updateTask.message || t("失败") : "";
  const statusLabel = failure || updateFailure ? t("失败") : !status.installed ? t("未安装") : "";

  // installed is true only when the Agent's command resolved on the managed
  // PATH, so it is already the precise "there is something to launch" signal.
  const canLaunch = status.installed;
  const offer = updateOffer(catalog, status);

  const launch = () => {
    const stored = localStorage.getItem(`bootagent:launch-directory:${agentId}`);
    setLaunchDirectory(stored || defaultDirectory || "");
    setRememberDirectory(Boolean(stored));
    setDirectoryDialog(true);
  };
  const chooseDirectory = async () => {
    try {
      const selected = await Dialogs.OpenFile({ Title: t("选择启动目录"), Directory: launchDirectory || defaultDirectory || undefined, CanChooseDirectories: true, CanChooseFiles: false }) as unknown as string | string[];
      const directory = Array.isArray(selected) ? selected[0] : selected;
      if (directory) setLaunchDirectory(directory);
    } catch { /* cancelled */ }
  };
  const confirmLaunch = async () => {
    if (!launchDirectory.trim()) return;
    if (rememberDirectory) localStorage.setItem(`bootagent:launch-directory:${agentId}`, launchDirectory.trim());
    else localStorage.removeItem(`bootagent:launch-directory:${agentId}`);
    setDirectoryDialog(false);
    setLaunching(true);
    setFailure("");
    try {
      await api.launchAgent(agentId, launchDirectory.trim());
    } catch (error) {
      setFailure(describeFailure(error, t("无法启动 Agent"), t).message);
    } finally {
      setLaunching(false);
    }
  };
  const update = async () => {
    if (!startTask({
      id: updateTaskID,
      kind: "update",
      target: agentId,
      title: t("更新 {name}", { name: catalog?.name || agentId }),
      route: updateTaskRoute(agentId),
    })) return;
    setLocalUpdating(true);
    setFailure("");
    try {
      const request = api.updateAgent(agentId);
      setTaskCanceller(updateTaskID, taskCanceller(request));
      await request;
      finishTask(updateTaskID, { kind: "success", message: t("更新完成") });
      await onChanged?.();
    } catch (error) {
      finishTask(updateTaskID, { kind: "failure", message: describeFailure(error, t("无法更新 Agent"), t).message });
    } finally {
      setLocalUpdating(false);
    }
  };
  const migrateConversations = async () => {
    const confirmLabel = t("继续迁移");
    const choice = await Dialogs.Question({
      Title: t("迁移对话"),
      Message: t("将所有 Codex 与 ChatGPT Desktop 历史对话迁入 BootAgent。此操作不创建备份，无法自动恢复。"),
      Buttons: [{ Label: confirmLabel }, { Label: t("取消"), IsCancel: true }],
    }).catch(() => "");
    if (choice !== confirmLabel) return;
    setMigrating(true);
    setFailure("");
    setNotice("");
    try {
      const result = await api.migrateConversations();
      setNotice(t("已迁移 {files} 个对话文件和 {threads} 条索引记录", { files: result.files, threads: result.threads }));
    } catch (error) {
      setFailure(describeFailure(error, t("无法迁移对话"), t).message);
    } finally {
      setMigrating(false);
    }
  };

  return (
    <div className="agent-manage-row" data-testid={`agent-${agentId}`}>
      <div className="agent-manage-summary">
        <div className="agent-manage-identity">
          <span className="agent-icon" title={agentTagline(agentId, t) || undefined}>
            <AgentIcon agentId={agentId} size={20} />
          </span>
          <span className="agent-manage-identity-copy">
            <strong>{catalog?.name || agentId}</strong>
            {failure || updateFailure ? <small className="agent-manage-note is-error">{failure || updateFailure}</small> : null}
          </span>
        </div>
        {/* Right-aligned, in a fixed order, with the Profile and Provider slots
            always rendered. Dropping an absent Provider slid every later token
            left, so the same field sat in a different position from row to row
            and the strip could not be read down the column. */}
        <div className="agent-manage-meta" aria-label={t("状态")}>
          <span className={`agent-manage-pill${profileLabel ? "" : " is-muted"}`} title={t("配置模版")}>
            {profileLabel || t("无配置模版")}
          </span>
          <span className={`agent-manage-pill${providerName ? "" : " is-muted"}`} title={t("模型服务")}>
            {providerName ? <i aria-hidden="true" /> : null}
            {providerName || t("无模型服务")}
          </span>
          {model ? (
            <span className="agent-manage-pill agent-manage-model" title={t("模型")}>
              {model}
            </span>
          ) : null}
          {version ? (
            <span className={`agent-manage-pill agent-manage-version${version.behind ? " is-behind" : ""}`} title={t("版本")}>
              {version.text}
            </span>
          ) : null}
          {statusLabel ? <span className={`agent-manage-state${failure || updateFailure ? " is-error" : ""}`}>{statusLabel}</span> : null}
        </div>
      </div>
      {notice ? <div className="notice notice-success">{notice}</div> : null}
      {directoryDialog ? (
        <dialog className="transfer-password-dialog" open>
          <form onSubmit={(event) => { event.preventDefault(); void confirmLaunch(); }}>
            <h2>{t("选择启动目录")}</h2>
            <label className="launch-directory-label" htmlFor={`launch-directory-${agentId}`}>{t("启动目录")}</label>
            <div className="launch-directory-input-row">
              <input id={`launch-directory-${agentId}`} value={launchDirectory} onChange={(event) => setLaunchDirectory(event.target.value)} autoFocus spellCheck={false} autoCorrect="off" autoCapitalize="none" />
              <button className="icon-button" type="button" onClick={() => void chooseDirectory()} title={t("选择目录")} aria-label={t("选择目录")}><FolderOpen size={18} /></button>
            </div>
            <label className="launch-remember-row"><input type="checkbox" checked={rememberDirectory} onChange={(event) => setRememberDirectory(event.target.checked)} /><span>{t("记住此 Agent 的目录")}</span></label>
            <footer><button className="button button-secondary" type="button" onClick={() => setDirectoryDialog(false)}>{t("取消")}</button><button className="button button-primary" type="submit">{t("启动")}</button></footer>
          </form>
        </dialog>
      ) : null}
      <div className="agent-manage-actions">
        {/* Always in the row, not only when the Agent cannot launch. Configuring
            an installed Agent was previously reachable only by opening <details>,
            which made the common case the hidden one. */}
        {agentId === "codex" ? (
          <button className="button button-secondary" type="button" onClick={() => void migrateConversations()} disabled={migrating || launching || updating}>
            {migrating ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <History size={15} aria-hidden="true" />}
            {migrating ? t("迁移中") : t("迁移对话")}
          </button>
        ) : null}
        {offer.npm ? (
          <button
            className="button button-secondary agent-update-button"
            type="button"
            onClick={() => void update()}
            disabled={updating || launching || isTaskRunning(taskKey("install", agentId))}
            title={offer.behind ? t("有新版本 {version}，点击更新", { version: offer.behind }) : t("执行 npm update")}
          >
            <RefreshCw size={15} className={updating ? "spin" : ""} aria-hidden="true" />
            {updating ? t("更新中") : t("更新")}
            {/* The dot is decoration; the title above is what carries the new
                version to a screen reader, so this is not announced twice. */}
            {offer.behind && !updating ? <span className="agent-update-dot" aria-hidden="true" /> : null}
          </button>
        ) : null}
        <Link
          className="button button-secondary"
          to={`/agents/${agentId}`}
          title={t("编辑这个 Agent 关联的配置模版")}
        >
          <SlidersHorizontal size={15} aria-hidden="true" />
          {t("配置")}
        </Link>

        {canLaunch ? (
          <button
            className="button button-primary"
            type="button"
            onClick={() => void launch()}
            disabled={launching}
            title={t("在新终端窗口中启动，并载入 BootAgent 写入的配置")}
          >
            {launching ? <RefreshCw size={14} className="spin" aria-hidden="true" /> : <Play size={15} aria-hidden="true" />}
            {t("启动")}
          </button>
        ) : null}
      </div>
      <details className="agent-manage-details">
        <summary>{t("详情")}</summary>
        <div className="agent-manage-details-body">
          {providerName ? (
            <div><small>{t("模型服务")}</small><span>{providerName}</span></div>
          ) : null}
          {profileLabel ? (
            <div><small>{t("配置模版")}</small><span>{profileLabel}</span></div>
          ) : null}
          {model ? (
            <div><small>{t("模型")}</small><span className="agent-manage-detail-code">{model}</span></div>
          ) : null}
          {baseUrl ? (
            <div><small>URL</small><span className="agent-manage-detail-code" title={baseUrl}>{baseUrl}</span></div>
          ) : null}
          {target.text && target.text !== t("未配置") ? (
            <div><small>{t("检测状态")}</small><span>{target.text}</span></div>
          ) : null}
          {target.note ? (
            <div className="agent-manage-detail-note"><small>{t("备注")}</small><span>{target.note}</span></div>
          ) : null}
          {catalog?.packageManager === "npm" && catalog.packageName ? (
            <div><small>npm</small><span className="agent-manage-detail-code">{catalog.packageName}</span></div>
          ) : null}
        </div>
      </details>
    </div>
  );
}
