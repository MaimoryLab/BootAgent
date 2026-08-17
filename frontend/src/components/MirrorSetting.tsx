import { useEffect, useState } from "react";
import { Globe2 } from "lucide-react";

import { api, describeFailure } from "../backend/api";
import { useI18n } from "../i18n";

/**
 * The single download-host preference, shared by every screen that starts a
 * download.
 *
 * One setting rather than one per screen: it governs runtime archives and
 * npm-managed Agent packages together, and a user who chose a mirror because
 * their network is slow means it for both.
 */
export function MirrorSetting() {
  const { t } = useI18n();
  const [preferMirror, setPreferMirror] = useState<boolean | null>(null);
  const [backupRetention, setBackupRetention] = useState(3);
  // Whether the tick came from the machine's region rather than from a choice.
  // Shown so an already-ticked box does not look like something the user did and
  // forgot, and so it is clear it can be turned off.
  const [fromRegion, setFromRegion] = useState(false);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    let active = true;
    api
      .getSettings()
      .then((settings) => {
        if (!active) return;
        setPreferMirror(settings.prefer_mirror);
        setFromRegion(settings.mirror_from_region);
        setBackupRetention(settings.backup_retention ?? 3);
      })
      .catch(() => {
        // An unreadable preference is not worth a visible error: the backend
        // falls back to the official source, which is also what we render.
        if (active) {
          setPreferMirror(false);
          // A zero retention tells the backend to preserve a value written by
          // a newer Settings page when this older control cannot read it.
          setBackupRetention(0);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const toggle = async (next: boolean) => {
    const previous = preferMirror;
    const wasFromRegion = fromRegion;
    setPreferMirror(next);
    // Touching the switch makes it the user's own choice either way.
    setFromRegion(false);
    setFailure("");
    try {
      const saved = await api.saveSettings({ prefer_mirror: next });
      setPreferMirror(saved.prefer_mirror);
      setFromRegion(saved.mirror_from_region);
      setBackupRetention(saved.backup_retention ?? backupRetention);
    } catch (error) {
      // Put the switch back so it never shows a preference that was not stored.
      setPreferMirror(previous);
      setFromRegion(wasFromRegion);
      setFailure(describeFailure(error, t("无法保存下载设置"), t).message);
    }
  };

  return (
    <div className="settings-row mirror-setting-row">
      <label className="toggle-row">
        <Globe2 size={18} aria-hidden="true" />
        <span>
          <strong>{t("优先使用国内镜像")}</strong>
          {fromRegion ? <small>{t("已根据系统地区设置默认使用镜像。可以改回官方源")}</small> : null}
          <small>
            {fromRegion
              ? t("已根据系统语言/地区自动开启。优化中国大陆地区的下载速度")
              : t("优化中国大陆地区的下载速度")}
          </small>
        </span>
        <input
          type="checkbox"
          role="switch"
          aria-label={t("优先使用国内镜像")}
          checked={preferMirror === true}
          disabled={preferMirror === null}
          onChange={(event) => void toggle(event.target.checked)}
        />
      </label>
      {failure ? <div className="notice notice-error">{failure}</div> : null}
    </div>
  );
}
