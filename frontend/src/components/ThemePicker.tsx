import { Moon, Sun, SunMoon } from "lucide-react";

import { useI18n } from "../i18n";
import { type ThemePreference, useTheme } from "../state/ThemeContext";
import { SelectField } from "./SelectField";

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

  // A div, not a label: label only associates implicitly with form elements, and
  // wrapping a button in one makes every click on the row activate it. The
  // accessible name comes from SelectField's own aria-label instead.
  return (
    <div className="theme-picker">
      <Icon size={16} aria-hidden="true" />
      <span>{t("外观")}</span>
      <SelectField
        className="theme-select"
        label={t("外观")}
        value={preference}
        onChange={(next) => setPreference(next as ThemePreference)}
        options={[
          { value: "system", label: t("跟随系统") },
          { value: "light", label: t("浅色") },
          { value: "dark", label: t("深色") },
        ]}
      />
    </div>
  );
}
