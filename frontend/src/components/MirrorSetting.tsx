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
  const [failure, setFailure] = useState("");

  useEffect(() => {
    let active = true;
    api
      .getSettings()
      .then((settings) => {
        if (!active) return;
        setPreferMirror(settings.prefer_mirror);
      })
      .catch(() => {
        // An unreadable preference is not worth a visible error: the backend
        // falls back to the official source, which is also what we render.
        if (active) {
          setPreferMirror(false);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const toggle = async (next: boolean) => {
    const previous = preferMirror;
    setPreferMirror(next);
    setFailure("");
    try {
      const saved = await api.saveSettings({ prefer_mirror: next });
      setPreferMirror(saved.prefer_mirror);
    } catch (error) {
      // Put the switch back so it never shows a preference that was not stored.
      setPreferMirror(previous);
      setFailure(describeFailure(error, t("无法保存下载设置"), t).message);
    }
  };

  return (
    <div className="settings-row mirror-setting-row">
      <label className="toggle-row">
        <Globe2 size={18} aria-hidden="true" />
        <span>
          <strong>{t("优先使用国内镜像")}</strong>
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
