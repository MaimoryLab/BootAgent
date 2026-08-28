import {
  createContext,
  type PropsWithChildren,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useInRouterContext, useLocation } from "react-router-dom";

import { api } from "../backend/api";
import type { InstallOutput, TaskHistoryRecord } from "../types/api";

const TASK_STORAGE_KEY = "bootagent.task-center.v1";

function persistedTasks(): TaskRecord[] {
  if (typeof window === "undefined" || import.meta.env.MODE === "test") return [];
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(TASK_STORAGE_KEY) || "[]");
    if (!Array.isArray(value)) return [];
    return value.filter((task): task is TaskRecord => Boolean(task && typeof task === "object" && typeof task.id === "string" && typeof task.target === "string" && typeof task.kind === "string" && typeof task.startedAt === "number"))
      .map((task) => task.state === "running"
        ? { ...task, state: "failure", phase: "failed", message: task.message || "应用重启后任务状态未知，请检查安装结果", events: appendEvent(task.events || [], { at: Date.now(), kind: "result", phase: "failed", message: "应用重启后任务状态未知" }) }
        : { ...task, events: task.events || [] });
  } catch {
    return [];
  }
}

/** One download in flight. total is 0 when the server sent no Content-Length. */
export interface TaskProgress {
  received: number;
  total: number;
  /** Bytes per second calculated from live progress samples. */
  speed?: number;
  /** Estimated seconds remaining when total and speed are known. */
  etaSeconds?: number;
}

export type TaskPhase = "preparing" | "source" | "downloading" | "verifying" | "installing" | "configuring" | "waiting_restart" | "completed" | "failed" | "cancelled";

export type TaskKind = "install" | "update" | "uninstall" | "download" | "migration";
type TaskState = "running" | "success" | "failure" | "cancelled";

export interface TaskOutcome {
  kind: Exclude<TaskState, "running">;
  message: string;
  errorCode?: string;
  exitCode?: number;
  retryable?: boolean;
}

export interface TaskEvent {
  at: number;
  kind: "phase" | "source" | "log" | "result";
  phase?: TaskPhase;
  source?: string;
  message: string;
}

export type TaskCanceller = () => void | PromiseLike<void>;

export interface TaskAction {
  label: string;
  run: () => void | PromiseLike<void>;
}

export function taskCanceller(request: { cancel?: (cause?: unknown) => void | PromiseLike<void> }): TaskCanceller | undefined {
  return typeof request.cancel === "function" ? () => request.cancel?.() : undefined;
}

export interface TaskInput {
  /** Stable card id. */
  id?: string;
  kind: TaskKind;
  target: string;
  title: string;
  /** Route to return to when the card is clicked. */
  route: string;
  /** False keeps a status-only task from navigating when clicked. */
  openable?: boolean;
  /** False hides cancellation for an operation that must finish atomically. */
  cancellable?: boolean;
  /** Progress events use this when it differs from target. */
  progressTarget?: string;
  /** Cards backed by one request are cancelled together. */
  group?: string;
  action?: TaskAction;
  /** Optional locked or requested version shown by the task center. */
  version?: string;
  /** Safe source label or host; never a secret-bearing URL. */
  source?: string;
}

export interface TaskRecord extends TaskInput {
  id: string;
  progressTarget: string;
  state: TaskState;
  phase: TaskPhase;
  progress?: TaskProgress;
  message?: string;
  errorCode?: string;
  exitCode?: number;
  retryable?: boolean;
  log?: string;
  startedAt: number;
  events: TaskEvent[];
}

function defaultTask(target: string): TaskInput {
  return { kind: "download", target, title: target, route: "/overview" };
}

/** The stable key shared by page guards and task cards. */
export function taskKey(kind: TaskKind, target: string): string {
  return `${kind}:${target}`;
}

function taskLockKey(kind: TaskKind, target: string): string {
  return kind === "install" || kind === "update" || kind === "uninstall" ? `agent:${target}` : taskKey(kind, target);
}

/** HashRouter is used by the desktop shell; this also works in browser tests. */
function taskRoute(): string {
  if (typeof window === "undefined") return "/overview";
  const hash = window.location.hash;
  return hash.startsWith("#") && hash.slice(1) ? hash.slice(1) : `${window.location.pathname}${window.location.search}`;
}

function useRouterTaskRoute(): string {
  const location = useLocation();
  return `${location.pathname}${location.search}${location.hash}`;
}

/** Reads Router state when available; standalone component tests use the window fallback. */
export function useTaskRoute(): string {
  const inRouter = useInRouterContext();
  return inRouter ? useRouterTaskRoute() : taskRoute();
}

export interface TaskCenterValue {
  /** All active and terminal cards, kept until explicitly dismissed. */
  tasks: TaskRecord[];
  /** Byte progress keyed by the backend target for existing progress rows. */
  progress: Record<string, TaskProgress>;
  /** Compatibility projection for callers that only need a target presence. */
  running: Record<string, true>;
  /** Registers a card and returns false when the operation target is locked. */
  startTask: (task: TaskInput | string) => boolean;
  /** Finishes one card. `id` may be a card id or a target for old callers. */
  finishTask: (id: string, outcome: TaskOutcome) => void;
  setTaskCanceller: (id: string, cancel?: TaskCanceller) => void;
  setTaskAction: (id: string, action?: TaskAction) => void;
  setTaskMessage: (id: string, message: string) => void;
  setTaskPhase: (id: string, phase: TaskPhase) => void;
  cancelTask: (id: string, message?: string) => void;
  dismissTask: (id: string) => void;
  taskFor: (id: string) => TaskRecord | undefined;
  isTaskRunning: (id: string) => boolean;
}

const TaskCenterContext = createContext<TaskCenterValue>({
  tasks: [],
  progress: {},
  running: {},
  // A component rendered outside the provider still performs its operation;
  // it simply cannot display a durable card in that embedding.
  startTask: () => true,
  finishTask: () => {},
  setTaskCanceller: () => {},
  setTaskAction: () => {},
  setTaskMessage: () => {},
  setTaskPhase: () => {},
  cancelTask: () => {},
  dismissTask: () => {},
  taskFor: () => undefined,
  isTaskRunning: () => false,
});

function taskInputFor(value: TaskInput | string): TaskInput {
  return typeof value === "string" ? defaultTask(value) : value;
}

export function installTaskRoute(target: string): string {
  return `/tasks/install/${encodeURIComponent(target)}`;
}

export function updateTaskRoute(target: string): string {
  return `/tasks/update/${encodeURIComponent(target)}`;
}

function outputText(output: InstallOutput): string {
  if (output.kind === "progress") return "";
  if (output.kind === "phase" || output.kind === "source") return "";
  if (output.kind === "command") return `$ ${output.args.join(" ")}\n`;
  return output.text;
}

function appendEvent(events: TaskEvent[], event: TaskEvent): TaskEvent[] {
  const next = [...events, event];
  return next.length > 200 ? next.slice(next.length - 200) : next;
}

function toHistory(task: TaskRecord): TaskHistoryRecord {
  return {
    id: task.id, kind: task.kind, target: task.target, title: task.title, route: task.route,
    progressTarget: task.progressTarget, state: task.state, phase: task.phase, version: task.version || "",
    source: task.source || "", message: task.message || "", errorCode: task.errorCode || "",
    exitCode: task.exitCode ?? null, retryable: task.retryable === true, startedAt: task.startedAt,
    log: task.log || "", events: task.events.map((event) => ({ at: event.at, kind: event.kind, phase: event.phase || "", source: event.source || "", message: event.message })),
  };
}

function fromHistory(record: TaskHistoryRecord): TaskRecord {
  const state = record.state === "success" || record.state === "failure" || record.state === "cancelled" ? record.state : "failure";
  return {
    id: record.id, kind: record.kind as TaskKind, target: record.target, title: record.title, route: record.route,
    progressTarget: record.progressTarget || record.target, state, phase: state === "failure" && record.state === "running" ? "failed" : record.phase as TaskPhase,
    version: record.version || undefined, source: record.source || undefined, message: record.state === "running" ? (record.message || "应用重启后任务状态未知，请检查安装结果") : record.message || undefined,
    errorCode: record.errorCode || undefined, exitCode: record.exitCode ?? undefined, retryable: record.retryable === true,
    startedAt: record.startedAt, log: record.log || undefined, events: (record.events || []).map((event) => ({ at: event.at, kind: event.kind as TaskEvent["kind"], phase: event.phase as TaskPhase || undefined, source: event.source || undefined, message: event.message })),
  };
}

/**
 * The provider is mounted above the route content. A page can therefore unmount while
 * its Go request is still running without losing the card or its progress.
 */
export function TaskCenterProvider({ children }: PropsWithChildren) {
  const [tasks, setTasks] = useState<TaskRecord[]>(persistedTasks);
  const [progress, setProgress] = useState<Record<string, TaskProgress>>({});
  const progressSamplesRef = useRef(new Map<string, { received: number; at: number }>());
  const historyHydratedRef = useRef(false);
  const tasksRef = useRef<TaskRecord[]>([]);
  const cancellersRef = useRef(new Map<string, TaskCanceller>());
  const pendingCancelsRef = useRef(new Set<string>());
  const updateTasks = useCallback((update: (current: TaskRecord[]) => TaskRecord[]) => {
    const next = update(tasksRef.current);
    tasksRef.current = next;
    setTasks(next);
  }, []);

  useEffect(() => {
    if (typeof window === "undefined" || import.meta.env.MODE === "test") return;
    const load = api.loadTaskHistory;
    if (typeof load !== "function") { historyHydratedRef.current = true; return; }
    void load().then((records) => {
      if (records.length) {
        const restored = records.map(fromHistory);
        tasksRef.current = restored;
        setTasks(restored);
      }
      historyHydratedRef.current = true;
    }).catch(() => { historyHydratedRef.current = true; });
  }, []);

  useEffect(() => {
    if (typeof window === "undefined" || import.meta.env.MODE === "test" || !historyHydratedRef.current) return;
    const records = tasks.map(toHistory);
    const save = api.saveTaskHistory;
    if (typeof save === "function") void save(records).catch(() => {});
    try {
      const safe = tasks.map(({ action: _action, ...task }) => task);
      window.localStorage.setItem(TASK_STORAGE_KEY, JSON.stringify(safe.slice(-50)));
    } catch {
      // A full or unavailable browser store must not break an install task.
    }
  }, [tasks]);

  useEffect(
    () =>
      api.onInstallOutput((output: InstallOutput) => {
        const outputTarget = output.kind === "progress" || output.kind === "source" ? output.target : undefined;
        const matchesTask = (task: TaskRecord) => task.kind !== "migration" && task.state === "running" && (output.agent
          ? task.target === output.agent
          : outputTarget !== undefined && (task.progressTarget === outputTarget || task.target === outputTarget));
        const targetTasks = tasksRef.current.filter(matchesTask);
        if (output.kind === "progress") {
          const now = Date.now();
          const previous = progressSamplesRef.current.get(output.target);
          const elapsed = previous ? Math.max(0.001, (now - previous.at) / 1000) : 0;
          const speed = previous && output.received >= previous.received ? (output.received - previous.received) / elapsed : 0;
          const remaining = output.total > output.received && speed > 0 ? Math.ceil((output.total - output.received) / speed) : undefined;
          progressSamplesRef.current.set(output.target, { received: output.received, at: now });
          const nextProgress: TaskProgress = { received: output.received, total: output.total, ...(speed > 0 ? { speed } : {}), ...(remaining !== undefined ? { etaSeconds: remaining } : {}) };
          setProgress((current) => ({
            ...current,
            [output.target]: nextProgress,
          }));
          updateTasks((current) => current.map((task) => (
            matchesTask(task) ? { ...task, phase: "downloading", progress: nextProgress } : task
          )));
          return;
        }
        if (!targetTasks.length) return;
        updateTasks((current) => current.map((task) => (
          matchesTask(task)
            ? { ...task, ...(output.kind === "source" ? { phase: "source", source: output.source, events: appendEvent(task.events, { at: Date.now(), kind: "source", phase: "source", source: output.source, message: "source selected" }) } : {}), ...(output.kind === "phase" && output.phase === "verified" ? { phase: "verifying", events: appendEvent(task.events, { at: Date.now(), kind: "phase", phase: "verifying", message: "checksum verified" }) } : {}), ...(output.kind === "command" && task.phase === "preparing" ? { phase: "installing" } : {}), ...(outputText(output) ? { log: `${task.log || ""}${outputText(output)}`, events: appendEvent(task.events, { at: Date.now(), kind: "log", message: outputText(output).trim() }) } : {}) }
            : task
        )));
      }),
    [updateTasks],
  );

  const startTask = useCallback((value: TaskInput | string): boolean => {
    const input = taskInputFor(value);
    const id = input.id || taskKey(input.kind, input.target);
    const progressTarget = input.progressTarget || input.target;
    const lock = taskLockKey(input.kind, input.target);
    if (tasksRef.current.some((task) => task.state === "running" && taskLockKey(task.kind, task.target) === lock)) return false;
    cancellersRef.current.delete(id);
    pendingCancelsRef.current.delete(id);
    setProgress((current) => {
      const target = progressTarget;
      if (!(target in current)) return current;
      const next = { ...current };
      delete next[target];
      return next;
    });
    progressSamplesRef.current.delete(progressTarget);
    updateTasks((current) => {
      const next = current.filter((task) => task.id !== id);
      const startedAt = Date.now();
      next.push({ ...input, id, progressTarget, state: "running", phase: "preparing", startedAt, events: [{ at: startedAt, kind: "phase", phase: "preparing", message: "task started" }] });
      return next;
    });
    return true;
  }, [updateTasks]);

  const setTaskCanceller = useCallback((id: string, cancel?: TaskCanceller) => {
    const task = tasksRef.current.find((item) => item.id === id || item.target === id);
    const taskID = task?.id || id;
    if (!cancel) {
      cancellersRef.current.delete(taskID);
      return;
    }
    if (pendingCancelsRef.current.delete(taskID) || task?.state === "cancelled") {
      try {
        void Promise.resolve(cancel()).catch(() => {});
      } catch {
        // The card is already cancelled; a failing bridge cancellation cannot restore it.
      }
      return;
    }
    if (task?.state === "running") cancellersRef.current.set(taskID, cancel);
  }, []);

  const setTaskAction = useCallback((id: string, action?: TaskAction) => {
    updateTasks((current) => current.map((task) => (
      task.id === id || task.target === id ? { ...task, action } : task
    )));
  }, [updateTasks]);

  const setTaskMessage = useCallback((id: string, message: string) => {
    updateTasks((current) => current.map((task) => (
      task.id === id || task.target === id ? { ...task, message } : task
    )));
  }, [updateTasks]);

  const setTaskPhase = useCallback((id: string, phase: TaskPhase) => {
    updateTasks((current) => current.map((task) => task.id === id || task.target === id ? { ...task, phase, events: appendEvent(task.events, { at: Date.now(), kind: "phase", phase, message: phase }) } : task));
  }, [updateTasks]);

  const finishTask = useCallback((id: string, outcome: TaskOutcome) => {
    const hasExactID = tasksRef.current.some((task) => task.id === id);
    const matches = (task: TaskRecord) => (hasExactID ? task.id === id : task.target === id) && task.state === "running";
    const matched = tasksRef.current.filter(matches);
    const progressTargets = new Set(matched.map((task) => task.progressTarget));
    for (const task of matched) {
      cancellersRef.current.delete(task.id);
      pendingCancelsRef.current.delete(task.id);
    }
    setProgress((current) => {
      if (!progressTargets.size) return current;
      const next = { ...current };
      for (const target of progressTargets) delete next[target];
      return next;
    });
    updateTasks((current) => current.map((task) => matches(task)
      ? { ...task, state: outcome.kind, phase: outcome.kind === "success" ? "completed" : outcome.kind === "failure" ? "failed" : "cancelled", message: outcome.message, errorCode: outcome.errorCode, exitCode: outcome.exitCode, retryable: outcome.retryable, progress: undefined, events: appendEvent(task.events, { at: Date.now(), kind: "result", phase: outcome.kind === "success" ? "completed" : outcome.kind === "failure" ? "failed" : "cancelled", message: outcome.message }) }
      : task));
  }, [updateTasks]);

  const cancelTask = useCallback((id: string, message = "") => {
    const root = tasksRef.current.find((task) => task.state === "running" && task.id === id)
      || tasksRef.current.find((task) => task.state === "running" && task.target === id);
    if (!root) return;
    const matched = tasksRef.current.filter((task) => task.state === "running" && (
      root.group ? task.group === root.group : task.id === root.id
    ));
    const progressTargets = new Set(matched.map((task) => task.progressTarget));
    const cancellers = new Set<TaskCanceller>();
    for (const task of matched) {
      const cancel = cancellersRef.current.get(task.id);
      if (cancel) cancellers.add(cancel);
      cancellersRef.current.delete(task.id);
    }
    if (!cancellers.size) {
      for (const task of matched) pendingCancelsRef.current.add(task.id);
    }
    setProgress((current) => {
      const next = { ...current };
      for (const target of progressTargets) delete next[target];
      return next;
    });
    const ids = new Set(matched.map((task) => task.id));
    updateTasks((current) => current.map((task) => ids.has(task.id)
      ? { ...task, state: "cancelled", phase: "cancelled", message, progress: undefined, events: appendEvent(task.events, { at: Date.now(), kind: "result", phase: "cancelled", message: message || "cancelled" }) }
      : task));
    for (const cancel of cancellers) {
      try {
        void Promise.resolve(cancel()).catch(() => {});
      } catch {
        // The request may already have settled between the click and cancellation.
      }
    }
  }, [updateTasks]);

  const dismissTask = useCallback((id: string) => {
    cancellersRef.current.delete(id);
    pendingCancelsRef.current.delete(id);
    updateTasks((current) => current.filter((task) => task.id !== id || task.state === "running"));
  }, [updateTasks]);

  const taskFor = useCallback((id: string) => tasks.find((task) => task.id === id || task.target === id), [tasks]);
  const isTaskRunning = useCallback((id: string) => tasks.some((task) => task.id === id && task.state === "running"), [tasks]);

  const running = useMemo<Record<string, true>>(() => {
    const result: Record<string, true> = {};
    for (const task of tasks) {
      if (task.state === "running") {
        result[task.id] = true;
        result[task.target] = true;
        result[task.progressTarget] = true;
      }
    }
    return result;
  }, [tasks]);

  const value = useMemo<TaskCenterValue>(
    () => ({ tasks, progress, running, startTask, finishTask, setTaskCanceller, setTaskAction, setTaskMessage, setTaskPhase, cancelTask, dismissTask, taskFor, isTaskRunning }),
    [cancelTask, dismissTask, finishTask, isTaskRunning, progress, running, setTaskAction, setTaskCanceller, setTaskMessage, setTaskPhase, startTask, taskFor, tasks],
  );
  return <TaskCenterContext.Provider value={value}>{children}</TaskCenterContext.Provider>;
}

export function useTaskCenter(): TaskCenterValue {
  return useContext(TaskCenterContext);
}
