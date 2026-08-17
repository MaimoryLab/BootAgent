import { Archive, BookOpen, ChevronRight, ExternalLink, Import, Languages, RefreshCw, Radio } from "lucide-react";
import { useState } from "react";
import { useEffect } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure, failureLine } from "../backend/api";
import { OTA_PROGRESS_TARGET } from "../backend/wails";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { ThemePicker } from "../components/ThemePicker";
import { GitHubMark } from "../components/icons/GitHubMark";
import { useI18n } from "../i18n";
import { taskCanceller, taskKey, updateTaskRoute, useTaskCenter } from "../state/TaskCenterContext";
import type { ConversionConfig, Settings, StatusResponse } from "../types/api";

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
  const [conversion, setConversion] = useState<ConversionConfig | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [conversionFailure, setConversionFailure] = useState("");
  const [settingsTab, setSettingsTab] = useState<"general" | "conversion">("general");

  useEffect(() => { void api.version().then(setVersion).catch(() => {}); }, []);
  useEffect(() => { void Promise.all([api.getConversion(), api.status()]).then(([c, s]) => { setConversion(c); setStatus(s); }).catch(() => setConversionFailure(t("无法读取格式转换设置"))); }, [t]);
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

  const saveBackupRetention = async () => {
    if (!settings) return;
    const nextRetention = boundedBackupRetention(backupRetention === "" ? defaultBackupRetention : backupRetention);
    setBackupRetention(nextRetention);
    setBackupFailure("");
    try {
      const saved = await api.saveSettings({
        ...settings,
        schema_version: 1,
        mirror_from_region: false,
        backup_retention: nextRetention,
      });
      setSettings(saved);
      setBackupRetention(saved.backup_retention ?? nextRetention);
    } catch (error) {
      setBackupFailure(describeFailure(error, t("无法保存备份设置"), t).message);
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
      <div className="agent-tabs" role="tablist" aria-label={t("设置")}>
        <button className={`agent-tab${settingsTab === "general" ? " is-active" : ""}`} role="tab" aria-selected={settingsTab === "general"} type="button" onClick={() => setSettingsTab("general")}>{t("常规设置")}</button>
        <button className={`agent-tab${settingsTab === "conversion" ? " is-active" : ""}`} role="tab" aria-selected={settingsTab === "conversion"} type="button" onClick={() => setSettingsTab("conversion")}>{t("本地格式转换")}</button>
      </div>
      {settingsTab === "conversion" ? <section className="settings-section">
        <h2>{t("本地格式转换")}</h2>
        {conversion ? <>
          <div className="settings-row"><Radio size={16} aria-hidden="true" /><label htmlFor="conversion-enabled"><strong>{t("启用本地 API 格式转换")}</strong><small>{t("将 Anthropic Messages 和 OpenAI Responses 转为 OpenAI Chat Completions")}</small></label><input id="conversion-enabled" type="checkbox" checked={conversion.enabled} onChange={(e) => setConversion({ ...conversion, enabled: e.target.checked })} /></div>
          <div className="settings-row"><label htmlFor="conversion-target"><strong>{t("目标 Profile")}</strong></label><select id="conversion-target" value={conversion.target_profile} onChange={(e) => setConversion({ ...conversion, target_profile: e.target.value })}>{(status?.profiles ?? []).filter((p) => p.model).map((p) => <option key={p.id} value={p.id}>{p.label || p.id}</option>)}</select></div>
          <div className="settings-row"><label htmlFor="conversion-listen"><strong>{t("监听地址")}</strong></label><input id="conversion-listen" value={conversion.listen} onChange={(e) => setConversion({ ...conversion, listen: e.target.value })} /></div>
          <div className="settings-row"><label htmlFor="conversion-key"><strong>{t("本地 API Key")}</strong></label><input id="conversion-key" type="password" value={conversion.api_key} onChange={(e) => setConversion({ ...conversion, api_key: e.target.value })} /></div>
          <div className="settings-row"><label htmlFor="conversion-anthropic-model"><strong>{t("Anthropic 模型")}</strong></label><input id="conversion-anthropic-model" value={conversion.anthropic_model} onChange={(e) => setConversion({ ...conversion, anthropic_model: e.target.value })} /></div>
          <div className="settings-row"><label htmlFor="conversion-responses-model"><strong>{t("Responses 模型")}</strong></label><input id="conversion-responses-model" value={conversion.responses_model} onChange={(e) => setConversion({ ...conversion, responses_model: e.target.value })} /></div>
          <button className="button button-primary" type="button" onClick={() => void api.saveConversion(conversion).then(setConversion).catch((e) => setConversionFailure(String(e)))}>{t("保存格式转换设置")}</button>
          {conversionFailure ? <p className="settings-field-error" role="status">{conversionFailure}</p> : null}
        </> : <p className="settings-field-error">{conversionFailure}</p>}
      </section> : <>
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
      </>}
    </PageScaffold>
  );
}
