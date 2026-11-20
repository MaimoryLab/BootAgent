import { useEffect, useState } from "react";

import { api, describeFailure } from "../backend/api";
import { modelListMessage } from "../backend/failureCopy";
import { useI18n } from "../i18n";
import { ModelPicker } from "./ModelPicker";

interface ProviderModelPickerProps {
  provider: string;
  protocol: string;
  hasKey: boolean;
  value: string;
  onChange: (value: string) => void;
  inputId: string;
  /** Overrides the field label; defaults to the Profile editors' "模型". */
  inputLabel?: string;
  /** The line under the field, for a caller whose model means something else. */
  hint?: string;
  /**
   * Profile editors need a model to save. The wizard's connection step does not:
   * the field there only picks what the probe requests, and leaving it empty lets
   * the backend choose.
   */
  required?: boolean;
  /** Committed-on-blur callers pass this through to the field. */
  onBlur?: () => void;
  /** Keep the popup above the field when a following action row must stay clear. */
  preferDropUp?: boolean;
  /**
   * grid-column: 1 / -1, which only means anything inside the Profile editor
   * grid. Off by default so a caller outside that grid is not handed a stray
   * declaration.
   */
  wide?: boolean;
}

export function ProviderModelPicker({ provider, protocol, hasKey, value, onChange, inputId, inputLabel, hint, required = true, onBlur, preferDropUp = false, wide = false }: ProviderModelPickerProps) {
  const { t } = useI18n();
  const [models, setModels] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [success, setSuccess] = useState(false);

  useEffect(() => {
    if (!protocol || !hasKey) return;
    let cancelled = false;
    setLoading(true);
    setMessage("");
    void api.models({ provider, apiBaseUrl: "", apiKey: "" }).then((result) => {
      if (cancelled) return;
      const listed = modelListMessage(result, t);
      setModels(result.models ?? []);
      setSuccess(listed.ok);
      setMessage(listed.message);
    }).catch((error) => {
      if (cancelled) return;
      setModels([]);
      setSuccess(false);
      setMessage(describeFailure(error, t("无法获取模型列表"), t).message);
    }).finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => { cancelled = true; };
  }, [hasKey, protocol, provider, t]);

  return (
    <div className={`field-stack${wide ? " profile-editor-wide" : ""}`}>
      {/* collapsible: every caller is a compact form where an always-open list
          pushed the footer off screen. The arrow is also the only affordance that
          said the discovered models were selectable at all. */}
      <ModelPicker models={models} value={value} onChange={onChange} onBlur={onBlur} inputId={inputId} inputLabel={inputLabel || t("模型")} required={required} collapsible preferDropUp={preferDropUp} />
      {/* The listing's status is a line in a reserved slot under the field
          (docs/ui-interaction-plan.md §3.5). As a loading block and then a
          notice above it, the field itself jumped down and back while the user
          was reaching for it. */}
      <small className={`model-list-status${loading ? "" : success ? " is-ok" : message ? " is-warning" : ""}`} aria-live="polite">
        {loading ? t("正在读取模型列表") : message}
      </small>
      {hint ? <small>{hint}</small> : null}
    </div>
  );
}
