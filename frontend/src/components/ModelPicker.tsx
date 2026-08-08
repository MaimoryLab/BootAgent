import { Check, ChevronDown, Search } from "lucide-react";
import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";

import { useI18n } from "../i18n";

interface ModelPickerProps {
  models: string[];
  value: string;
  onChange: (value: string) => void;
  inputId?: string;
  inputLabel?: string;
  required?: boolean;
  /**
   * Hides the list behind a disclosure arrow, for the compact Profile editors.
   * The wizard's model step is a whole page about choosing one, so there the
   * list stays open.
   */
  collapsible?: boolean;
}

export function ModelPicker({ models, value, onChange, inputId = "manual-model", inputLabel, required, collapsible = false }: ModelPickerProps) {
  const { t } = useI18n();
  const listId = `${inputId}-list`;
  // The typed query is tracked apart from the committed model ID. They used to
  // be one string, so a prefilled value -- the Provider's default_model, which
  // the Profile editor always supplies -- was also a filter. If that model was
  // not among the ones discovered, the list rendered "no matching models"
  // directly under a notice saying how many had been found, and there was no way
  // to reach any of them.
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [dropUp, setDropUp] = useState(false);
  const fieldRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const listOpen = models.length > 0 && (!collapsible || open);

  const filtered = useMemo(
    () => models.filter((model) => model.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())),
    [models, query],
  );

  // Flips upward when the popup would not fit, the same measurement SelectField
  // makes: the editor's model field is the last row of the form, so the list
  // opens near the bottom of the scroll area more often than not.
  useLayoutEffect(() => {
    if (!collapsible || !listOpen) return;
    const field = fieldRef.current;
    const list = listRef.current;
    if (!field || !list) return;
    const box = field.getBoundingClientRect();
    setDropUp(box.bottom + list.offsetHeight + 16 > window.innerHeight);
  }, [collapsible, listOpen, filtered.length]);

  useEffect(() => {
    if (!collapsible || !open) return;
    // Bubble phase, and pointerdown rather than click, for the reasons given in
    // SelectField: on capture the event that opened the list closes it again.
    const onPointerDown = (event: PointerEvent) => {
      if (!fieldRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [collapsible, open]);

  const commit = (model: string) => {
    onChange(model);
    // Cleared so the list is whole again next time it opens. Leaving the query
    // behind would reproduce the bug this separation fixes.
    setQuery("");
    if (collapsible) setOpen(false);
  };

  const type = (next: string) => {
    setQuery(next);
    onChange(next);
    if (collapsible) setOpen(true);
  };

  const list = listOpen ? (
    <div
      ref={listRef}
      id={listId}
      className={`model-list${dropUp ? " is-above" : ""}`}
      role="radiogroup"
      aria-label={t("模型列表")}
    >
      {filtered.map((model) => (
        <button
          type="button"
          role="radio"
          aria-checked={model === value}
          className={`model-row${model === value ? " is-selected" : ""}`}
          key={model}
          onClick={() => commit(model)}
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
  ) : null;

  return (
    <div className="model-picker">
      <div className="field-stack manual-model-field">
        <label htmlFor={inputId}>{inputLabel || t("手动输入模型 ID")}</label>
        {/* Only the collapsible variant nests the list here, where it can be
            positioned against the field as a popup. The wizard's model step
            keeps it as a sibling below, laid out by .model-picker's own gap. */}
        {/* Escape is handled here rather than on the input: focus sits on the
            arrow after opening with it, and a key that only closed the list from
            one of the two controls would look broken from the other. */}
        <div
          className={`model-picker-field${collapsible ? " is-collapsible" : ""}`}
          ref={fieldRef}
          onKeyDown={(event) => {
            if (!collapsible || !open || event.key !== "Escape") return;
            event.preventDefault();
            setOpen(false);
          }}
        >
          {models.length ? (
            <div className="search-field">
              <Search size={17} />
              <input
                id={inputId}
                value={value}
                onChange={(event) => type(event.target.value)}
                onKeyDown={(event) => {
                  if (!collapsible || open || event.key !== "ArrowDown") return;
                  event.preventDefault();
                  setOpen(true);
                }}
                placeholder={t("搜索模型")}
                aria-label={inputLabel || t("搜索模型")}
                spellCheck={false}
                autoCorrect="off"
                autoCapitalize="none"
                required={required}
              />
              {collapsible ? (
                <button
                  type="button"
                  className="model-picker-toggle"
                  aria-expanded={open}
                  aria-controls={listId}
                  aria-label={open ? t("收起模型列表") : t("展开模型列表")}
                  title={open ? t("收起模型列表") : t("展开模型列表")}
                  onClick={() => {
                    // The query goes with the list: reopening after a filtered
                    // session should offer everything again, not the last search.
                    setQuery("");
                    setOpen((current) => !current);
                  }}
                >
                  <ChevronDown size={15} aria-hidden="true" />
                </button>
              ) : null}
            </div>
          ) : (
            <input id={inputId} className="text-field" value={value} onChange={(event) => onChange(event.target.value)} placeholder={t("例如 gpt-4.1")} spellCheck={false} autoCorrect="off" autoCapitalize="none" required={required} />
          )}
          {collapsible ? list : null}
        </div>
      </div>
      {collapsible ? null : list}
    </div>
  );
}
