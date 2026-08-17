import { Search, X } from "lucide-react";

import { useI18n } from "../i18n";

interface ManagementSearchProps {
  value: string;
  onValueChange: (value: string) => void;
  placeholder: string;
}

/** Shared, presentation-only search field for the Skills and MCP lists. */
export function ManagementSearch({ value, onValueChange, placeholder }: ManagementSearchProps) {
  const { t } = useI18n();
  return (
    <div role="search" className="management-search">
      <Search size={15} aria-hidden="true" className="management-search-glyph" />
      <input
        type="text"
        value={value}
        onChange={(event) => onValueChange(event.target.value)}
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
