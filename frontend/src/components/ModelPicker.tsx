import { Check, Search } from "lucide-react";
import { useMemo, useState } from "react";

interface ModelPickerProps {
  models: string[];
  value: string;
  onChange: (value: string) => void;
}

export function ModelPicker({ models, value, onChange }: ModelPickerProps) {
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
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索模型" aria-label="搜索模型" />
          </div>
          <div className="model-list" role="radiogroup" aria-label="模型列表">
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
            {!filtered.length ? <div className="empty-row">没有匹配的模型</div> : null}
          </div>
        </>
      ) : null}

      <div className="field-stack manual-model-field">
        <label htmlFor="manual-model">{models.length ? "或手动输入模型 ID" : "手动输入模型 ID"}</label>
        <input id="manual-model" className="text-field" value={value} onChange={(event) => onChange(event.target.value)} placeholder="例如 gpt-4.1" />
      </div>
    </div>
  );
}
