import { CheckCircle2, CircleAlert, TriangleAlert, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useI18n } from "../i18n";
import { FEEDBACK_SUCCESS_MS, type FeedbackItem, feedbackPersists, useFeedback } from "../state/FeedbackContext";

const icons = { success: CheckCircle2, failure: CircleAlert, warning: TriangleAlert };

function FeedbackToast({ item, onDismiss }: { item: FeedbackItem; onDismiss: () => void }) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const [paused, setPaused] = useState(false);
  // Time left for a success, kept across pauses so hovering twice does not
  // restart the full three seconds.
  const remaining = useRef(FEEDBACK_SUCCESS_MS);
  const Icon = icons[item.tone];
  const persists = feedbackPersists(item.tone);

  useEffect(() => {
    if (persists || paused || expanded) return;
    const startedAt = Date.now();
    const timer = setTimeout(onDismiss, remaining.current);
    return () => {
      clearTimeout(timer);
      remaining.current = Math.max(0, remaining.current - (Date.now() - startedAt));
    };
  }, [expanded, onDismiss, paused, persists]);

  return (
    <div
      className={`feedback-toast is-${item.tone}`}
      // A failure interrupts; a success waits its turn in the polite region
      // around it. No role on success: a second role="status" on the page would
      // compete with the ones forms already use.
      role={persists ? "alert" : undefined}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setPaused(false); }}
      onKeyDown={(event) => {
        if (event.key !== "Escape") return;
        event.stopPropagation();
        onDismiss();
      }}
    >
      <div className="feedback-toast-line">
        <Icon size={16} aria-hidden="true" />
        <span className="feedback-toast-message">{item.message}</span>
        {item.detail ? (
          <button className="feedback-toast-link" type="button" aria-expanded={expanded} onClick={() => setExpanded((open) => !open)}>
            {t("详情")}
          </button>
        ) : null}
        {item.action ? (
          // The action reports its own outcome, under the same key when it is a
          // retry, so this message goes first rather than lingering beside it.
          <button className="feedback-toast-link" type="button" onClick={() => { onDismiss(); void item.action?.run(); }}>
            {item.action.label}
          </button>
        ) : null}
        <button className="feedback-toast-close" type="button" onClick={onDismiss} aria-label={t("关闭")} title={t("关闭")}>
          <X size={14} aria-hidden="true" />
        </button>
      </div>
      {expanded && item.detail ? <p className="feedback-toast-detail">{item.detail}</p> : null}
    </div>
  );
}

/**
 * The receipt overlay, top right of the page body. PageScaffold renders it in
 * the body's own grid cell, so it covers a corner of the content instead of
 * taking a row of the layout: nothing on the page moves when it appears.
 */
export function FeedbackViewport() {
  const { t } = useI18n();
  const { available, items, dismiss } = useFeedback();
  if (!available) return null;
  const current = items[0];
  return (
    // Always mounted, even empty: a live region inserted together with its
    // first message is not reliably announced.
    <div className="feedback-viewport" aria-live="polite" aria-label={t("通知")}>
      {current ? <FeedbackToast key={current.id} item={current} onDismiss={() => dismiss(current.id)} /> : null}
    </div>
  );
}
