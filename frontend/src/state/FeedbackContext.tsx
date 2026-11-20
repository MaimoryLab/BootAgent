import { createContext, type PropsWithChildren, useCallback, useContext, useMemo, useRef, useState } from "react";

import { describeFailure, isCancellationError } from "../backend/errors";
import { useI18n } from "../i18n";

/**
 * Operation feedback that never moves the page (docs/ui-interaction-plan.md §3).
 *
 * Results used to be written into the page body as notices, which pushed the
 * content -- and the button the user was about to press -- down when one
 * appeared and back up when it went. They also lived in the component that
 * raised them, so a result followed by navigation was unmounted with that
 * component before anyone read it. The state lives here instead, above the
 * routes, and FeedbackViewport overlays it on whichever page is showing.
 */

export type FeedbackTone = "success" | "failure" | "warning";

export interface FeedbackAction {
  label: string;
  run: () => void | Promise<void>;
}

export interface FeedbackInput {
  /** A new message with the same key replaces the old one, e.g. `launch:codex`. */
  key?: string;
  tone: FeedbackTone;
  /** One sentence, at most 18 characters in Chinese. */
  message: string;
  /** Reason, remedy and error code, shown only behind 详情. */
  detail?: string;
  /** At most one: retry, view or undo. 详情 is built in and does not count. */
  action?: FeedbackAction;
}

export interface FeedbackItem extends FeedbackInput {
  id: number;
  key: string;
  createdAt: number;
}

/** How long a success stays up, and how long one may wait behind another message. */
export const FEEDBACK_SUCCESS_MS = 3000;
/** The message on screen plus this many waiting behind it. */
const FEEDBACK_QUEUE_LIMIT = 3;

interface FeedbackContextValue {
  /** False without a provider, so standalone component tests render no viewport. */
  available: boolean;
  items: FeedbackItem[];
  show: (input: FeedbackInput) => void;
  /**
   * Reports a failed operation. `headline` is what failed ("无法启动 Codex"); the
   * localised reason, its remedy and the error code go behind 详情, which is where
   * the hint describeFailure produces finally has somewhere to be shown.
   * A cancellation is the user's own doing and reports nothing.
   */
  failure: (error: unknown, headline: string, options?: Pick<FeedbackInput, "key" | "action">) => void;
  dismiss: (id: number) => void;
}

const noop = () => undefined;

const FeedbackContext = createContext<FeedbackContextValue>({
  available: false,
  items: [],
  show: noop,
  failure: noop,
  dismiss: noop,
});

/** Persistent unless it is a success: a failure or a warning waits for the user. */
export function feedbackPersists(tone: FeedbackTone): boolean {
  return tone !== "success";
}

/**
 * The queue after `input` arrives. Pure so the ordering rules are testable:
 * a matching key replaces in place, anything else waits its turn, and the
 * oldest waiting success is the first thing dropped when the queue is full --
 * a failure is never pushed out by later good news.
 */
export function enqueueFeedback(items: FeedbackItem[], next: FeedbackItem): FeedbackItem[] {
  const index = items.findIndex((item) => item.key === next.key);
  if (index >= 0) return items.map((item, position) => (position === index ? next : item));
  const queued = [...items, next];
  while (queued.length > FEEDBACK_QUEUE_LIMIT + 1) {
    const waiting = queued.findIndex((item, position) => position > 0 && !feedbackPersists(item.tone));
    queued.splice(waiting > 0 ? waiting : 1, 1);
  }
  return queued;
}

/**
 * The queue once the message on screen is gone. A success that waited longer
 * than it would have been shown is stale: "已保存" surfacing a minute later,
 * after the user closed a failure, describes a moment that has passed.
 */
export function advanceFeedback(items: FeedbackItem[], now: number): FeedbackItem[] {
  return items.filter((item) => feedbackPersists(item.tone) || now - item.createdAt < FEEDBACK_SUCCESS_MS);
}

export function FeedbackProvider({ children }: PropsWithChildren) {
  const { t } = useI18n();
  const [items, setItems] = useState<FeedbackItem[]>([]);
  const nextId = useRef(1);

  const show = useCallback((input: FeedbackInput) => {
    const id = nextId.current++;
    const item: FeedbackItem = { ...input, id, key: input.key || `feedback:${id}`, createdAt: Date.now() };
    setItems((current) => enqueueFeedback(current, item));
  }, []);

  const dismiss = useCallback((id: number) => {
    setItems((current) => {
      const remaining = current.filter((item) => item.id !== id);
      // Only leaving the screen advances the queue; dismissing a waiting item
      // (not reachable from the UI today) must not drop the others.
      return current[0]?.id === id ? advanceFeedback(remaining, Date.now()) : remaining;
    });
  }, []);

  const failure = useCallback<FeedbackContextValue["failure"]>((error, headline, options) => {
    if (isCancellationError(error)) return;
    const described = describeFailure(error, headline, t);
    const lines = [
      described.message !== headline ? described.message : "",
      described.hint ?? "",
      described.code && described.code !== "INTERNAL_ERROR" ? `${t("错误码")}: ${described.code}` : "",
    ].filter(Boolean);
    show({ ...options, tone: "failure", message: headline, detail: lines.join("\n") || undefined });
  }, [show, t]);

  const value = useMemo(() => ({ available: true, items, show, failure, dismiss }), [dismiss, failure, items, show]);
  return <FeedbackContext.Provider value={value}>{children}</FeedbackContext.Provider>;
}

export function useFeedback(): FeedbackContextValue {
  return useContext(FeedbackContext);
}
