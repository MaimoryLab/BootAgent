import { Archive, BookOpen, ChevronRight, ExternalLink, Import, Languages, Power, RefreshCw, Terminal as TerminalIcon } from "lucide-react";
import { useState } from "react";
import { useEffect } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure, failureLine } from "../backend/api";
import { OTA_PROGRESS_TARGET } from "../backend/wails";
import { MirrorSetting } from "../components/MirrorSetting";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { ThemePicker } from "../components/ThemePicker";
import { GitHubMark } from "../components/icons/GitHubMark";
import { useI18n } from "../i18n";
import { taskCanceller, taskKey, updateTaskRoute, useTaskCenter } from "../state/TaskCenterContext";
import type { Settings } from "../types/api";

const defaultBackupRetention = 3;

function boundedBackupRetention(value: number): number {
  return Math.min(100, Math.max(1, Number.isFinite(value) ? Math.trunc(value) : defaultBackupRetention));
}

export function SettingsPage() {
  const navigate = useNavigate();
  const { locale, setLocale, t } = useI18n();
  const taskCenter = useTaskCenter();
  const [version, setVersion] = useState("v0.0.0-dev");
  const [checking, setChecking] = useState(false);
  const [updateMessage, setUpdateMessage] = useState("");
  const [latestVersion, setLatestVersion] = useState("");
  const [helpFailure, setHelpFailure] = useState("");
  const [settings, setSettings] = useState<Settings | null>(null);
  const [backupRetention, setBackupRetention] = useState<number | "">("");
  const [backupFailure, setBackupFailure] = useState("");
  const [autostartFailure, setAutostartFailure] = useState("");
  const [terminalFailure, setTerminalFailure] = useState("");

  useEffect(() => {
    // The desktop bridge normally returns a string, but a browser preview or an
    // older binding can resolve with a structured error object. Never put an
    // arbitrary bridge value into React children: one malformed value would
    // unmount the whole settings page and make unrelated controls unusable.
    void api.version().then((value) => {
      if (typeof value === "string" && value.trim()) setVersion(value);
    }).catch(() => {});
  }, []);
  useEffect(() => {
    let active = true;
    void api.getSettings().then((loaded) => {
      if (!active) return;
      setSettings(loaded);
      setBackupRetention(loaded.backup_retention ?? defaultBackupRetention);
    }).catch(() => {
      if (!active) return;
      setSettings(null);
      setBackupRetention(defaultBackupRetention);
      setBackupFailure(t("无法读取备份设置"));
    });
    return () => { active = false; };
  }, []);

  const saveTerminalApp = async (next: string) => {
    if (!settings) return;
    setTerminalFailure("");
    // Optimistic: the picker is the only writer, so showing the choice now keeps
    // the control responsive, and a failure below restores the stored value.
    const previous = settings.terminal_app;
    setSettings({ ...settings, terminal_app: next });
    try {
      const saved = await api.saveSettings({ terminal_app: next });
      setSettings(saved);
    } catch (error) {
      setSettings({ ...settings, terminal_app: previous });
      setTerminalFailure(describeFailure(error, t("无法保存终端设置"), t).message);
    }
  };

  const saveBackupRetention = async () => {
    if (!settings) return;
    const nextRetention = boundedBackupRetention(backupRetention === "" ? defaultBackupRetention : backupRetention);
    setBackupRetention(nextRetention);
    setBackupFailure("");
    try {
      const saved = await api.saveSettings({ backup_retention: nextRetention });
      setSettings(saved);
      setBackupRetention(saved.backup_retention ?? nextRetention);
    } catch (error) {
      setBackupFailure(describeFailure(error, t("无法保存备份设置"), t).message);
    }
  };

  const toggleAutostart = async (enabled: boolean) => {
    if (!settings) return;
    const previous = settings.autostart;
    setSettings({ ...settings, autostart: enabled });
    setAutostartFailure("");
    try {
      const saved = await api.saveSettings({ autostart: enabled });
      setSettings(saved);
    } catch (error) {
      setSettings({ ...settings, autostart: previous });
      setAutostartFailure(describeFailure(error, t("无法保存开机自启动设置"), t).message);
    }
  };

  const checkUpdate = async () => {
    setChecking(true);
    setUpdateMessage("");
    try {
      const latest = await api.checkUpdate();
      setLatestVersion(latest);
      setUpdateMessage(latest ? t("发现新版本 {version}", { version: latest }) : t("当前已是最新版本"));
    } catch (error) {
      // failureLine, not .message: for UPDATE_LOCATION_BLOCKED the hint carries
      // the instruction, and this single line is the whole report.
      setUpdateMessage(failureLine(error, t("检查更新失败"), t));
    } finally {
      setChecking(false);
    }
  };

  const installUpdate = async () => {
    if (!latestVersion) return;
    const taskID = taskKey("update", OTA_PROGRESS_TARGET);
    if (!taskCenter.startTask({ id: taskID, kind: "update", target: OTA_PROGRESS_TARGET, progressTarget: OTA_PROGRESS_TARGET, title: t("更新 BootAgent {version}", { version: latestVersion }), route: updateTaskRoute(OTA_PROGRESS_TARGET) })) return;
    try {
      const request = api.downloadUpdate();
      taskCenter.setTaskCanceller(taskID, taskCanceller(request));
      await request;
      taskCenter.finishTask(taskID, { kind: "success", message: t("更新已下载") });
      taskCenter.setTaskAction(taskID, { label: t("重启并更新"), run: () => api.restartUpdate() });
      setUpdateMessage(t("更新已下载"));
    } catch (error) {
      taskCenter.finishTask(taskID, { kind: "failure", message: failureLine(error, t("更新失败"), t) });
    }
  };

  const openHelp = async () => {
    setHelpFailure("");
    try {
      await api.openHelp();
    } catch (error) {
      setHelpFailure(describeFailure(error, t("无法打开帮助文档"), t).message);
    }
  };

  const openGitHub = async () => {
    setHelpFailure("");
    try {
      await api.openGitHub();
    } catch (error) {
      setHelpFailure(describeFailure(error, t("无法打开 GitHub"), t).message);
    }
  };
  return (
    <PageScaffold
      title={t("设置")}
      description={t("管理界面偏好与配置迁移")}
      bodyClassName="settings-page"
      footerNote={(
        <div className="settings-footer-content" aria-label={t("关于")}>
          <div className="settings-about-meta"><span>{t("版本")} {version}</span><span>{"(c) 2026 MaimoryLab"}</span></div>
          {updateMessage ? <small className="settings-update-message" role="status">{updateMessage}</small> : null}
        </div>
      )}
      secondaryAction={(
        <>
          <button className="button button-secondary" type="button" onClick={() => void checkUpdate()} disabled={checking}>
            <RefreshCw size={15} aria-hidden="true" className={checking ? "spin" : undefined} />
            {checking ? t("正在检查") : t("检查更新")}
          </button>
          {latestVersion ? <button className="button button-primary" type="button" onClick={() => void installUpdate()}>{t("立即更新")}</button> : null}
        </>
      )}
    >
      <section className="settings-section">
        <h2>{t("界面")}</h2>
        <div className="settings-row"><ThemePicker /></div>
        <div className="settings-row language-picker">
          <Languages size={16} aria-hidden="true" />
          <span>{t("语言")}</span>
          <SelectField
            className="language-select"
            label={t("语言")}
            value={locale}
            onChange={(next) => setLocale(next as "zh-CN" | "en")}
            options={[{ value: "zh-CN", label: "中文" }, { value: "en", label: "English" }]}
          />
        </div>
      </section>
      <section className="settings-section">
        <h2>{t("下载")}</h2>
        <MirrorSetting />
      </section>
      <section className="settings-section">
        <h2>{t("启动设置")}</h2>
        <div className="settings-row settings-toggle-row">
          <label className="toggle-row">
            <Power size={16} aria-hidden="true" />
            <span>
              <strong>{t("开机自启动")}</strong>
              <small>{t("登录系统后自动启动 BootAgent")}</small>
            </span>
            <input
              type="checkbox"
              role="switch"
              aria-label={t("开机自启动")}
              checked={settings?.autostart === true}
              disabled={settings === null}
              onChange={(event) => void toggleAutostart(event.target.checked)}
            />
          </label>
        </div>
        {autostartFailure ? <p className="settings-field-error" role="status">{autostartFailure}</p> : null}
        <div className="settings-row terminal-app-row">
          <TerminalIcon size={16} aria-hidden="true" />
          <label htmlFor="terminal-app">
            <strong>{t("启动 CLI Agent 的终端")}</strong>
            <small>{t("自动会使用系统默认终端；未安装的终端不可选")}</small>
          </label>
          <SelectField
            id="terminal-app"
            className="terminal-app-select"
            label={t("启动 CLI Agent 的终端")}
            value={settings?.terminal_app ?? ""}
            onChange={(next) => void saveTerminalApp(next)}
            options={[
              { value: "", label: t("自动") },
              ...(settings?.terminals ?? []).map((terminal) => ({
                value: terminal.id,
                // An uninstalled terminal stays in the list but cannot be picked:
                // dropping it would leave a stored choice unexplained.
                label: terminal.installed ? terminal.name : t("{name}（未安装）", { name: terminal.name }),
                disabled: !terminal.installed,
              })),
            ]}
          />
        </div>
        {terminalFailure ? <p className="settings-field-error" role="status">{terminalFailure}</p> : null}
      </section>
      <section className="settings-section">
        <h2>{t("数据")}</h2>
        <div className="settings-row backup-retention-row">
          <Archive size={16} aria-hidden="true" />
          <label htmlFor="backup-retention">
            <strong>{t("备份历史版本数")}</strong>
            <small>{t("每个目标分别保留历史版本，默认保留 3 个")}</small>
          </label>
          <input
            id="backup-retention"
            aria-label={t("备份历史版本数")}
            type="number"
            min={1}
            max={100}
            step={1}
            value={backupRetention}
            disabled={settings === null}
            onChange={(event) => setBackupRetention(event.target.value === "" ? "" : Number(event.target.value))}
            onBlur={() => void saveBackupRetention()}
          />
        </div>
        {backupFailure ? <p className="settings-field-error" role="status">{backupFailure}</p> : null}
        <button className="settings-link" type="button" onClick={() => navigate("/settings/transfer")}>
          <Import size={18} aria-hidden="true" />
          <span><strong>{t("导入导出")}</strong><small>{t("选择要迁移的模型服务和配置模版")}</small></span>
          <ChevronRight size={16} aria-hidden="true" />
        </button>
      </section>
      <section className="settings-section">
        <h2>{t("帮助")}</h2>
        {/* Opens the real browser through the backend rather than an <a
            target="_blank">: the webview has no tab to open one in, so a link
            would navigate the app away from itself. */}
        <button className="settings-link" type="button" onClick={() => void openHelp()}>
          <BookOpen size={18} aria-hidden="true" />
          <span><strong>{t("帮助文档")}</strong><small>{t("安装、切换模型、备份回退与常见问题")}</small></span>
          <ExternalLink size={15} aria-hidden="true" />
        </button>
        <button className="settings-link" type="button" onClick={() => void openGitHub()}>
          <GitHubMark size={18} />
          <span><strong>{t("Star 支持 BootAgent")}</strong><small>{t("如果 BootAgent 对你有帮助，欢迎在 GitHub 点个 Star")}</small></span>
          <ExternalLink size={15} aria-hidden="true" />
        </button>
        {helpFailure ? <p className="agent-manage-error">{helpFailure}</p> : null}
      </section>
    </PageScaffold>
  );
}
