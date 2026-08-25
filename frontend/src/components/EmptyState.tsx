import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  hint?: string;
  action?: ReactNode;
}

/** Centered empty placeholder with a glyph well, shared by list pages. */
export function EmptyState({ icon: Icon, title, hint, action }: EmptyStateProps) {
  return (
    <div className="empty-overview list-empty-state">
      <span className="list-empty-glyph" aria-hidden="true">
        <Icon size={22} strokeWidth={1.8} />
      </span>
      <strong>{title}</strong>
      {hint ? <span>{hint}</span> : null}
      {action ? <div className="list-empty-action">{action}</div> : null}
    </div>
  );
}
