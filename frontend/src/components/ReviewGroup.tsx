import { CheckCircle2 } from "lucide-react";
import type { PropsWithChildren } from "react";

export function ReviewGroup({ title, children }: PropsWithChildren<{ title: string }>) {
  return (
    <section className="review-group">
      <h2>{title}</h2>
      <div className="review-list">{children}</div>
    </section>
  );
}

export function ReviewRow({ label, value, muted = false }: { label: string; value: string; muted?: boolean }) {
  return (
    <div className={`review-row${muted ? " is-muted" : ""}`}>
      <CheckCircle2 size={17} aria-hidden="true" />
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
