import type { ProviderId } from "../types/api";

const providers: Array<{ id: ProviderId; label: string }> = [
  { id: "ppio", label: "PPIO" },
  { id: "novita", label: "Novita" },
  { id: "custom", label: "自定义" },
];

export function ProviderSegment({ value, onChange }: { value: ProviderId; onChange: (value: ProviderId) => void }) {
  return (
    <div className="provider-segment" role="radiogroup" aria-label="模型服务">
      {providers.map((provider) => (
        <button
          key={provider.id}
          type="button"
          role="radio"
          aria-checked={value === provider.id}
          className={value === provider.id ? "is-selected" : ""}
          onClick={() => onChange(provider.id)}
        >
          {provider.label}
        </button>
      ))}
    </div>
  );
}
