import { ChevronDown, ListChecks, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";

/**
 * The live install log, docked at the bottom of the sidebar.
 *
 * Installs run without a console, so this is where a user sees what a command
 * is doing while it runs. Runtime archive downloads use their page card rather
 * than a command-shaped log line. The durable copy is in ~/.oneagent/logs/<date>.log.
 */
export function TaskCenter() {
  const { t } = useI18n();
  const { log, progress, clear } = useTaskCenter();
  const [open, setOpen] = useState(false);
  const feed = useRef<HTMLPreElement>(null);
  const active = Object.keys(progress).length > 0;

  // Follow the tail while open, the way a terminal does.
  useEffect(() => {
    const element = feed.current;
    if (open && element) element.scrollTop = element.scrollHeight;
  }, [log, open]);

  const lines = log ? log.split("\n").filter((line, index, all) => line !== "" || index !== all.length - 1) : [];

  return (
    <section className={`task-center${open ? " is-open" : ""}`}>
      <button
        type="button"
        className="task-center-trigger"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
      >
        <ListChecks size={16} aria-hidden="true" />
        <span>{t("任务中心")}</span>
        {active ? <em className="task-center-dot" aria-label={t("有任务正在运行")} /> : null}
        <ChevronDown size={15} aria-hidden="true" />
      </button>
      {open ? (
        <div className="task-center-body">
          {lines.length ? (
            <pre ref={feed} className="task-center-feed" aria-live="polite">
              {lines.join("\n")}
            </pre>
          ) : (
            <p className="task-center-empty">{t("暂无任务日志，安装 Agent 时会显示在这里。运行时下载会显示进度卡片。")}</p>
          )}
          <div className="task-center-actions">
            <span>{t("完整日志：~/.oneagent/logs")}</span>
            <button type="button" onClick={clear} disabled={!lines.length}>
              <Trash2 size={14} aria-hidden="true" />
              {t("清空")}
            </button>
          </div>
        </div>
      ) : null}
    </section>
  );
}
