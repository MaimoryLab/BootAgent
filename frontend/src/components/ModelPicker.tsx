import { Check, Search } from "lucide-react";
import { useMemo, useState } from "react";

import { useI18n } from "../i18n";

interface ModelPickerProps {
  models: string[];
  value: string;
  onChange: (value: string) => void;
}

export function ModelPicker({ models, value, onChange }: ModelPickerProps) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const filtered = useMemo(
    () => models.filter((model) => model.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())),
    [models, query],
  );

  return (
    <div className="model-picker">
      {models.length ? (
        <>
          <div className="search-field">
            <Search size={17} />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("搜索模型")} aria-label={t("搜索模型")} />
          </div>
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
        </>
      ) : null}

      <div className="field-stack manual-model-field">
        <label htmlFor="manual-model">{models.length ? t("或手动输入模型 ID") : t("手动输入模型 ID")}</label>
        <input id="manual-model" className="text-field" value={value} onChange={(event) => onChange(event.target.value)} placeholder={t("例如 gpt-4.1")} />
      </div>
    </div>
  );
}
