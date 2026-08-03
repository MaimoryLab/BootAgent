import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";

function megabytes(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(1);
}

/**
 * The download bar for one runtime, shown inside an install surface.
 *
 * A pending caller can render it before the first progress event, which keeps
 * an internal runtime download visible without inventing a command log line.
 * A download whose server sent no Content-Length gets an indeterminate bar
 * rather than a percentage computed from a zero total.
 */
export function DownloadProgress({ runtimeId, pending = false }: { runtimeId: string; pending?: boolean }) {
  const { t } = useI18n();
  const { progress } = useTaskCenter();
  const current = progress[runtimeId];
  // A caller that already knows a download is in flight can show the bar before
  // the first 200ms progress event arrives. The default remains lazy for
  // ambient rows that should render nothing until there is real activity.
  if (!current && !pending) return null;

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
