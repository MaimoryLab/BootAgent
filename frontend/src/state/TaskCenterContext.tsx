import {
  createContext,
  type PropsWithChildren,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

import { api } from "../backend/api";
import type { InstallOutput } from "../types/api";

/** One download in flight. total is 0 when the server sent no Content-Length. */
export interface TaskProgress {
  received: number;
  total: number;
}

export interface TaskCenterValue {
  /** The live feed, newest at the end. */
  log: string;
  /** Keyed by the runtime or desktop-agent target the download belongs to. */
  progress: Record<string, TaskProgress>;
  /** Drops a stale bar before the same target is installed again. */
  resetProgress: (target: string) => void;
  clear: () => void;
}

// The full history lives in ~/.oneagent/logs; this pane only covers what a user
// is watching right now, so it keeps a tail rather than the whole session. The
// feed is one string because command output arrives in chunks that split
// mid-line: keeping a line array would mean re-joining the open line on every
// chunk.
const maxLogChars = 64 * 1024;

// The default is inert rather than null: the feed is ambient decoration, so a
// component rendered outside the provider should show no log and no bar, not
// crash the page it happens to sit on.
const TaskCenterContext = createContext<TaskCenterValue>({
  log: "",
  progress: {},
  resetProgress: () => {},
  clear: () => {},
});

function formatCommand(args: string[]): string {
  return args.map((arg) => (/^[A-Za-z0-9_./:@%+=,-]+$/.test(arg) ? arg : JSON.stringify(arg))).join(" ");
}

/**
 * Collects the live install feed for the whole window.
 *
 * It is mounted above the router so a log line is not lost when the user
 * navigates away from the page that started the install, and so the runtime
 * prompts and the Task Center read one shared stream instead of subscribing
 * twice.
 */
export function TaskCenterProvider({ children }: PropsWithChildren) {
  const [log, setLog] = useState("");
  const [progress, setProgress] = useState<Record<string, TaskProgress>>({});

  useEffect(
    () =>
      api.onInstallOutput((output: InstallOutput) => {
        if (output.kind === "progress") {
          setProgress((current) => ({
            ...current,
            [output.target]: { received: output.received, total: output.total },
          }));
          return;
        }
        const text = output.kind === "command" ? `$ ${formatCommand(output.args)}\n` : output.text;
        if (!text) return;
        setLog((current) => {
          const separator = output.kind === "command" && current && !current.endsWith("\n") ? "\n" : "";
          return (current + separator + text).slice(-maxLogChars);
        });
      }),
    [],
  );

  const resetProgress = useCallback((target: string) => {
    setProgress((current) => {
      if (!(target in current)) return current;
      const next = { ...current };
      delete next[target];
      return next;
    });
  }, []);

  const clear = useCallback(() => setLog(""), []);

  const value = useMemo<TaskCenterValue>(
    () => ({ log, progress, resetProgress, clear }),
    [clear, log, progress, resetProgress],
  );
  return <TaskCenterContext.Provider value={value}>{children}</TaskCenterContext.Provider>;
}

export function useTaskCenter(): TaskCenterValue {
  return useContext(TaskCenterContext);
}
