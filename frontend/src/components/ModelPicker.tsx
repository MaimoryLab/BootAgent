import { Check, Search } from "lucide-react";
import { useMemo } from "react";

import { useI18n } from "../i18n";

interface ModelPickerProps {
  models: string[];
  value: string;
  onChange: (value: string) => void;
  inputId?: string;
  inputLabel?: string;
  required?: boolean;
}

export function ModelPicker({ models, value, onChange, inputId = "manual-model", inputLabel, required }: ModelPickerProps) {
  const { t } = useI18n();
  const filtered = useMemo(
    () => models.filter((model) => model.toLocaleLowerCase().includes(value.trim().toLocaleLowerCase())),
    [models, value],
  );

  return (
    <div className="model-picker">
      <div className="field-stack manual-model-field">
        <label htmlFor={inputId}>{inputLabel || t("手动输入模型 ID")}</label>
        {models.length ? (
          <div className="search-field">
            <Search size={17} />
            <input id={inputId} value={value} onChange={(event) => onChange(event.target.value)} placeholder={t("搜索模型")} aria-label={inputLabel || t("搜索模型")} spellCheck={false} autoCorrect="off" autoCapitalize="none" required={required} />
          </div>
        ) : (
          <input id={inputId} className="text-field" value={value} onChange={(event) => onChange(event.target.value)} placeholder={t("例如 gpt-4.1")} spellCheck={false} autoCorrect="off" autoCapitalize="none" required={required} />
        )}
      </div>
      {models.length ? (
        <div className="model-list" role="radiogroup" aria-label={t("模型列表")}>
          {filtered.map((model) => (
            <button
              type="button"
              role="radio"
              aria-checked={model === value}
              className={`model-row${model === value ? " is-selected" : ""}`}
              key={model}
              onClick={() => onChange(model)}
            >
              <span>
                <strong>{model}</strong>
                <small>OpenAI-compatible model</small>
              </span>
              {model === value ? <Check size={17} /> : null}
            </button>
          ))}
          {!filtered.length ? <div className="empty-row">{t("没有匹配的模型")}</div> : null}
        </div>
      ) : null}
    </div>
  );
}
