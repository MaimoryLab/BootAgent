import { MoreHorizontal, type LucideIcon } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

const OPEN_EVENT = "bootagent:agent-action-menu-open";

export interface AgentActionMenuItem {
  id: string;
  label: string;
  icon?: LucideIcon;
  onSelect: () => void | Promise<void>;
  disabled?: boolean;
  tone?: "default" | "danger";
  separatorBefore?: boolean;
}

export function AgentActionMenu({ label, items, hasUpdate = false }: {
  label: string;
  items: AgentActionMenuItem[];
  hasUpdate?: boolean;
}) {
  const id = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const focusOnOpen = useRef<"first" | "last" | null>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const closeOther = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== id) setOpen(false);
    };
    document.addEventListener(OPEN_EVENT, closeOther);
    return () => document.removeEventListener(OPEN_EVENT, closeOther);
  }, [id]);

  useEffect(() => {
    if (!open) return;
    const closeOutside = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", closeOutside);
    return () => document.removeEventListener("mousedown", closeOutside);
  }, [open]);

  useEffect(() => {
    if (!open || !focusOnOpen.current) return;
    const enabled = enabledItems(menuRef.current);
    const target = focusOnOpen.current === "last" ? enabled.at(-1) : enabled[0];
    focusOnOpen.current = null;
    target?.focus();
  }, [open]);

  if (items.length === 0) return null;

  const setMenuOpen = (next: boolean, focus?: "first" | "last") => {
    if (next) {
      focusOnOpen.current = focus ?? null;
      document.dispatchEvent(new CustomEvent<string>(OPEN_EVENT, { detail: id }));
    }
    setOpen(next);
  };
  const closeAndFocusTrigger = () => {
    setOpen(false);
    triggerRef.current?.focus();
  };
  const moveFocus = (direction: 1 | -1) => {
    const enabled = enabledItems(menuRef.current);
    if (!enabled.length) return;
    const current = enabled.indexOf(document.activeElement as HTMLButtonElement);
    const next = current < 0 ? (direction > 0 ? 0 : enabled.length - 1) : (current + direction + enabled.length) % enabled.length;
    enabled[next]?.focus();
  };

  return (
    <div className="agent-action-menu" ref={rootRef}>
      <button
        ref={triggerRef}
        className="icon-button agent-action-menu-trigger"
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setMenuOpen(!open)}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            setMenuOpen(true, event.key === "ArrowUp" ? "last" : "first");
          }
        }}
      >
        <MoreHorizontal size={18} aria-hidden="true" />
        {hasUpdate ? <span className="agent-action-menu-dot" aria-hidden="true" /> : null}
      </button>
      {open ? (
        <div
          ref={menuRef}
          className="agent-action-menu-panel"
          role="menu"
          aria-label={label}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              closeAndFocusTrigger();
            } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
              event.preventDefault();
              moveFocus(event.key === "ArrowDown" ? 1 : -1);
            } else if (event.key === "Home" || event.key === "End") {
              event.preventDefault();
              const enabled = enabledItems(menuRef.current);
              (event.key === "Home" ? enabled[0] : enabled.at(-1))?.focus();
            }
          }}
        >
          {items.map((item) => {
            const Icon = item.icon;
            return (
              <button
                key={item.id}
                className={`agent-action-menu-item${item.tone === "danger" ? " is-danger" : ""}${item.separatorBefore ? " has-separator" : ""}`}
                type="button"
                role="menuitem"
                disabled={item.disabled}
                onClick={() => {
                  setOpen(false);
                  void item.onSelect();
                }}
              >
                {Icon ? <Icon size={15} aria-hidden="true" /> : null}
                <span>{item.label}</span>
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

function enabledItems(menu: HTMLDivElement | null): HTMLButtonElement[] {
  return menu ? Array.from(menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)')) : [];
}
