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
import type { InstallOutput } from "../types/api";

/** One download in flight. total is 0 when the server sent no Content-Length. */
export interface TaskProgress {
  received: number;
  total: number;
}

export type TaskKind = "install" | "update" | "download";
export type TaskState = "running" | "success" | "failure";

export interface TaskOutcome {
  kind: "success" | "failure";
  message: string;
}

export interface TaskInput {
  /** Stable card id. */
  id?: string;
  kind: TaskKind;
  target: string;
  title: string;
  /** Route to return to when the card is clicked. */
  route: string;
  /** Progress events use this when it differs from target. */
  progressTarget?: string;
}

export interface TaskRecord extends TaskInput {
  id: string;
  progressTarget: string;
  state: TaskState;
  progress?: TaskProgress;
  message?: string;
  startedAt: number;
}

function defaultTask(target: string): TaskInput {
  return { kind: "download", target, title: target, route: "/overview" };
}

/** The stable key shared by page guards and task cards. */
export function taskKey(kind: TaskKind, target: string): string {
  return `${kind}:${target}`;
}

function taskLockKey(kind: TaskKind, target: string): string {
  return kind === "install" || kind === "update" ? `agent:${target}` : taskKey(kind, target);
}

/** HashRouter is used by the desktop shell; this also works in browser tests. */
export function taskRoute(): string {
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
  /** Compatibility helper for callers that need to drop a stale bar. */
  resetProgress: (target: string) => void;
  /** Compatibility projection for callers that only need a target presence. */
  running: Record<string, true>;
  /** Compatibility projection of terminal outcomes keyed by target. */
  outcomes: Record<string, TaskOutcome>;
  /** Registers a card and returns false when the operation target is locked. */
  startTask: (task: TaskInput | string) => boolean;
  /** Finishes one card. `id` may be a card id or a target for old callers. */
  finishTask: (id: string, outcome: TaskOutcome) => void;
  /** Compatibility removal for a terminal task. */
  clearOutcome: (id: string) => void;
  dismissTask: (id: string) => void;
  taskFor: (id: string) => TaskRecord | undefined;
  isTaskRunning: (id: string) => boolean;
}

const TaskCenterContext = createContext<TaskCenterValue>({
  tasks: [],
  progress: {},
  resetProgress: () => {},
  running: {},
  outcomes: {},
  // A component rendered outside the provider still performs its operation;
  // it simply cannot display a durable card in that embedding.
  startTask: () => true,
  finishTask: () => {},
  clearOutcome: () => {},
  dismissTask: () => {},
  taskFor: () => undefined,
  isTaskRunning: () => false,
});

function taskInputFor(value: TaskInput | string): TaskInput {
  return typeof value === "string" ? defaultTask(value) : value;
}

/**
 * The provider is mounted above the route content. A page can therefore unmount while
 * its Go request is still running without losing the card or its progress.
 */
export function TaskCenterProvider({ children }: PropsWithChildren) {
  const [tasks, setTasks] = useState<TaskRecord[]>([]);
  const [progress, setProgress] = useState<Record<string, TaskProgress>>({});
  const tasksRef = useRef<TaskRecord[]>([]);
  const updateTasks = useCallback((update: (current: TaskRecord[]) => TaskRecord[]) => {
    const next = update(tasksRef.current);
    tasksRef.current = next;
    setTasks(next);
  }, []);

  useEffect(
    () =>
      api.onInstallOutput((output: InstallOutput) => {
        if (output.kind !== "progress") return;
        const targetTasks = tasksRef.current.filter((task) => task.progressTarget === output.target || task.target === output.target);
        if (targetTasks.length && !targetTasks.some((task) => task.state === "running")) return;
        setProgress((current) => ({
          ...current,
          [output.target]: { received: output.received, total: output.total },
        }));
        updateTasks((current) => current.map((task) => (
          task.state === "running" && (task.progressTarget === output.target || task.target === output.target)
            ? { ...task, progress: { received: output.received, total: output.total } }
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
    setProgress((current) => {
      const target = progressTarget;
      if (!(target in current)) return current;
      const next = { ...current };
      delete next[target];
      return next;
    });
    updateTasks((current) => {
      const next = current.filter((task) => task.id !== id);
      next.push({ ...input, id, progressTarget, state: "running", startedAt: Date.now() });
      return next;
    });
    return true;
  }, [updateTasks]);

  const resetProgress = useCallback((target: string) => {
    setProgress((current) => {
      if (!(target in current)) return current;
      const next = { ...current };
      delete next[target];
      return next;
    });
    updateTasks((current) => current.map((task) => (
      task.progressTarget === target || task.target === target
        ? (() => {
            const { progress: _progress, ...withoutProgress } = task;
            return withoutProgress as TaskRecord;
          })()
        : task
    )));
  }, [updateTasks]);

  const finishTask = useCallback((id: string, outcome: TaskOutcome) => {
    const hasExactID = tasksRef.current.some((task) => task.id === id);
    const matches = (task: TaskRecord) => (hasExactID ? task.id === id : task.target === id) && task.state === "running";
    const progressTargets = new Set(tasksRef.current.filter(matches).map((task) => task.progressTarget));
    setProgress((current) => {
      if (!progressTargets.size) return current;
      const next = { ...current };
      for (const target of progressTargets) delete next[target];
      return next;
    });
    updateTasks((current) => current.map((task) => matches(task)
      ? { ...task, state: outcome.kind, message: outcome.message, progress: undefined }
      : task));
  }, [updateTasks]);

  const clearOutcome = useCallback((id: string) => {
    updateTasks((current) => current.filter((task) => {
      if (task.state === "running") return true;
      return task.id !== id && task.target !== id;
    }));
  }, [updateTasks]);

  const dismissTask = useCallback((id: string) => {
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

  const outcomes = useMemo<Record<string, TaskOutcome>>(() => {
    const result: Record<string, TaskOutcome> = {};
    for (const task of tasks) {
      if (task.state !== "running") result[task.target] = { kind: task.state, message: task.message || "" };
    }
    return result;
  }, [tasks]);

  const value = useMemo<TaskCenterValue>(
    () => ({ tasks, progress, resetProgress, running, outcomes, startTask, finishTask, clearOutcome, dismissTask, taskFor, isTaskRunning }),
    [clearOutcome, dismissTask, finishTask, isTaskRunning, outcomes, progress, resetProgress, running, startTask, taskFor, tasks],
  );
  return <TaskCenterContext.Provider value={value}>{children}</TaskCenterContext.Provider>;
}

export function useTaskCenter(): TaskCenterValue {
  return useContext(TaskCenterContext);
}
