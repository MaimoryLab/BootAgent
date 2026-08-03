import { useEffect, useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import { AdvancedSection } from "./AdvancedSection";

/**
 * The single download-host preference, shared by every screen that starts a
 * download.
 *
 * One setting rather than one per screen: it governs runtime archives and
 * npm-managed Agent packages together, and a user who chose a mirror because
 * their network is slow means it for both. It is rendered in more than one place
 * only because either screen can be the first one where a download is slow.
 */
export function MirrorSetting({ label }: { label?: string }) {
  const { t } = useI18n();
  const [preferMirror, setPreferMirror] = useState<boolean | null>(null);
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
      })
      .catch(() => {
        // An unreadable preference is not worth a visible error: the backend
        // falls back to the official source, which is also what we render.
        if (active) setPreferMirror(false);
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
      const saved = await api.saveSettings({ schema_version: 1, prefer_mirror: next, mirror_from_region: false });
      setPreferMirror(saved.prefer_mirror);
      setFromRegion(saved.mirror_from_region);
    } catch (error) {
      // Put the switch back so it never shows a preference that was not stored.
      setPreferMirror(previous);
      setFromRegion(wasFromRegion);
      setFailure(describeError(error, t("无法保存下载设置")).message);
    }
  };

  // The collapsed hint has to state what is actually in effect. Saying "官方源"
  // on a machine that is about to use the mirror would be worse than saying
  // nothing.
  const hint = fromRegion
    ? t("已根据系统地区设置默认使用镜像。可以改回官方源。")
    : preferMirror
      ? t("正在优先使用国内镜像。")
      : t("默认使用官方源。国内网络较慢时可以改用镜像。");

  return (
    <AdvancedSection label={label ?? t("下载源")} hint={hint}>
      {failure ? <div className="notice notice-error">{failure}</div> : null}
      <label className="toggle-row">
        <span>
          <strong>{t("优先使用国内镜像")}</strong>
          <small>
            {fromRegion
              ? t("已根据系统语言/地区自动开启。运行时和 npm 安装的 Agent 都会校验与官方源相同的锁定校验值。")
              : t("同时作用于运行时下载和 npm 安装的 Agent，校验值与官方源一致；运行时下载失败会自动回退。Aider（uv）不受影响。")}
          </small>
        </span>
        <input
          type="checkbox"
          role="switch"
          checked={preferMirror === true}
          disabled={preferMirror === null}
          onChange={(event) => void toggle(event.target.checked)}
        />
      </label>
    </AdvancedSection>
  );
}
