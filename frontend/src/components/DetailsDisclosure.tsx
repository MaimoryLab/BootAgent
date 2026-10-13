import type { ReactNode } from "react";

import { useI18n } from "../i18n";

export interface DetailRow {
  label: string;
  value: ReactNode;
  /** Paths, IDs and URLs: monospace, and the full value on hover. */
  code?: boolean;
  /** Spans the whole width, for values that are a sentence rather than a token. */
  wide?: boolean;
}

/**
 * The "详情" fold on a card or row. Technical facts -- file paths, program
 * locations, endpoints, package names -- live here rather than in the row
 * itself: a newcomer does not need them to use the Agent, and a developer gets
 * them back in one click (docs/ui-friendliness-plan.md, principle 2). Empty
 * values are dropped, so callers can pass every fact they know; with nothing
 * left to show, there is no fold.
 */
export function DetailsDisclosure({ rows, className = "", children }: { rows: DetailRow[]; className?: string; children?: ReactNode }) {
  const { t } = useI18n();
  const shown = rows.filter((row) => row.value !== "" && row.value !== null && row.value !== undefined && row.value !== false);
  if (!shown.length && !children) return null;
  return (
    <details className={`agent-manage-details${className ? ` ${className}` : ""}`}>
      <summary>{t("详情")}</summary>
      <div className="agent-manage-details-body">
        {shown.map((row) => (
          <div key={row.label} className={row.wide ? "agent-manage-detail-note" : undefined}>
            <small>{row.label}</small>
            <span className={row.code ? "agent-manage-detail-code" : undefined} title={row.code && typeof row.value === "string" ? row.value : undefined}>{row.value}</span>
          </div>
        ))}
        {children}
      </div>
    </details>
  );
}
