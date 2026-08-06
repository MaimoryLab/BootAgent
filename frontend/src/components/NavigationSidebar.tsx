import { Boxes, FolderCog, Gauge, Languages, Layers3 } from "lucide-react";
import { NavLink } from "react-router-dom";

import { type TranslationKey, useI18n } from "../i18n";
import { SelectField } from "./SelectField";
import { TaskCenter } from "./TaskCenter";
import { ThemePicker } from "./ThemePicker";

// Only real destinations belong here. /setup/* are wizard steps behind
// SetupGuard: listing them made the sidebar look broken, because clicking one
// without a selected Agent bounced straight back to the first step.
const navItems: Array<{ to: string; label: TranslationKey | "Provider"; icon: typeof Gauge }> = [
  { to: "/overview", label: "环境总览", icon: Gauge },
  { to: "/providers", label: "Provider", icon: Layers3 },
  { to: "/profiles", label: "配置模板", icon: FolderCog },
];

export function NavigationSidebar() {
  const { locale, setLocale, t } = useI18n();
  return (
    <aside className="navigation-sidebar">
      <div className="brand-lockup">
        <span className="brand-mark" aria-hidden="true">
          <Boxes size={21} strokeWidth={1.8} />
        </span>
        <div>
          <strong>OneAgent</strong>
          <span>{t("Agent 管家")}</span>
        </div>
      </div>

      <nav className="sidebar-nav" aria-label={t("主导航")}>
        <div className="sidebar-section-label">{t("工作区")}</div>
        {navItems.map(({ to, label, icon: Icon }) => (
          <NavLink key={to} to={to} className={({ isActive }) => `sidebar-link${isActive ? " is-active" : ""}`}>
            <Icon size={18} strokeWidth={1.8} />
            <span>{label === "Provider" ? label : t(label)}</span>
          </NavLink>
        ))}
      </nav>

      {/* First of the bottom group, so its margin-top: auto pushes appearance
          and language down together. The task centre is viewport-docked. */}
      <ThemePicker />

      {/* One picker, where there used to be two selects differing only in their
          option text -- CSS showed one and hid the other per breakpoint. The
          short labels now live in the option list, which stays readable at the
          72px rail because the list is ours and is not clipped to the trigger. */}
      <div className="language-picker">
        <Languages size={16} aria-hidden="true" />
        <span>{t("语言")}</span>
        <SelectField
          className="language-select"
          label={t("语言")}
          value={locale}
          onChange={(next) => setLocale(next as "zh-CN" | "en")}
          options={[
            { value: "zh-CN", label: "中文" },
            { value: "en", label: "English" },
          ]}
        />
      </div>

      {/* The task centre is fixed to the viewport's lower-left corner. */}
      <TaskCenter />
    </aside>
  );
}
