import { createContext, type PropsWithChildren, useCallback, useContext, useEffect, useMemo, useState } from "react";

/**
 * The appearance setting.
 *
 * Three states, not two: "system" is a real choice, not the absence of one. A
 * two-state toggle would have to guess what "off" means the moment the desktop
 * flips, and a user who wants to track their desktop has no way to say so.
 *
 * The resolved palette lives in CSS (styles/tokens.css). This only decides which
 * class sits on <html>; no colour values belong here.
 */
export type ThemePreference = "system" | "light" | "dark";

export const THEME_STORAGE_KEY = "oneagent.theme";

function isPreference(value: unknown): value is ThemePreference {
  return value === "system" || value === "light" || value === "dark";
}

/** The stored choice, or "system" when nothing was stored or storage is barred. */
export function storedPreference(): ThemePreference {
  try {
    const saved = localStorage.getItem(THEME_STORAGE_KEY);
    if (isPreference(saved)) return saved;
  } catch {
    // Storage can be unavailable in hardened webviews; the system preference
    // still applies, it just cannot be overridden across restarts.
  }
  return "system";
}

/**
 * Put the preference on <html>.
 *
 * "system" carries no class at all, which is what lets the media query in
 * tokens.css decide. Exported so the pre-hydration script and the provider apply
 * it exactly the same way.
 */
function applyPreference(preference: ThemePreference): void {
  const root = document.documentElement;
  root.classList.toggle("theme-dark", preference === "dark");
  root.classList.toggle("theme-light", preference === "light");
}

interface ThemeContextValue {
  preference: ThemePreference;
  setPreference: (preference: ThemePreference) => void;
  /** What the preference resolves to right now, for anything that needs the answer rather than the setting. */
  resolved: "light" | "dark";
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined);

function systemPrefersDark(): boolean {
  return typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: dark)").matches === true;
}

export function ThemeProvider({ children }: PropsWithChildren) {
  const [preference, setPreferenceState] = useState<ThemePreference>(storedPreference);
  const [systemDark, setSystemDark] = useState(systemPrefersDark);

  // A desktop change still moves the app while the preference is "system". The
  // listener runs regardless of the current preference so switching back to
  // "system" already has the right answer.
  useEffect(() => {
    const query = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!query) return;
    const onChange = (event: MediaQueryListEvent) => setSystemDark(event.matches);
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  const resolved: "light" | "dark" = preference === "system" ? (systemDark ? "dark" : "light") : preference;

  useEffect(() => {
    applyPreference(preference);
  }, [preference]);

  // The two media-driven <meta name="theme-color"> tags in index.html cannot see
  // a forced palette, so the window chrome would keep the desktop's colour while
  // the content switched. One resolved tag overrides both.
  useEffect(() => {
    const id = "theme-color-resolved";
    let tag = document.head.querySelector<HTMLMetaElement>(`meta#${id}`);
    if (!tag) {
      tag = document.createElement("meta");
      tag.id = id;
      tag.name = "theme-color";
      document.head.append(tag);
    }
    tag.content = resolved === "dark" ? "#151517" : "#f5f5f7";
  }, [resolved]);

  const setPreference = useCallback((next: ThemePreference) => {
    setPreferenceState(next);
    try {
      localStorage.setItem(THEME_STORAGE_KEY, next);
    } catch {
      // In-memory only, as with the locale: the choice holds for this session.
    }
  }, []);

  const value = useMemo(() => ({ preference, setPreference, resolved }), [preference, setPreference, resolved]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const value = useContext(ThemeContext);
  if (!value) {
    throw new Error("useTheme must be used inside ThemeProvider");
  }
  return value;
}
