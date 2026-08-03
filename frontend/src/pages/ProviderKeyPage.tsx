import { ExternalLink, FlaskConical, Link2 } from "lucide-react";
import { useEffect, useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import { PROTOCOL_LABELS } from "../types/api";
import type { ProtocolId, ProviderId } from "../types/api";

export function ProviderKeyPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, secret } = useWizard();
  const providerMeta = state.status?.providers[state.provider];
  const apiBaseUrl = providerMeta?.base_url || "";
  const canProbe = state.hasApiKey;
  // Continuing requires a successful probe, not just a non-empty key: a wrong
  // key must not reach the model step. canProbe stays separate so the test
  // button remains clickable while the verdict is still outstanding.
  const canContinue = canProbe && state.keyVerified;

  useEffect(() => {
    if (!providerMeta?.has_key) return;
    let active = true;
    void api.getProvider(state.provider)
      .then((entry) => {
        if (active) secret.setApiKey(entry.api_key);
      })
      .catch((error) => {
        if (active) dispatch({ type: "CONNECTION_FAILED", failure: describeError(error, t("无法读取已保存的 API Key")) });
      });
    return () => { active = false; };
  }, [dispatch, providerMeta?.has_key, secret.setApiKey, state.provider]);
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
    secret.clearApiKey();
    dispatch({ type: "SET_PROVIDER", value: provider });
  };

  const testConnection = async () => {
    dispatch({ type: "CONNECTION_LOADING" });
    try {
      const result = await api.probe({
        provider: state.provider,
        apiBaseUrl: "",
        apiKey: secret.keyRef.current,
        // A user-supplied ID lets providers without model discovery validate
        // the model that will actually be configured.
        model: state.model,
        agents: state.selectedAgentIds,
      });
      dispatch({ type: "CONNECTION_RESULT", result });
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", failure: describeError(error, t("连接测试失败")) });
    }
  };

  const openRegistration = async () => {
    if (!providerMeta?.home) return;
    try {
      await api.openRegister(state.provider, state.selectedAgentIds);
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", failure: describeError(error, t("无法打开注册页面")) });
    }
  };

  return (
    <PageScaffold
      title={t("连接模型服务")}
      description={t("Key 不会进入日志、URL 或前端持久化状态。")}
      stepper
      onBack={() => navigate("/setup/agents")}
      primaryLabel={t("继续选择模型")}
      onPrimary={() => navigate("/setup/model")}
      primaryDisabled={!canContinue || state.connectionState === "loading"}
      footerNote={endpoint ? <span className="endpoint-note"><Link2 size={14} />{endpoint}</span> : undefined}
    >
      <ProviderSegment
        value={state.provider}
        providers={state.status?.providers ?? {}}
        onAdd={() => navigate(`/providers/new?returnTo=${encodeURIComponent("/setup/provider")}`)}
        onChange={changeProvider}
      />

      <div className="provider-form">
        <div className="provider-identity-row">
          <div>
            <strong>{providerMeta?.name}</strong>
            <span>{providerMeta?.base_url}</span>
          </div>
          {providerMeta?.home ? (
            <button className="button button-secondary" type="button" onClick={() => void openRegistration()}>
              <ExternalLink size={15} />
              {t("注册并获取 Key")}
            </button>
          ) : null}
        </div>

        <div className="field-stack">
          <label htmlFor="provider-model">{t("自定义模型名称（可选）")}</label>
          <input
            id="provider-model"
            className="text-field"
            value={state.model}
            onChange={(event) => dispatch({ type: "SET_MODEL", value: event.target.value })}
            placeholder={t("例如 deepseek/deepseek-v3")}
            spellCheck={false}
          />
          <small>{t("填写后将用此模型测试连接；留空时自动选择。")}</small>
        </div>

        <SecureKeyField value={secret.keyRef.current} onChange={secret.setApiKey} />

        <div className="connection-row">
          <button className="button button-secondary" type="button" onClick={() => void testConnection()} disabled={!canProbe || state.connectionState === "loading"}>
            <FlaskConical size={16} />
            {t("测试连接")}
          </button>
          <ConnectionStatus state={state.connectionState} result={state.connection} />
        </div>
        {canProbe && state.connectionState === "idle" && (
          <small>{t("连接测试通过后才能继续选择模型。")}</small>
        )}
      </div>
    </PageScaffold>
  );
}
