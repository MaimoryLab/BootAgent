import { Search, X } from "lucide-react";
import { useRef } from "react";

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
  return (
    <div role="search" className="management-search">
      <Search size={15} aria-hidden="true" className="management-search-glyph" />
      <input
        type="text"
        value={value}
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
          onValueChange(nextValue);
        }}
        onChange={(event) => {
          const nextValue = event.target.value;
          if (composingRef.current || (event.nativeEvent as InputEvent).isComposing) return;
          if (suppressNextChangeRef.current === nextValue) {
            suppressNextChangeRef.current = null;
            return;
          }
          suppressNextChangeRef.current = null;
          onValueChange(nextValue);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape" && value) {
            event.stopPropagation();
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
      {value ? (
        <button type="button" className="icon-button management-search-clear" onClick={() => onValueChange("")} title={t("清空搜索")} aria-label={t("清空搜索")}>
          <X size={14} />
        </button>
      ) : null}
    </div>
  );
}
