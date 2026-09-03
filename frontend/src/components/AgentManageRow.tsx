import { Dialogs } from "@wailsio/runtime";
import { FolderOpen, History, Play, RefreshCw, SlidersHorizontal, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import { useConversationMigration } from "../hooks/useConversationMigration";
import { sourceTranslate, type Translate, useI18n } from "../i18n";
import { taskCanceller, taskKey, updateTaskRoute, useTaskCenter, useTaskRoute } from "../state/TaskCenterContext";
import type { AgentCatalogItem, AgentStatus, ProfileSummary, StatusResponse } from "../types/api";
import { AgentActionMenu, type AgentActionMenuItem } from "./AgentActionMenu";
import { AgentIcon, agentTagline } from "./icons/agents";
import { ModalDialog } from "./ModalDialog";

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
  const [localUpdating, setLocalUpdating] = useState(false);
  const [localUninstalling, setLocalUninstalling] = useState(false);
  const [failure, setFailure] = useState("");
  const [launchDirectory, setLaunchDirectory] = useState("");
  const [rememberDirectory, setRememberDirectory] = useState(false);
  const [installationID, setInstallationID] = useState("");
  const installations = status.installations ?? [];
  const [uninstallPickerOpen, setUninstallPickerOpen] = useState(false);
  const [selectedInstallationIDs, setSelectedInstallationIDs] = useState<string[]>([]);
  const [directoryDialog, setDirectoryDialog] = useState(false);
  const updateTaskID = taskKey("update", agentId);
  const uninstallTaskID = taskKey("uninstall", agentId);
  const updateTask = taskFor(updateTaskID);
  const uninstallTask = taskFor(uninstallTaskID);
  const migration = useConversationMigration();
  const updating = updateTask?.state === "running" || localUpdating;
  const uninstalling = uninstallTask?.state === "running" || localUninstalling;
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
  const taskFailure = updateTask?.state === "failure"
    ? updateTask.message || t("失败")
    : uninstallTask?.state === "failure" ? uninstallTask.message || t("失败") : "";
  const statusLabel = failure || taskFailure ? t("失败") : !status.installed ? t("未安装") : "";

  // installed is true only when the Agent's command resolved on the managed
  // PATH, so it is already the precise "there is something to launch" signal.
  const canLaunch = status.installed;
  const offer = updateOffer(catalog, status);
  const manageable = Boolean(catalog?.packageManager && ["npm", "uv", "official-script"].includes(catalog.packageManager) && status.installed);
  const busy = launching || updating || uninstalling || migration.running || isTaskRunning(taskKey("install", agentId));

  const startLaunch = async (directory: string) => {
    setDirectoryDialog(false);
    setLaunching(true);
    setFailure("");
    try {
      await api.launchAgent(agentId, directory);
    } catch (error) {
      setFailure(describeFailure(error, t("无法启动 Agent"), t).message);
    } finally {
      setLaunching(false);
    }
  };
  // A web-app Agent serves a local page and chooses its own workspace, so asking
  // where to start it would collect an answer nothing then uses. It launches
  // straight away instead.
  const launch = () => {
    if (catalog?.webApp) {
      void startLaunch("");
      return;
    }
    const stored = localStorage.getItem(`bootagent:launch-directory:${agentId}`);
    setLaunchDirectory(stored || defaultDirectory || "");
    setRememberDirectory(Boolean(stored));
    setDirectoryDialog(true);
  };
  const chooseDirectory = async () => {
    try {
      const selected = await Dialogs.OpenFile({ Title: t("选择启动目录"), Directory: launchDirectory || defaultDirectory || undefined, CanChooseDirectories: true, CanChooseFiles: false, CanCreateDirectories: true }) as unknown as string | string[];
      const directory = Array.isArray(selected) ? selected[0] : selected;
      if (directory) setLaunchDirectory(directory);
    } catch { /* cancelled */ }
  };
  const confirmLaunch = async () => {
    if (!launchDirectory.trim()) return;
    if (rememberDirectory) localStorage.setItem(`bootagent:launch-directory:${agentId}`, launchDirectory.trim());
    else localStorage.removeItem(`bootagent:launch-directory:${agentId}`);
    await startLaunch(launchDirectory.trim());
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
  const uninstall = async (selectedIDs?: string[]) => {
    if (installations.length > 1 && !selectedIDs) {
      setSelectedInstallationIDs(installations.filter((item) => item.canUninstall).map((item) => item.id));
      setUninstallPickerOpen(true);
      return;
    }
    const confirmLabel = t("卸载 Agent");
    const choice = await Dialogs.Question({
      Title: confirmLabel,
      Message: t("确定卸载「{name}」吗？只会移除 Agent 程序，配置模版、模型服务、配置文件和对话数据都会保留。", { name: catalog?.name || agentId }),
      Buttons: [{ Label: confirmLabel }, { Label: t("取消"), IsCancel: true }],
    }).catch(() => "");
    if (choice !== confirmLabel || !startTask({
      id: uninstallTaskID,
      kind: "uninstall",
      target: agentId,
      title: t("卸载 {name}", { name: catalog?.name || agentId }),
      route,
      cancellable: false,
    })) return;
    setLocalUninstalling(true);
	setFailure("");
	try {
		const runUninstall = async (allowCrossEnvironment: boolean) => {
				const ids = selectedIDs ?? (installations.length === 1 ? [installations[0].id] : []);
				const request = ids.length ? api.uninstallAgent(agentId, allowCrossEnvironment, "", ids) : (allowCrossEnvironment || installationID ? api.uninstallAgent(agentId, allowCrossEnvironment, installationID) : api.uninstallAgent(agentId));
			setTaskCanceller(uninstallTaskID, taskCanceller(request));
			await request;
		};
		try {
			await runUninstall(false);
		} catch (error) {
			if (!(error instanceof BootAgentApiError) || error.code !== "AGENT_NPM_ENVIRONMENT_MISMATCH") throw error;
			const crossChoice = await Dialogs.Question({
				Title: t("确认跨环境卸载"),
				Message: t("当前 Agent 由另一套 Node/npm 环境管理。是否允许使用已记录的原始 npm 环境卸载？不会使用 sudo，也不会删除配置和对话数据。"),
				Buttons: [{ Label: t("允许并继续") }, { Label: t("取消"), IsCancel: true }],
			}).catch(() => "");
			if (crossChoice !== t("允许并继续")) throw error;
			await runUninstall(true);
		}
      finishTask(uninstallTaskID, { kind: "success", message: t("已卸载 {name}，配置和对话数据已保留", { name: catalog?.name || agentId }) });
      await onChanged?.();
    } catch (error) {
      finishTask(uninstallTaskID, { kind: "failure", message: describeFailure(error, t("无法卸载 Agent"), t).message });
    } finally {
      setLocalUninstalling(false);
    }
  };
  const menuItems: AgentActionMenuItem[] = status.installed ? [
    ...(agentId === "codex" ? [{
      id: "migration",
      label: migration.running ? t("迁移中") : t("迁移对话"),
      icon: History,
      onSelect: migration.run,
      disabled: busy,
    }] : []),
    ...(offer.npm ? [{
      id: "update",
      label: updating ? t("更新中") : offer.behind ? t("更新至 {version}", { version: offer.behind }) : t("更新"),
      icon: RefreshCw,
      onSelect: update,
      disabled: busy,
    }] : []),
    {
      id: "refresh",
      label: t("刷新状态"),
      icon: RefreshCw,
      onSelect: async () => { await onChanged?.(); },
      disabled: busy,
    },
    ...(manageable ? [{
      id: "uninstall",
      label: uninstalling ? t("卸载中") : t("卸载 Agent"),
      icon: Trash2,
      onSelect: uninstall,
      disabled: busy,
      tone: "danger" as const,
      separatorBefore: true,
    }] : []),
  ] : [];
  return (
    <div className="agent-manage-row" data-testid={`agent-${agentId}`}>
      <div className="agent-manage-summary">
        <div className="agent-manage-identity">
          <span className="agent-icon" title={agentTagline(agentId, t) || undefined}>
            <AgentIcon agentId={agentId} size={20} />
          </span>
          <span className="agent-manage-identity-copy">
            <strong>{catalog?.name || agentId}</strong>
            {failure || taskFailure ? <small className="agent-manage-note is-error">{failure || taskFailure}</small> : null}
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
          {statusLabel ? <span className={`agent-manage-state${failure || taskFailure ? " is-error" : ""}`}>{statusLabel}</span> : null}
        </div>
      </div>
      {directoryDialog ? (
        <ModalDialog className="transfer-password-dialog" label={t("选择启动目录")} onDismiss={() => setDirectoryDialog(false)}>
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
        </ModalDialog>
      ) : null}
      {uninstallPickerOpen ? (
        <ModalDialog className="agent-uninstall-dialog" label={t("选择卸载实例")} onDismiss={() => setUninstallPickerOpen(false)}>
          <h2>{t("选择卸载实例")}</h2>
          <div className="agent-uninstall-list">{installations.map((installation) => <label className="agent-uninstall-item" key={installation.id}><input type="checkbox" checked={selectedInstallationIDs.includes(installation.id)} disabled={!installation.canUninstall} onChange={(event) => setSelectedInstallationIDs((current) => event.target.checked ? [...current, installation.id] : current.filter((id) => id !== installation.id))} /><span className="agent-uninstall-item-copy"><strong>{installation.manager}</strong><code title={installation.executable}>{installation.executable}</code><small>{installation.canUninstall ? t("可卸载") : installation.reason || t("不可卸载")}</small></span></label>)}</div>
          <footer><button className="button button-secondary" type="button" onClick={() => setUninstallPickerOpen(false)}>{t("取消")}</button><button className="button button-primary" type="button" disabled={!selectedInstallationIDs.length} onClick={() => { setUninstallPickerOpen(false); void uninstall(selectedInstallationIDs); }}>{t("继续卸载")}</button></footer>
        </ModalDialog>
      ) : null}
      <div className="agent-manage-actions">
        {/* Always in the row, not only when the Agent cannot launch. Configuring
            an installed Agent was previously reachable only by opening <details>,
            which made the common case the hidden one. */}
        <AgentActionMenu
          label={t("{name} 更多操作", { name: catalog?.name || agentId })}
          items={menuItems}
          hasUpdate={Boolean(offer.behind && !updating)}
        />
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
            disabled={busy}
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
          {catalog?.packageManager && catalog.packageName ? (
            <div><small>npm</small><span className="agent-manage-detail-code">{catalog.packageName}</span></div>
          ) : null}
          {installations.length ? (
            <div className="agent-installation-details">
              <small>{t("检测到的安装实例")}</small>
              <div className="agent-installation-list">
                {installations.map((installation) => (
                  <div className="agent-installation-item" key={installation.id}>
                    <span>{installation.manager}</span>
                    <code title={installation.executable}>{installation.executable}</code>
                    <small>{installation.canUninstall ? t("可卸载") : installation.reason || t("不可卸载")}</small>
                  </div>
                ))}
              </div>
            </div>
          ) : null}
        </div>
      </details>
    </div>
  );
}
