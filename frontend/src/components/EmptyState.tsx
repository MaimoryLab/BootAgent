import type { LucideIcon } from "lucide-react";

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  hint?: string;
}

/** Centered empty placeholder with a glyph well, shared by list pages. */
export function EmptyState({ icon: Icon, title, hint }: EmptyStateProps) {
  return (
    <div className="empty-overview list-empty-state">
      <span className="list-empty-glyph" aria-hidden="true">
        <Icon size={22} strokeWidth={1.8} />
      </span>
      <strong>{title}</strong>
      {hint ? <span>{hint}</span> : null}
    </div>
  );
}
