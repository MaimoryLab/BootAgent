import { Eye, EyeOff, KeyRound } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useI18n } from "../i18n";

export function SecureKeyField({ value, onChange, resetKey }: {
  value: string;
  onChange: (value: string) => void;
  /**
   * Clears the field when it changes. The wizard passes a constant `value=""`,
   * so the effect below cannot see a reset: the prop it depends on never
   * changes. That left a key the user typed for one Provider still displayed
   * after switching to another, while the ref behind it had been cleared -- the
   * field showed a secret that was no longer going to be saved.
   */
  resetKey?: string;
}) {
  const { t } = useI18n();
  const [visible, setVisible] = useState(false);
  // The wizard keeps the key in a ref, not in state, so the parent gives no
  // re-render guarantee per keystroke; echo must come from local state.
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  // Skips its own first run: on mount `draft` is already the incoming value, and
  // clearing here would discard the saved key the Provider editor loads.
  const seenReset = useRef(resetKey);
  useEffect(() => {
    if (seenReset.current === resetKey) return;
    seenReset.current = resetKey;
    setDraft("");
    setVisible(false);
  }, [resetKey]);
  return (
    <div className="field-stack">
      <label htmlFor="api-key">API Key</label>
      <div className="secure-field">
        <KeyRound size={17} aria-hidden="true" />
        <input
          id="api-key"
          type={visible ? "text" : "password"}
          value={draft}
          onChange={(event) => {
            setDraft(event.target.value);
            onChange(event.target.value);
          }}
          autoComplete="off"
          spellCheck={false}
          autoCorrect="off"
          autoCapitalize="none"
          placeholder={t("粘贴你的 API Key")}
        />
        <button type="button" onClick={() => setVisible((current) => !current)} aria-label={visible ? t("隐藏密钥") : t("显示密钥")}>
          {visible ? <EyeOff size={17} /> : <Eye size={17} />}
        </button>
      </div>
      <small>{t("密钥只发送到当前本机服务，并保存在本机私有配置中")}</small>
    </div>
  );
}
