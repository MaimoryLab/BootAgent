import { useEffect, useState } from "react";

import { api, describeFailure } from "../backend/api";
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
  /**
   * grid-column: 1 / -1, which only means anything inside the Profile editor
   * grid. Off by default so a caller outside that grid is not handed a stray
   * declaration.
   */
  wide?: boolean;
}

export function ProviderModelPicker({ provider, protocol, hasKey, value, onChange, inputId, inputLabel, hint, required = true, wide = false }: ProviderModelPickerProps) {
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
      setModels(result.models ?? []);
      setSuccess(result.ok);
      setMessage(result.message.replace(/^Found (\d+) models\.$/, (_, count: string) => t("找到 {count} 个模型", { count })));
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
      {loading ? <div className="loading-block"><span className="spinner" />{t("正在读取模型列表")}</div> : null}
      {message && !loading ? <div className={`notice ${success ? "notice-success" : "notice-warning"}`}>{message}</div> : null}
      {/* collapsible: every caller is a compact form where an always-open list
          pushed the footer off screen. The arrow is also the only affordance that
          said the discovered models were selectable at all. */}
      <ModelPicker models={models} value={value} onChange={onChange} inputId={inputId} inputLabel={inputLabel || t("模型")} required={required} collapsible />
      {hint ? <small>{hint}</small> : null}
    </div>
  );
}
