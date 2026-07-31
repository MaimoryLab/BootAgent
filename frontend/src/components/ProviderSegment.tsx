import { Plus } from "lucide-react";

import { useI18n } from "../i18n";
import type { ProviderId, StatusResponse } from "../types/api";

export function ProviderSegment({
  value,
  providers,
  onAdd,
  onChange,
}: {
  value: ProviderId;
  providers: StatusResponse["providers"];
  onAdd: () => void;
  onChange: (value: ProviderId) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="provider-picker">
      <label htmlFor="provider-select">{t("模型服务")}</label>
      <div className="provider-picker-control">
        <select id="provider-select" value={value} onChange={(event) => onChange(event.target.value)}>
          {Object.entries(providers)
            .sort(([first], [second]) => first.localeCompare(second))
            .map(([id, provider]) => <option key={id} value={id}>{provider.name}</option>)}
        </select>
        <button className="provider-add-button" type="button" onClick={onAdd} aria-label={t("新增 Provider")} title={t("新增 Provider")}>
          <Plus size={17} />
        </button>
      </div>
    </div>
  );
}
