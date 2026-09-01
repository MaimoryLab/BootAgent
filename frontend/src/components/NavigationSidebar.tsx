import { FolderCog, Gauge, Layers3, Network, Radio, Settings, ShoppingBag, Sparkles } from "lucide-react";
import { NavLink } from "react-router-dom";

import { type TranslationKey, useI18n } from "../i18n";
import { BrandMark } from "./icons/BrandMark";
import { TaskCenter } from "./TaskCenter";

// Only real destinations belong here. /setup/* are wizard steps behind
// SetupGuard: listing them made the sidebar look broken, because clicking one
// without a selected Agent bounced straight back to the first step.
const navItems: Array<{ to: string; label: TranslationKey; icon: typeof Gauge }> = [
  { to: "/overview", label: "环境总览", icon: Gauge },
  { to: "/providers", label: "模型服务", icon: Layers3 },
  { to: "/profiles", label: "配置模版", icon: FolderCog },
  { to: "/marketplace", label: "工具市场", icon: ShoppingBag },
  { to: "/mcp", label: "MCP 服务器", icon: Network },
  { to: "/skills", label: "Skills", icon: Sparkles },
  { to: "/conversion", label: "API 协议适配", icon: Radio },
];

export function NavigationSidebar() {
  const { t } = useI18n();
  return (
    <aside className="navigation-sidebar">
      <div className="brand-lockup">
        {/* The product's own mark. This was lucide's generic <Boxes> glyph --
            a stock icon from the icon library standing in for a brand that did
            not exist yet. */}
        <span className="brand-mark" aria-hidden="true">
          <BrandMark size={22} />
        </span>
        <div>
          <strong>BootAgent</strong>
          <span>{t("Agent 管家")}</span>
        </div>
      </div>

      <nav className="sidebar-nav" aria-label={t("主导航")}>
        <div className="sidebar-section-label">{t("工作区")}</div>
        {navItems.map(({ to, label, icon: Icon }) => (
          <NavLink key={to} to={to} className={({ isActive }) => `sidebar-link${isActive ? " is-active" : ""}`}>
            <Icon size={18} strokeWidth={1.8} />
            <span>{t(label)}</span>
          </NavLink>
        ))}
      </nav>

      <div className="sidebar-bottom">
        <TaskCenter />
        <NavLink to="/settings" className={({ isActive }) => `sidebar-link${isActive ? " is-active" : ""}`}>
          <Settings size={18} strokeWidth={1.8} />
          <span>{t("设置")}</span>
        </NavLink>
      </div>
    </aside>
  );
}
