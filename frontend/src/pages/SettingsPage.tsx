import { BookOpen, ChevronRight, ExternalLink, Import, Languages, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { OTA_PROGRESS_TARGET } from "../backend/wails";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { ThemePicker } from "../components/ThemePicker";
import { useI18n } from "../i18n";
import { taskCanceller, taskKey, updateTaskRoute, useTaskCenter } from "../state/TaskCenterContext";

export function SettingsPage() {
  const navigate = useNavigate();
  const { locale, setLocale, t } = useI18n();
  const taskCenter = useTaskCenter();
  const [version, setVersion] = useState("v0.0.0-dev");
  const [checking, setChecking] = useState(false);
  const [updateMessage, setUpdateMessage] = useState("");
  const [latestVersion, setLatestVersion] = useState("");
  const [helpFailure, setHelpFailure] = useState("");

  useEffect(() => { void api.version().then(setVersion).catch(() => {}); }, []);

  const checkUpdate = async () => {
    setChecking(true);
    setUpdateMessage("");
    try {
      const latest = await api.checkUpdate();
      setLatestVersion(latest);
      setUpdateMessage(latest ? t("发现新版本 {version}", { version: latest }) : t("当前已是最新版本"));
    } catch (error) {
      setUpdateMessage(describeFailure(error, t("检查更新失败"), t).message);
    } finally {
      setChecking(false);
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

  const installUpdate = async () => {
    if (!latestVersion) return;
    const taskID = taskKey("update", OTA_PROGRESS_TARGET);
    if (!taskCenter.startTask({ id: taskID, kind: "update", target: OTA_PROGRESS_TARGET, progressTarget: OTA_PROGRESS_TARGET, title: t("更新 OneAgent {version}", { version: latestVersion }), route: updateTaskRoute(OTA_PROGRESS_TARGET) })) return;
    try {
      const request = api.downloadUpdate();
      taskCenter.setTaskCanceller(taskID, taskCanceller(request));
      await request;
      taskCenter.finishTask(taskID, { kind: "success", message: t("更新已下载") });
      taskCenter.setTaskAction(taskID, { label: t("重启并更新"), run: () => api.restartUpdate() });
      setUpdateMessage(t("更新已下载"));
    } catch (error) {
      taskCenter.finishTask(taskID, { kind: "failure", message: describeFailure(error, t("更新失败"), t).message });
    }
  };

  return (
    <PageScaffold title={t("设置")} description={t("管理界面偏好与配置迁移")} bodyClassName="settings-page">
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
        {helpFailure ? <p className="agent-manage-error">{helpFailure}</p> : null}
      </section>
      <section className="settings-about" aria-label={t("关于")}>
        <div className="settings-about-meta"><span>{t("版本")} {version}</span><span>{"(c) 2026 MaimoryLab"}</span></div>
        <button className="button button-secondary" type="button" onClick={() => void checkUpdate()} disabled={checking}>
          <RefreshCw size={15} aria-hidden="true" className={checking ? "spin" : undefined} />
          {checking ? t("正在检查") : t("检查更新")}
        </button>
        {updateMessage ? <div className="settings-update-message" role="status"><small>{updateMessage}</small>{latestVersion ? <button className="button button-primary button-compact" type="button" onClick={() => void installUpdate()}>{t("立即更新")}</button> : null}</div> : null}
      </section>
    </PageScaffold>
  );
}
