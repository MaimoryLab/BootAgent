import { Eye, EyeOff, KeyRound } from "lucide-react";
import { useEffect, useState } from "react";

import { useI18n } from "../i18n";

export function SecureKeyField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const { t } = useI18n();
  const [visible, setVisible] = useState(false);
  // The wizard keeps the key in a ref, not in state, so the parent gives no
  // re-render guarantee per keystroke; echo must come from local state.
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
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
