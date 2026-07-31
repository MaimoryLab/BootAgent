import { Boxes, FolderCog, Gauge, Layers3 } from "lucide-react";
import { NavLink } from "react-router-dom";

// Only real destinations belong here. /setup/* are wizard steps behind
// SetupGuard: listing them made the sidebar look broken, because clicking one
// without a selected Agent bounced straight back to the first step.
const navItems = [
  { to: "/overview", label: "激活环境", icon: Gauge },
  { to: "/providers", label: "Provider", icon: Layers3 },
  { to: "/profiles", label: "配置模板", icon: FolderCog },
];

export function NavigationSidebar() {
  return (
    <aside className="navigation-sidebar">
      <div className="brand-lockup">
        <span className="brand-mark" aria-hidden="true">
          <Boxes size={21} strokeWidth={1.8} />
        </span>
        <div>
          <strong>OneAgent</strong>
          <span>AI 开发环境</span>
        </div>
      </div>

      <nav className="sidebar-nav" aria-label="主导航">
        <div className="sidebar-section-label">工作区</div>
        {navItems.map(({ to, label, icon: Icon }) => (
          <NavLink key={to} to={to} className={({ isActive }) => `sidebar-link${isActive ? " is-active" : ""}`}>
            <Icon size={18} strokeWidth={1.8} />
            <span>{label}</span>
          </NavLink>
        ))}
      </nav>
    </aside>
  );
}
