import { ChevronRight, Import, Languages, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { ThemePicker } from "../components/ThemePicker";
import { useI18n } from "../i18n";

export function SettingsPage() {
  const navigate = useNavigate();
  const { locale, setLocale, t } = useI18n();
  const [version, setVersion] = useState("v0.0.0-dev");
  const [checking, setChecking] = useState(false);
  const [updateMessage, setUpdateMessage] = useState("");

  useEffect(() => { void api.version().then(setVersion).catch(() => {}); }, []);

  const checkUpdate = async () => {
    setChecking(true);
    setUpdateMessage("");
    try {
      const latest = await api.checkUpdate();
      setUpdateMessage(latest ? t("发现新版本 {version}", { version: latest }) : t("当前已是最新版本"));
    } catch (error) {
      setUpdateMessage(describeError(error, t("检查更新失败")).message);
    } finally {
      setChecking(false);
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
          <span><strong>{t("导入导出")}</strong><small>{t("选择要迁移的 Provider 和 Profile")}</small></span>
          <ChevronRight size={16} aria-hidden="true" />
        </button>
      </section>
      <section className="settings-about" aria-label={t("关于")}>
        <div className="settings-about-meta"><span>{t("版本")} {version}</span><span>{"(c) 2026 MaimoryLab"}</span></div>
        <button className="button button-secondary" type="button" onClick={() => void checkUpdate()} disabled={checking}>
          <RefreshCw size={15} aria-hidden="true" className={checking ? "spin" : undefined} />
          {checking ? t("正在检查") : t("检查更新")}
        </button>
        {updateMessage ? <small className="settings-update-message" role="status">{updateMessage}</small> : null}
      </section>
    </PageScaffold>
  );
}
