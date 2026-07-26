import type { PropsWithChildren } from "react";

export function StatusBadge({ tone, children }: PropsWithChildren<{ tone: "success" | "warning" | "danger" | "info" | "neutral" }>) {
  return <span className={`status-badge status-${tone}`}>{children}</span>;
}
