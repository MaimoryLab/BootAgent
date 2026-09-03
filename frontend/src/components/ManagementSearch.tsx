import { Search, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useI18n } from "../i18n";

interface ManagementSearchProps {
  value: string;
  onValueChange: (value: string) => void;
  placeholder: string;
}

/** Shared, presentation-only search field for the Skills and MCP lists. */
export function ManagementSearch({ value, onValueChange, placeholder }: ManagementSearchProps) {
  const { t } = useI18n();
  const composingRef = useRef(false);
  const suppressNextChangeRef = useRef<string | null>(null);
  const [draftValue, setDraftValue] = useState(value);

  useEffect(() => {
    if (!composingRef.current) setDraftValue(value);
  }, [value]);
  return (
    <div role="search" className="management-search">
      <Search size={15} aria-hidden="true" className="management-search-glyph" />
      <input
        type="text"
        value={draftValue}
        onCompositionStart={() => {
          composingRef.current = true;
          suppressNextChangeRef.current = null;
        }}
        onCompositionEnd={(event) => {
          composingRef.current = false;
          const nextValue = event.currentTarget.value;
          // Chromium dispatches one ordinary input/change immediately after
          // compositionend. Remember the committed value so it is not sent to
          // the parent twice and does not trigger a duplicate URL/query update.
          suppressNextChangeRef.current = nextValue;
          setDraftValue(nextValue);
          onValueChange(nextValue);
        }}
        onChange={(event) => {
          const nextValue = event.target.value;
          setDraftValue(nextValue);
          if (composingRef.current) return;
          if (suppressNextChangeRef.current === nextValue) {
            suppressNextChangeRef.current = null;
            return;
          }
          suppressNextChangeRef.current = null;
          onValueChange(nextValue);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape" && draftValue) {
            event.stopPropagation();
            setDraftValue("");
            onValueChange("");
          }
        }}
        placeholder={placeholder}
        aria-label={placeholder}
        autoComplete="off"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
      />
      {draftValue ? (
        <button type="button" className="icon-button management-search-clear" onClick={() => { setDraftValue(""); onValueChange(""); }} title={t("清空搜索")} aria-label={t("清空搜索")}>
          <X size={14} />
        </button>
      ) : null}
    </div>
  );
}
