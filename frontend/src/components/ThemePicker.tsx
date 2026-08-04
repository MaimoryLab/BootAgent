import { Moon, Sun, SunMoon } from "lucide-react";

import { useI18n } from "../i18n";
import { type ThemePreference, useTheme } from "../state/ThemeContext";

const icons: Record<ThemePreference, typeof Sun> = {
  system: SunMoon,
  light: Sun,
  dark: Moon,
};

/**
 * The appearance setting, shaped like the language picker beside it.
 *
 * A select rather than a two-state switch: "system" is one of three real
 * choices, and a toggle cannot express it. The icon reflects what is in force,
 * so "system" shows the sun or the moon once resolved rather than staying
 * ambiguous.
 */
export function ThemePicker() {
  const { t } = useI18n();
  const { preference, setPreference, resolved } = useTheme();
  const Icon = preference === "system" ? icons[resolved] : icons[preference];

  return (
    <label className="theme-picker">
      <Icon size={16} aria-hidden="true" />
      <span>{t("外观")}</span>
      <select
        value={preference}
        onChange={(event) => setPreference(event.target.value as ThemePreference)}
        aria-label={t("外观")}
      >
        <option value="system">{t("跟随系统")}</option>
        <option value="light">{t("浅色")}</option>
        <option value="dark">{t("深色")}</option>
      </select>
    </label>
  );
}
