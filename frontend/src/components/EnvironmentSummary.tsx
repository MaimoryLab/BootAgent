import { Bot, CheckCircle2, Cpu, Layers3, ShieldCheck } from "lucide-react";

import type { EnvironmentProfile, ProbeResponse } from "../types/api";

export function EnvironmentSummary({ environment, probe }: { environment: EnvironmentProfile; probe: ProbeResponse | null }) {
  const items = [
    { label: "Provider", value: environment.provider, icon: Layers3 },
    { label: "模型", value: environment.model || "使用已有账号", icon: Cpu },
    { label: "Agent", value: `${environment.agent_ids.length} 个已激活`, icon: Bot },
    { label: "配置", value: environment.config_mode === "provider" ? "本地模型服务" : "官方账号 / 现有配置", icon: ShieldCheck },
  ];
  return (
    <div className="environment-summary">
      <div className="environment-ready">
        <CheckCircle2 size={28} />
        <div>
          <strong>开发环境已就绪</strong>
          <span>{probe?.ok ? "首次模型请求已验证" : "本机配置已完成"}</span>
        </div>
      </div>
      <div className="summary-grid">
        {items.map(({ label, value, icon: Icon }) => (
          <div className="summary-item" key={label}>
            <Icon size={18} />
            <span>{label}</span>
            <strong>{value}</strong>
          </div>
        ))}
      </div>
    </div>
  );
}
