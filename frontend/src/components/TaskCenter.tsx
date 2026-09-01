import { CheckCircle2, ChevronDown, CircleAlert, CircleStop, LoaderCircle, ListChecks, RefreshCw, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useInRouterContext, useNavigate } from "react-router-dom";

import { useI18n } from "../i18n";
import { type TaskRecord, useTaskCenter } from "../state/TaskCenterContext";

function megabytes(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(1);
}

function rate(bytesPerSecond?: number): string {
  return bytesPerSecond && bytesPerSecond > 0 ? `${megabytes(bytesPerSecond)} MB/s` : "";
}

function eta(seconds?: number): string {
  if (!seconds || seconds < 0) return "";
  if (seconds < 60) return `${seconds}s`;
  return `${Math.ceil(seconds / 60)}m`;
}

function TaskCard({ task, onOpen, onCancel, onDismiss }: { task: TaskRecord; onOpen: () => void; onCancel: () => void; onDismiss: () => void }) {
  const { t } = useI18n();
  const progress = task.progress;
  const knownTotal = Boolean(progress && progress.total > 0);
  const percent = knownTotal && progress ? Math.min(100, Math.round((progress.received / progress.total) * 100)) : 0;
  const status = task.state === "running" ? t("进行中") : task.state === "success" ? t("已完成") : task.state === "failure" ? t("失败") : t("已取消");
  const phase = task.phase === "preparing" ? t("准备中") : task.phase === "source" ? t("连接下载源") : task.phase === "downloading" ? t("下载中") : task.phase === "verifying" ? t("校验中") : task.phase === "installing" ? t("安装中") : task.phase === "configuring" ? t("配置中") : task.phase === "waiting_restart" ? t("等待重启") : task.phase === "completed" ? t("已完成") : task.phase === "failed" ? t("失败") : t("已取消");
  const Main = task.openable === false ? "div" : "button";
  return (
    <article className={`task-card is-${task.state}${task.action ? " has-action" : ""}`}>
      <Main className="task-card-main" {...(task.openable === false ? {} : { type: "button", onClick: onOpen, "aria-label": t("返回任务页面：{title}", { title: task.title }) })}>
        <span className="task-card-icon" aria-hidden="true">
          {task.state === "running" ? <LoaderCircle size={16} className="spin" /> : task.state === "success" ? <CheckCircle2 size={16} /> : task.state === "failure" ? <CircleAlert size={16} /> : <CircleStop size={16} />}
        </span>
        <span className="task-card-copy">
          <strong>{task.title}</strong>
          <small>
            {task.state === "running" ? <><span>{status}</span><span> · {phase}</span></> : <span>{status}{task.message ? ` · ${task.message}` : ""}{task.errorCode ? ` · ${task.errorCode}` : ""}</span>}
            {task.version ? <span> · {task.version}</span> : null}
            {task.source ? <span> · {task.source}</span> : null}
          </small>
          {task.state === "running" && progress ? (
            <span className="task-card-progress">
              <span className={`task-card-progress-track${knownTotal ? "" : " is-indeterminate"}`}>
                <span className="task-card-progress-fill" style={knownTotal ? { width: `${percent}%` } : undefined} />
              </span>
              <small>
                {knownTotal
                  ? t("已下载 {done} MB / {total} MB（{percent}%）", { done: megabytes(progress.received), total: megabytes(progress.total), percent })
                  : t("已下载 {done} MB", { done: megabytes(progress.received) })}
                {rate(progress.speed) ? ` · ${rate(progress.speed)}` : ""}
                {progress.etaSeconds !== undefined ? ` · ${t("剩余约 {eta}", { eta: eta(progress.etaSeconds) })}` : ""}
              </small>
            </span>
          ) : null}
        </span>
      </Main>
      {task.state !== "running" && task.action ? (
        <button type="button" className="task-card-action" onClick={() => { void task.action?.run(); }}>
          <RefreshCw size={13} aria-hidden="true" />
          {task.action.label}
        </button>
      ) : null}
      {task.state === "running" && task.cancellable !== false ? (
        <button type="button" className="task-card-dismiss is-cancel" onClick={onCancel} aria-label={t("取消任务")} title={t("取消任务")}>
          <CircleStop size={14} aria-hidden="true" />
        </button>
      ) : (
        <button type="button" className="task-card-dismiss" onClick={onDismiss} aria-label={t("关闭任务")} title={t("关闭任务")}>
          <X size={14} aria-hidden="true" />
        </button>
      )}
    </article>
  );
}

function TaskCenterBody({ navigate }: { navigate: (route: string) => void }) {
  const { t } = useI18n();
  const { tasks, cancelTask, dismissTask } = useTaskCenter();
  return (
    <div className="task-center-body">
      {tasks.length ? tasks.map((task) => (
        <TaskCard
          key={task.id}
          task={task}
          onOpen={() => task.route && navigate(task.route)}
          onCancel={() => cancelTask(task.id)}
          onDismiss={() => dismissTask(task.id)}
        />
      )) : <p className="task-center-empty">{t("暂无任务")}</p>}
    </div>
  );
}

function TaskCenterWithRouter() {
  const navigate = useNavigate();
  return <TaskCenterShell navigate={navigate} />;
}

function TaskCenterShell({ navigate }: { navigate: (route: string) => void }) {
  const { t } = useI18n();
  const { tasks } = useTaskCenter();
  const [open, setOpen] = useState(false);
  const centerRef = useRef<HTMLElement>(null);
  const active = tasks.some((task) => task.state === "running");

  useEffect(() => {
    if (active) setOpen(true);
  }, [active]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!centerRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  return (
    <section ref={centerRef} className={`task-center${open ? " is-open" : ""}`}>
      <button
        type="button"
        className="task-center-trigger"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        aria-label={t("任务中心")}
      >
        <ListChecks size={16} aria-hidden="true" />
        <span>{t("任务中心")}</span>
        {active ? <em className="task-center-dot" aria-label={t("有任务正在运行")} /> : null}
        <ChevronDown size={15} aria-hidden="true" />
      </button>
      {open ? <TaskCenterBody navigate={(route) => { setOpen(false); navigate(route); }} /> : null}
    </section>
  );
}

/** The standalone branch keeps component tests and embedders router-free. */
function TaskCenterWithoutRouter() {
  return <TaskCenterShell navigate={() => {}} />;
}

export function TaskCenter() {
  return useInRouterContext() ? <TaskCenterWithRouter /> : <TaskCenterWithoutRouter />;
}
