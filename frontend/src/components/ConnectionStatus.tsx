import { AlertCircle, CheckCircle2, LoaderCircle, Radio, ShieldAlert } from "lucide-react";

import { failureCopyFor, isHTTPStatus } from "../backend/failureCopy";
import { useI18n } from "../i18n";
import type { AsyncState } from "../state/wizardReducer";
import { PROTOCOL_LABELS, type ProbeResponse, type ProtocolId } from "../types/api";

export function ConnectionStatus({ state, result }: { state: AsyncState; result: ProbeResponse | null }) {
  const { t } = useI18n();
  if (state === "idle") {
    return (
      <div className="inline-status status-idle">
        <Radio size={17} />
        {t("尚未测试连接")}
      </div>
    );
  }
  if (state === "loading") {
    return (
      <div className="inline-status status-loading" role="status">
        <LoaderCircle size={17} className="spin" />
        {t("正在验证端点和 Key")}
      </div>
    );
  }
  // Every protocol the selected Agents need is probed concurrently, and one
  // failure used to overwrite the single reported result (internal/app/provider.go
  // :75-83). So a Provider that serves Chat Completions but refuses Responses
  // reported one failure naming one protocol, with no way to tell that the other
  // had passed or which of the user's Agents would actually work. Shown per
  // protocol whenever more than one was tested.
  const perProtocol = Object.entries(result?.protocols ?? {})
    .filter((entry): entry is [ProtocolId, ProbeResponse] => Boolean(entry[1]));
  const breakdown = perProtocol.length > 1 ? (
    <ul className="protocol-results">
      {perProtocol.map(([protocol, outcome]) => (
        <li key={protocol} className={outcome.ok ? "is-ok" : "is-failed"}>
          {outcome.ok ? <CheckCircle2 size={13} aria-hidden="true" /> : <AlertCircle size={13} aria-hidden="true" />}
          <strong>{PROTOCOL_LABELS[protocol] ?? protocol}</strong>
          <span>{outcome.ok ? t("可用") : failureCopyFor(outcome.error_code, outcome.status, t)?.message || t("不可用")}</span>
        </li>
      ))}
    </ul>
  ) : null;

  if (result?.ok) {
    return (
      <div className="inline-status status-success" role="status">
        <CheckCircle2 size={17} />
        <span>
          {result.message}
          {breakdown}
        </span>
      </div>
    );
  }
  const rejected = result?.error_code === "API_KEY_REJECTED"
    // 401/403 behind PROVIDER_UNREACHABLE is the same rejection: classifyHTTPModels
    // only tags the code when the models call is what failed.
    || (isHTTPStatus(result?.status) && (result?.status === 401 || result?.status === 403));
  // Localised copy replaces the backend's English sentence where a code maps to
  // any. Falls through to the raw message otherwise -- INVALID_REQUEST carries
  // field-level detail a generic sentence would lose.
  const copy = failureCopyFor(result?.error_code, result?.status, t);
  // A model BootAgent chose is named in the failure. A Provider's catalogue is
  // mostly image, video and audio generators, and one of those rejecting a chat
  // payload otherwise reads as a broken key -- a wrong verdict about the user's
  // credentials. Not shown when the key itself was rejected, which is about the
  // key whatever model was used, nor when the user named the model themselves.
  const blamedModel = !rejected && result?.auto_selected_model ? result.model : "";
  return (
    <div className={`inline-status ${rejected ? "status-warning" : "status-error"}`} role="alert">
      {rejected ? <ShieldAlert size={17} /> : <AlertCircle size={17} />}
      <span>
        {copy?.message || result?.message || t("连接失败")}
        {/* The auto-selected model explains the failure, so it wins over the
            generic hint: a hint about the key is wrong when the model is at fault. */}
        {blamedModel ? (
          <small>{t("测试使用的模型 {model} 由 BootAgent 自动选择，可能不支持对话。可在上方自定义模型名称后重试", { model: blamedModel })}</small>
        ) : copy?.hint ? <small>{copy.hint}</small> : null}
        {breakdown}
      </span>
    </div>
  );
}
