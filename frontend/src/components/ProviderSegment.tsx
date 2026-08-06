import { Plus } from "lucide-react";

import { useI18n } from "../i18n";
import { byProviderCreatedAt } from "../state/ranking";
import type { ProtocolId, ProviderId, StatusResponse } from "../types/api";
import { SelectField } from "./SelectField";

export function ProviderSegment({
  value,
  providers,
  onAdd,
  onChange,
  protocol,
}: {
  value: ProviderId;
  providers: StatusResponse["providers"];
  onAdd: () => void;
  onChange: (value: ProviderId) => void;
  protocol?: ProtocolId | "";
}) {
  const { t } = useI18n();
  return (
    <div className="provider-picker">
      {/* A span, not a label: htmlFor only associates with form elements, and the
          trigger is a button. SelectField carries the accessible name itself. */}
      <span className="provider-picker-label">{t("模型服务")}</span>
      <div className="provider-picker-control">
        <SelectField
          id="provider-select"
          label={t("模型服务")}
          value={value}
          onChange={onChange}
          options={byProviderCreatedAt(providers).map(([id, provider]) => ({
            value: id,
            label: provider.name,
            disabled: Boolean(protocol) && (protocol === "anthropic" ? !provider.anthropic_base_url : !provider.base_url),
          }))}
        />
        <button className="provider-add-button" type="button" onClick={onAdd} aria-label={t("新增 Provider")} title={t("新增 Provider")}>
          <Plus size={17} />
        </button>
      </div>
    </div>
  );
}
