import { ExternalLink, FlaskConical, Link2 } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { useWizard } from "../state/WizardContext";
import { PROTOCOL_LABELS } from "../types/api";
import type { ProtocolId, ProviderId } from "../types/api";

export function ProviderKeyPage() {
  const navigate = useNavigate();
  const { state, dispatch, secret } = useWizard();
  const providerMeta = state.provider === "custom" ? null : state.status?.providers[state.provider];
  const apiBaseUrl = state.provider === "custom" ? state.customBaseUrl : providerMeta?.base_url || "";
  const canProbe = state.hasApiKey && (state.provider !== "custom" || Boolean(state.customBaseUrl.trim()));
  // Continuing requires a successful probe, not just a non-empty key: a wrong
  // key must not reach the model step. canProbe stays separate so the test
  // button remains clickable while the verdict is still outstanding.
  const canContinue = canProbe && state.connectionState === "success";
  // The selected Agents decide which protocols get tested; a model that serves
  // Chat Completions may still refuse Responses, so do not imply a single one.
  const protocols = useMemo(() => {
    const byId = new Map(state.status?.catalog.map((item) => [item.id, item]) ?? []);
    const selected = state.selectedAgentIds
      .map((id) => byId.get(id)?.protocol)
      .filter((value): value is ProtocolId => Boolean(value));
    return [...new Set(selected)].sort();
  }, [state.status, state.selectedAgentIds]);

  const endpoint = useMemo(() => {
    if (!apiBaseUrl) return "";
    const base = apiBaseUrl.replace(/\/$/, "");
    if (!protocols.length) return `${base}/v1/chat/completions`;
    return `${base} · ${protocols.map((p) => PROTOCOL_LABELS[p]).join(" + ")}`;
  }, [apiBaseUrl, protocols]);

  const changeProvider = (provider: ProviderId) => {
    dispatch({ type: "SET_PROVIDER", value: provider });
  };

  const testConnection = async () => {
    dispatch({ type: "CONNECTION_LOADING" });
    try {
      const result = await api.probe({
        provider: state.provider,
        apiBaseUrl: state.provider === "custom" ? state.customBaseUrl : "",
        apiKey: secret.keyRef.current,
        // Empty before the model step; the backend picks its probe default.
        model: state.model,
        agents: state.selectedAgentIds,
      });
      dispatch({ type: "CONNECTION_RESULT", result });
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", failure: describeError(error, "连接测试失败") });
    }
  };

  const openRegistration = async () => {
    if (state.provider === "custom") return;
    try {
      await api.openRegister(state.provider, state.selectedAgentIds);
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", failure: describeError(error, "无法打开注册页面") });
    }
  };

  return (
    <PageScaffold
      title="连接模型服务"
      description="Key 不会进入日志、URL 或前端持久化状态。"
      stepper
      onBack={() => navigate("/setup/mode")}
      primaryLabel="继续选择模型"
      onPrimary={() => navigate("/setup/model")}
      primaryDisabled={!canContinue || state.connectionState === "loading"}
      footerNote={endpoint ? <span className="endpoint-note"><Link2 size={14} />{endpoint}</span> : undefined}
    >
      <ProviderSegment value={state.provider} onChange={changeProvider} />

      <div className="provider-form">
        {state.provider === "custom" ? (
          <div className="field-stack">
            <label htmlFor="custom-base-url">Base URL</label>
            <input
              id="custom-base-url"
              className="text-field"
              value={state.customBaseUrl}
              onChange={(event) => dispatch({ type: "SET_CUSTOM_BASE", value: event.target.value })}
              placeholder="https://models.example.com/openai"
              inputMode="url"
            />
            <small>支持 HTTP/HTTPS，包括用户主动配置的本机地址；URL 不能包含账号或密码。</small>
          </div>
        ) : (
          <div className="provider-identity-row">
            <div>
              <strong>{providerMeta?.name}</strong>
              <span>{providerMeta?.base_url}</span>
            </div>
            <button className="button button-secondary" type="button" onClick={() => void openRegistration()}>
              <ExternalLink size={15} />
              注册并获取 Key
            </button>
          </div>
        )}

        <SecureKeyField value={secret.keyRef.current} onChange={secret.setApiKey} />

        <div className="connection-row">
          <button className="button button-secondary" type="button" onClick={() => void testConnection()} disabled={!canProbe || state.connectionState === "loading"}>
            <FlaskConical size={16} />
            测试连接
          </button>
          <ConnectionStatus state={state.connectionState} result={state.connection} />
        </div>
        {canProbe && state.connectionState === "idle" && (
          <small>连接测试通过后才能继续选择模型。</small>
        )}
      </div>
    </PageScaffold>
  );
}
