import { Boxes, FolderCog, Gauge, Languages, Layers3 } from "lucide-react";
import { NavLink } from "react-router-dom";

import { type TranslationKey, useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import { TaskCenter } from "./TaskCenter";

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
  const { state } = useWizard();
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

      <label className="language-picker">
        <Languages size={16} aria-hidden="true" />
        <span>{t("语言")}</span>
        <select className="language-select-wide" value={locale} onChange={(event) => setLocale(event.target.value as "zh-CN" | "en")} aria-label={t("语言")}>
          <option value="zh-CN">中文</option>
          <option value="en">English</option>
        </select>
        <select className="language-select-compact" value={locale} onChange={(event) => setLocale(event.target.value as "zh-CN" | "en")} aria-label={t("语言")}>
          <option value="zh-CN">中</option>
          <option value="en">EN</option>
        </select>
      </label>

      {/* Last child, so the language picker's margin-top: auto pushes both to
          the bottom of the sidebar as one group. */}
      <TaskCenter logDir={state.status?.paths.logs || ""} />
    </aside>
  );
}
