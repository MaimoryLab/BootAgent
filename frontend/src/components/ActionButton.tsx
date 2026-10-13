import { Check, type LucideIcon, RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useFeedback } from "../state/FeedbackContext";

/** How long the confirmed face stays before the button returns to its label. */
export const ACTION_DONE_MS = 1600;

type Phase = "idle" | "busy" | "done";

interface ActionButtonProps {
  icon: LucideIcon;
  label: string;
  /** Four characters at most: 已打开, 已保存, 已应用. */
  doneLabel: string;
  /** Headline for the receipt when the action throws, e.g. 无法启动 Codex. */
  failureMessage: string;
  /** Receipt key, so a retry replaces the failure it answers. */
  feedbackKey?: string;
  /** Resolving to `false` means the action did not go ahead (a declined
   *  confirmation): the button returns to idle without claiming success. */
  onAction: () => unknown | Promise<unknown>;
  className?: string;
  disabled?: boolean;
  title?: string;
}

/**
 * A button that reports its own result in place (docs/ui-interaction-plan.md
 * §3.2.1): idle, busy, then a short confirmation, all at one width. Both faces
 * share a single grid cell, so the button is as wide as the longer of the two
 * from the first render and never resizes the row it sits in. A failure goes
 * to the receipt overlay rather than turning the button red, which would read
 * as a destructive action.
 */
export function ActionButton({
  icon: Icon,
  label,
  doneLabel,
  failureMessage,
  feedbackKey,
  onAction,
  className = "button button-primary",
  disabled,
  title,
}: ActionButtonProps) {
  const feedback = useFeedback();
  const [phase, setPhase] = useState<Phase>("idle");
  const mounted = useRef(true);
  useEffect(() => () => { mounted.current = false; }, []);

  useEffect(() => {
    if (phase !== "done") return;
    const timer = setTimeout(() => setPhase("idle"), ACTION_DONE_MS);
    return () => clearTimeout(timer);
  }, [phase]);

  const run = async () => {
    setPhase("busy");
    try {
      const result = await onAction();
      if (mounted.current) setPhase(result === false ? "idle" : "done");
    } catch (error) {
      if (mounted.current) setPhase("idle");
      feedback.failure(error, failureMessage, { key: feedbackKey });
    }
  };

  const done = phase === "done";
  return (
    <>
      <button
        className={`${className} action-button`}
        type="button"
        onClick={() => void run()}
        disabled={disabled || phase === "busy"}
        aria-busy={phase === "busy"}
        title={title}
      >
        <span className="action-button-faces">
          <span className={`action-button-face${done ? " is-inactive" : ""}`} aria-hidden={done}>
            {phase === "busy" ? <RefreshCw size={15} className="spin" aria-hidden="true" /> : <Icon size={15} aria-hidden="true" />}
            {label}
          </span>
          <span className={`action-button-face${done ? "" : " is-inactive"}`} aria-hidden={!done}>
            <Check size={15} aria-hidden="true" />
            {doneLabel}
          </span>
        </span>
      </button>
      {/* The face swap changes the button's text, which screen readers do not
          announce on their own. */}
      <span className="visually-hidden" aria-live="polite">{done ? doneLabel : ""}</span>
    </>
  );
}
