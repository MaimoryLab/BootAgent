import { Dialogs } from "@wailsio/runtime";
import { FolderOpen, History, Play, RefreshCw, SlidersHorizontal, Trash2 } from "lucide-react";
import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import { useConversationMigration } from "../hooks/useConversationMigration";
import { sourceTranslate, type Translate, useI18n } from "../i18n";
import { providerDisplayName } from "../state/conversion";
import { useFeedback } from "../state/FeedbackContext";
import { taskCanceller, taskKey, updateTaskRoute, useTaskCenter, useTaskRoute } from "../state/TaskCenterContext";
import type { AgentCatalogItem, AgentStatus, ProfileSummary, StatusResponse } from "../types/api";
import { ActionButton } from "./ActionButton";
import { AgentActionMenu, type AgentActionMenuItem } from "./AgentActionMenu";
import { DetailsDisclosure } from "./DetailsDisclosure";
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

function versionNote(status: AgentStatus): { text: string; behind: boolean } | null {
  if (!status.installed || !status.version) return null;
  // Only an older local version is an update; the arrow says which way it goes.
  // A newer one is normal -- the user upgraded the Agent themselves -- and is
  // shown as the plain version: the locked one, a term a newcomer cannot place,
  // is in the details as the recommended version.
  if (status.lockedVersion && compareVersions(status.version, status.lockedVersion) < 0) {
    return { text: `${status.version} → ${status.lockedVersion}`, behind: true };
  }
  // Being current needs no words either: five rows saying "已是最新" is noise.
  return { text: status.version, behind: false };
}

/**
 * What to say this Agent currently points at.
 *
 * Two sources: BootAgent's own record of what it wrote, and the Agent's config
 * file as found on disk. Reporting only the first is what made a hand-written
 * configuration render as "未配置" — the file was seen but never read. Where they
 * disagree the file wins, because that is what the Agent will actually use.
 *
 * `alert` is the one line the row itself shows when something needs the
 * user's attention, in plain words; `text` and `note` carry the specifics
 * (endpoint, parse error) for the details.
 */
export function targetSummary(
  status: AgentStatus,
  providers: Providers,
  t: Translate = sourceTranslate,
): { text: string; note: string; alert: string } {
  const detected = status.detected;
  if (detected?.unreadable) {
    return { text: t("配置无法解析"), note: detected.unreadable, alert: t("这个 Agent 的设置无法读取") };
  }
  const providerName = status.provider ? providerDisplayName(providers, status.provider, t) : "";
  // Ours, and the file agrees (or has nothing to add).
  if (status.provider && status.model) {
    const drifted =
      detected && detected.baseUrl && status.baseUrl && detected.baseUrl !== status.baseUrl;
    return {
      text: `${providerName} · ${status.model}`,
      note: drifted ? t("实际使用的接口地址是 {url}", { url: detected!.baseUrl }) : "",
      alert: drifted ? t("设置已在 BootAgent 之外被修改") : "",
    };
  }
  // No record of our own, but the file says something.
  if (detected && (detected.baseUrl || detected.model)) {
    const parts = [detected.baseUrl, detected.model].filter(Boolean);
    const foreign = !detected.managedByBootAgent;
    return {
      text: parts.join(" · "),
      note: foreign ? t("这份设置不是 BootAgent 写入的") : "",
      alert: foreign ? t("正在使用 BootAgent 之外的设置") : "",
    };
  }
  return { text: t("未配置"), note: "", alert: "" };
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
  const navigate = useNavigate();
  const feedback = useFeedback();
  const { startTask, finishTask, setTaskCanceller, taskFor, isTaskRunning } = useTaskCenter();
  const route = useTaskRoute();
  const [launching, setLaunching] = useState(false);
  const [localUpdating, setLocalUpdating] = useState(false);
  const [localUninstalling, setLocalUninstalling] = useState(false);
  // The last launch failure, kept so the row can still name it after its
  // receipt is closed. Cleared by the next launch that succeeds.
  const [launchFailure, setLaunchFailure] = useState<unknown>(null);
  // Settles the launch button's pending action once the directory dialog
  // answers: false when it is dismissed, the launch outcome when it is submitted.
  const launchSettle = useRef<{ resolve: (launched: boolean) => void; reject: (error: unknown) => void } | null>(null);
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
  const version = versionNote(status);
  const recommended = status.lockedVersion && status.version && status.lockedVersion !== status.version && !version?.behind ? status.lockedVersion : "";
  const target = targetSummary(status, providers, t);
  const providerId = profile?.provider || status.provider || "";
  const providerName = providerId ? providerDisplayName(providers, providerId, t) : "";
  const profileLabel = profileName || status.profileId || "";
  const model = profile?.model || status.model || status.detected?.model || "";
  const baseUrl = status.baseUrl || status.detected?.baseUrl || "";
  const agentName = catalog?.name || agentId;
  const launchKey = `launch:${agentId}`;
  // No "not configured" state here: the Profile and Provider tokens already name
  // whichever piece is absent, so a third word for the same condition only adds
  // a term the user has to map back onto them.
  //
  // A failure is a fixed-size marker in the token strip, not a line of text
  // under the name: the reason is in the receipt, which the marker reopens.
  const failureMarker = launchFailure
    ? { label: t("启动失败"), reopen: () => feedback.failure(launchFailure, t("无法启动 {name}", { name: agentName }), { key: launchKey }) }
    : updateTask?.state === "failure"
      ? { label: t("更新失败"), reopen: () => feedback.show({ key: updateTaskID, tone: "failure", message: t("无法更新 {name}", { name: agentName }), detail: updateTask.message || undefined }) }
      : uninstallTask?.state === "failure"
        ? { label: t("卸载失败"), reopen: () => feedback.show({ key: uninstallTaskID, tone: "failure", message: t("无法卸载 {name}", { name: agentName }), detail: uninstallTask.message || undefined }) }
        : null;

  // installed is true only when the Agent's command resolved on the managed
  // PATH, so it is already the precise "there is something to launch" signal.
  const canLaunch = status.installed;
  const offer = updateOffer(catalog, status);
  const manageable = Boolean(catalog?.packageManager && ["npm", "uv", "official-script"].includes(catalog.packageManager) && status.installed);
  const busy = launching || updating || uninstalling || migration.running || isTaskRunning(taskKey("install", agentId));

  /**
   * Says which terminal opened when it was not the one chosen in settings. The
   * backend falls back rather than failing when that terminal is gone, and
   * returns the one it used precisely so the substitution is not silent.
   */
  const reportTerminal = async (terminal: string) => {
    if (!terminal) return;
    try {
      const settings = await api.getSettings();
      const preferred = settings.terminal_app;
      if (!preferred || preferred === terminal) return;
      const nameOf = (id: string) => settings.terminals?.find((option) => option.id === id)?.name || id;
      feedback.show({
        key: "launch-terminal",
        tone: "warning",
        message: t("已改用 {terminal} 打开", { terminal: nameOf(terminal) }),
        detail: t("设置里选择的 {terminal} 未安装，可以在设置中更换", { terminal: nameOf(preferred) }),
        action: { label: t("去设置"), run: () => navigate("/settings") },
      });
    } catch {
      // The launch itself succeeded; not naming the terminal is no reason to say otherwise.
    }
  };
  const startLaunch = async (directory: string) => {
    setLaunching(true);
    try {
      const result = await api.launchAgent(agentId, directory);
      setLaunchFailure(null);
      void reportTerminal(result?.terminal ?? "");
    } catch (error) {
      setLaunchFailure(error);
      throw error;
    } finally {
      setLaunching(false);
    }
  };
  const settleLaunch = (outcome: { launched: boolean } | { error: unknown }) => {
    const pending = launchSettle.current;
    launchSettle.current = null;
    if (!pending) return;
    if ("error" in outcome) pending.reject(outcome.error);
    else pending.resolve(outcome.launched);
  };
  const closeDirectoryDialog = () => {
    setDirectoryDialog(false);
    settleLaunch({ launched: false });
  };
  // A web-app Agent serves a local page and chooses its own workspace, so asking
  // where to start it would collect an answer nothing then uses. It launches
  // straight away instead. Otherwise the launch button stays pending until the
  // directory dialog answers, so it confirms the launch, not the dialog.
  const launch = async (): Promise<boolean> => {
    if (catalog?.webApp) {
      await startLaunch("");
      return true;
    }
    const stored = localStorage.getItem(`bootagent:launch-directory:${agentId}`);
    setLaunchDirectory(stored || defaultDirectory || "");
    setRememberDirectory(Boolean(stored));
    setDirectoryDialog(true);
    return new Promise<boolean>((resolve, reject) => { launchSettle.current = { resolve, reject }; });
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
    setDirectoryDialog(false);
    try {
      await startLaunch(launchDirectory.trim());
      settleLaunch({ launched: true });
    } catch (error) {
      settleLaunch({ error });
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
			Message: t("卸载「{name}」？只移除程序，你的设置和对话都会保留。", { name: catalog?.name || agentId }),
			Buttons: [{ Label: confirmLabel }, { Label: t("卸载并删除全部数据") }, { Label: t("取消"), IsCancel: true }],
		}).catch(() => "");
		if (![confirmLabel, t("卸载并删除全部数据")].includes(choice)) return;
		const removeUserData = choice === t("卸载并删除全部数据");
		if (removeUserData) {
			const destructiveChoice = await Dialogs.Question({
				Title: t("确认永久删除数据"),
				Message: t("会永久删除这个 Agent 的设置、登录信息和全部对话，无法找回。"),
				Buttons: [{ Label: t("永久删除并卸载") }, { Label: t("取消"), IsCancel: true }],
			}).catch(() => "");
			if (destructiveChoice !== t("永久删除并卸载")) return;
		}
		if (!startTask({
      id: uninstallTaskID,
      kind: "uninstall",
      target: agentId,
      title: t("卸载 {name}", { name: catalog?.name || agentId }),
      route,
      cancellable: false,
    })) return;
    setLocalUninstalling(true);
	try {
		const runUninstall = async (allowCrossEnvironment: boolean) => {
				const ids = selectedIDs ?? (installations.length === 1 ? [installations[0].id] : []);
				const request = ids.length ? api.uninstallAgent(agentId, allowCrossEnvironment, "", ids, removeUserData) : (allowCrossEnvironment || installationID || removeUserData ? api.uninstallAgent(agentId, allowCrossEnvironment, installationID, [], removeUserData) : api.uninstallAgent(agentId));
			setTaskCanceller(uninstallTaskID, taskCanceller(request));
			await request;
		};
		try {
			await runUninstall(false);
		} catch (error) {
			if (!(error instanceof BootAgentApiError) || error.code !== "AGENT_NPM_ENVIRONMENT_MISMATCH") throw error;
			const crossChoice = await Dialogs.Question({
				Title: t("确认跨环境卸载"),
				Message: t("这个 Agent 是用另一套 Node.js 安装的。要用当初安装它的那一套来卸载吗？不会删除你的设置和对话。"),
				Buttons: [{ Label: t("允许并继续") }, { Label: t("取消"), IsCancel: true }],
			}).catch(() => "");
			if (crossChoice !== t("允许并继续")) throw error;
			await runUninstall(true);
		}
		finishTask(uninstallTaskID, { kind: "success", message: removeUserData ? t("已卸载 {name}，全部数据已删除", { name: catalog?.name || agentId }) : t("已卸载 {name}，配置和对话数据已保留", { name: catalog?.name || agentId }) });
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
            {target.alert ? <small className="agent-manage-note is-warning">{target.alert}</small> : null}
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
          {failureMarker ? (
            <button className="agent-manage-state is-error" type="button" onClick={failureMarker.reopen} title={t("查看原因")}>
              {failureMarker.label}
            </button>
          ) : !status.installed ? <span className="agent-manage-state">{t("未安装")}</span> : null}
        </div>
      </div>
      {directoryDialog ? (
        <ModalDialog className="transfer-password-dialog" label={t("选择启动目录")} onDismiss={closeDirectoryDialog}>
          <form onSubmit={(event) => { event.preventDefault(); void confirmLaunch(); }}>
            <h2>{t("选择启动目录")}</h2>
            <label className="launch-directory-label" htmlFor={`launch-directory-${agentId}`}>{t("启动目录")}</label>
            <div className="launch-directory-input-row">
              <input id={`launch-directory-${agentId}`} value={launchDirectory} onChange={(event) => setLaunchDirectory(event.target.value)} autoFocus spellCheck={false} autoCorrect="off" autoCapitalize="none" />
              <button className="icon-button" type="button" onClick={() => void chooseDirectory()} title={t("选择目录")} aria-label={t("选择目录")}><FolderOpen size={18} /></button>
            </div>
            <label className="launch-remember-row"><input type="checkbox" checked={rememberDirectory} onChange={(event) => setRememberDirectory(event.target.checked)} /><span>{t("记住此 Agent 的目录")}</span></label>
            <footer><button className="button button-secondary" type="button" onClick={closeDirectoryDialog}>{t("取消")}</button><button className="button button-primary" type="submit">{t("启动")}</button></footer>
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
          <ActionButton
            icon={Play}
            label={t("启动")}
            doneLabel={t("已打开")}
            failureMessage={t("无法启动 {name}", { name: agentName })}
            feedbackKey={launchKey}
            onAction={launch}
            disabled={busy && !launching}
            title={t("在新的终端窗口里打开")}
          />
        ) : null}
      </div>
      {/* Facts a newcomer does not need to use the Agent. The Profile, Provider
          and model are already in the row above, so they are not repeated. */}
      <DetailsDisclosure
        rows={[
          { label: t("模型"), value: model, code: true },
          { label: t("接口地址"), value: baseUrl, code: true },
          { label: t("推荐版本"), value: recommended },
          { label: t("说明"), value: target.note, wide: true },
          { label: t("安装包"), value: catalog?.packageManager && catalog.packageName ? catalog.packageName : "", code: true },
        ]}
      >
        {installations.length ? (
          <div className="agent-installation-details">
            <small>{t("安装位置")}</small>
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
      </DetailsDisclosure>
    </div>
  );
}
