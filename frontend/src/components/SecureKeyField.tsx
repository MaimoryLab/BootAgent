import { Eye, EyeOff, KeyRound } from "lucide-react";
import { useState } from "react";

export function SecureKeyField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="field-stack">
      <label htmlFor="api-key">API Key</label>
      <div className="secure-field">
        <KeyRound size={17} aria-hidden="true" />
        <input
          id="api-key"
          type={visible ? "text" : "password"}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          autoComplete="off"
          spellCheck={false}
          placeholder="粘贴你的 API Key"
        />
        <button type="button" onClick={() => setVisible((current) => !current)} aria-label={visible ? "隐藏密钥" : "显示密钥"}>
          {visible ? <EyeOff size={17} /> : <Eye size={17} />}
        </button>
      </div>
      <small>密钥只发送到当前本机服务，并写入确认页列出的本地配置。</small>
    </div>
  );
}
