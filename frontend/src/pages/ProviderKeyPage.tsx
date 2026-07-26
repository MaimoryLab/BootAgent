import { ExternalLink, FlaskConical, Link2 } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { api } from "../api/client";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { useWizard } from "../state/WizardContext";
import type { ProviderId } from "../types/api";

export function ProviderKeyPage() {
  const navigate = useNavigate();
  const { state, dispatch, secret } = useWizard();
  const providerMeta = state.provider === "custom" ? null : state.status?.providers[state.provider];
  const apiBaseUrl = state.provider === "custom" ? state.customBaseUrl : providerMeta?.base_url || "";
  const canContinue = state.hasApiKey && (state.provider !== "custom" || Boolean(state.customBaseUrl.trim()));
  const endpoint = useMemo(() => (apiBaseUrl ? `${apiBaseUrl.replace(/\/$/, "")}/v1/chat/completions` : ""), [apiBaseUrl]);

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
        model: state.model || "gpt-4.1",
      });
      dispatch({ type: "CONNECTION_RESULT", result });
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", message: error instanceof Error ? error.message : "连接测试失败" });
    }
  };

  const openRegistration = async () => {
    if (state.provider === "custom") return;
    try {
      await api.openRegister(state.provider, state.selectedAgentIds);
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", message: error instanceof Error ? error.message : "无法打开注册页面" });
    }
  };

  return (
    <PageScaffold
      title="连接模型服务"
      description="Key 不会进入日志、URL 或前端持久化状态。"
      step={3}
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
          <button className="button button-secondary" type="button" onClick={() => void testConnection()} disabled={!canContinue || state.connectionState === "loading"}>
            <FlaskConical size={16} />
            测试连接
          </button>
          <ConnectionStatus state={state.connectionState} result={state.connection} />
        </div>
      </div>
    </PageScaffold>
  );
}
