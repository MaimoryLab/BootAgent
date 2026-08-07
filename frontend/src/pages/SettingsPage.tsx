import { ChevronRight, Import, Languages } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { ThemePicker } from "../components/ThemePicker";
import { useI18n } from "../i18n";

export function SettingsPage() {
  const navigate = useNavigate();
  const { locale, setLocale, t } = useI18n();
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
    </PageScaffold>
  );
}
