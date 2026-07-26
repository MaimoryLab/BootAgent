import { AlertCircle, CheckCircle2, LoaderCircle, Radio, ShieldAlert } from "lucide-react";

import type { AsyncState } from "../state/wizardReducer";
import type { ProbeResponse } from "../types/api";

export function ConnectionStatus({ state, result }: { state: AsyncState; result: ProbeResponse | null }) {
  if (state === "idle") {
    return (
      <div className="inline-status status-idle">
        <Radio size={17} />
        尚未测试连接
      </div>
    );
  }
  if (state === "loading") {
    return (
      <div className="inline-status status-loading" role="status">
        <LoaderCircle size={17} className="spin" />
        正在验证端点和 Key
      </div>
    );
  }
  if (result?.ok) {
    return (
      <div className="inline-status status-success" role="status">
        <CheckCircle2 size={17} />
        {result.message}
      </div>
    );
  }
  const rejected = result?.error_code === "API_KEY_REJECTED";
  return (
    <div className={`inline-status ${rejected ? "status-warning" : "status-error"}`} role="alert">
      {rejected ? <ShieldAlert size={17} /> : <AlertCircle size={17} />}
      {result?.message || "连接失败"}
    </div>
  );
}
