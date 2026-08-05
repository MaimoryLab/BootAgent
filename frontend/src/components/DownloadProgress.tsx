import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";

function megabytes(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(1);
}

/**
 * The download bar for one install target, shown inside its install surface.
 *
 * A pending caller can render it before the first progress event, which keeps
 * an internal download visible without inventing a command log line.
 * A download whose server sent no Content-Length gets an indeterminate bar
 * rather than a percentage computed from a zero total.
 */
export function DownloadProgress({ target, pending = false }: { target: string; pending?: boolean }) {
  const { t } = useI18n();
  const { progress, running } = useTaskCenter();
  const current = progress[target];
  // `running` comes from the provider above the router, so the bar reappears
  // when the user navigates back to a download still in flight. `pending` stays
  // as an explicit override for a caller that knows a download has started but
  // whose target is not keyed in `running` yet. Without either, an ambient row
  // renders nothing until real bytes arrive.
  if (!current && !pending && !running[target]) return null;

  const received = current?.received ?? 0;
  const total = current?.total ?? 0;
  const known = total > 0;
  const percent = known ? Math.min(100, Math.round((received / total) * 100)) : 0;
  return (
    <div className="download-progress">
      <div
        className={`download-progress-track${known ? "" : " is-indeterminate"}`}
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={known ? 100 : undefined}
        aria-valuenow={known ? percent : undefined}
        aria-label={t("下载进度")}
      >
        <span className="download-progress-fill" style={known ? { width: `${percent}%` } : undefined} />
      </div>
      <small>
        {known
          ? t("已下载 {done} MB / {total} MB（{percent}%）", {
              done: megabytes(received),
              total: megabytes(total),
              percent,
            })
          : t("已下载 {done} MB", { done: megabytes(received) })}
      </small>
    </div>
  );
}
