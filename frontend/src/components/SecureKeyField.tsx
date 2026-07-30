import { Eye, EyeOff, KeyRound } from "lucide-react";
import { useEffect, useRef, useState } from "react";

/**
 * The API key input.
 *
 * Uncontrolled on purpose. An earlier version mirrored the key into local state
 * so it could echo per keystroke, which put the credential in two places the
 * product promises it will not be: React's component state, and -- because a
 * controlled input's value is a prop React writes through to the DOM -- the
 * serialised markup, where anything reading the page sees it in plain text.
 *
 * It also meant the field could not be cleared. `clearApiKey()` resets the
 * wizard's ref, but the field's own copy survived it, so the key stayed on screen
 * after an install completed and across navigation away and back. One call site
 * worked around that by remounting the component with a changing `key`; the other
 * did not, and nothing failed either way.
 *
 * Uncontrolled means the DOM node is the only place the characters live. `register`
 * hands that node to the secret store so a clear can empty it, which is what makes
 * clearing actually clear.
 */
export function SecureKeyField({
  initialValue = "",
  onChange,
  register,
}: {
  // initialValue repopulates the field on mount from wherever the caller holds the
  // key, so navigating away and back does not silently leave an empty field while
  // the key is still held -- which would show a disabled-looking form that
  // nonetheless probes successfully. Assigned imperatively rather than passed as a
  // value prop: assigning .value sets the property without React writing a `value`
  // attribute, so the characters stay out of the serialised markup.
  initialValue?: string;
  onChange: (value: string) => void;
  register?: (node: HTMLInputElement | null) => void;
}) {
  const [visible, setVisible] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const node = inputRef.current;
    if (node && initialValue && !node.value) {
      node.value = initialValue;
    }
    register?.(node);
    return () => register?.(null);
    // initialValue is read once on mount by design; later changes come from typing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [register]);

  return (
    <div className="field-stack">
      <label htmlFor="api-key">API Key</label>
      <div className="secure-field">
        <KeyRound size={17} aria-hidden="true" />
        <input
          id="api-key"
          ref={inputRef}
          type={visible ? "text" : "password"}
          onChange={(event) => onChange(event.target.value)}
          autoComplete="off"
          spellCheck={false}
          placeholder="粘贴你的 API Key"
        />
        <button
          type="button"
          onClick={() => setVisible((current) => !current)}
          aria-label={visible ? "隐藏密钥" : "显示密钥"}
        >
          {visible ? <EyeOff size={17} /> : <Eye size={17} />}
        </button>
      </div>
      <small>密钥只发送到当前本机服务，并写入确认页列出的本地配置。</small>
    </div>
  );
}
