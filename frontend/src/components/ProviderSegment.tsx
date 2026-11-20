import { Plus } from "lucide-react";

import { useI18n } from "../i18n";
import { userProviders } from "../state/conversion";
import { byProviderCreatedAt } from "../state/ranking";
import type { ProtocolId, ProviderId, StatusResponse } from "../types/api";
import { SelectField } from "./SelectField";

/**
 * Whether a Provider has the endpoint `protocol` is sent to. Anthropic Messages
 * has an address of its own; both OpenAI protocols share the base URL.
 * `convertible` also accepts a Provider that only has the OpenAI address, for a
 * step where compatibility mode can bridge the difference.
 */
export function providerServes(provider: StatusResponse["providers"][string], protocol: ProtocolId | "" | undefined, convertible = false): boolean {
  if (!protocol) return true;
  if (protocol === "anthropic") return Boolean(provider.anthropic_base_url || (convertible && provider.base_url));
  return Boolean(provider.base_url);
}

export function ProviderSegment({
  value,
  providers,
  onAdd,
  onChange,
  protocol,
  convertible = false,
}: {
  value: ProviderId;
  providers: StatusResponse["providers"];
  onAdd: () => void;
  onChange: (value: ProviderId) => void;
  protocol?: ProtocolId | "";
  convertible?: boolean;
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
          // The adapter's own Providers are an implementation detail of
          // compatibility mode and are never chosen by hand. A Provider that
          // cannot serve this step stays listed and says why, rather than
          // greying out with no explanation.
          options={byProviderCreatedAt(userProviders(providers)).map(([id, provider]) => {
            const disabled = !providerServes(provider, protocol, convertible);
            return {
              value: id,
              label: disabled ? t("{name}（不支持此格式）", { name: provider.name }) : provider.name,
              disabled,
            };
          })}
        />
        <button className="provider-add-button" type="button" onClick={onAdd} aria-label={t("新增模型服务")} title={t("新增模型服务")}>
          <Plus size={17} />
        </button>
      </div>
    </div>
  );
}
