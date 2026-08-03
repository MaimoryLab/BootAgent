import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";

function megabytes(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(1);
}

/**
 * The download bar for one runtime, shown inside the installing prompt.
 *
 * Renders nothing until the first progress event arrives, so the button label
 * carries the "installing" state on its own until there are real bytes to show.
 * A download whose server sent no Content-Length gets an indeterminate bar
 * rather than a percentage computed from a zero total.
 */
export function DownloadProgress({ runtimeId }: { runtimeId: string }) {
  const { t } = useI18n();
  const { progress } = useTaskCenter();
  const current = progress[runtimeId];
  if (!current) return null;

  const known = current.total > 0;
  const percent = known ? Math.min(100, Math.round((current.received / current.total) * 100)) : 0;
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
              done: megabytes(current.received),
              total: megabytes(current.total),
              percent,
            })
          : t("已下载 {done} MB", { done: megabytes(current.received) })}
      </small>
    </div>
  );
}
