import { ExternalLink, FlaskConical, Link2 } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { useI18n } from "../i18n";
import { desktopProtocol, profileAgentIdForDesktop, selectedDesktopApp } from "../state/desktopSetup";
import { useWizard } from "../state/WizardContext";
import type { ProtocolId, ProviderId } from "../types/api";
import { PROTOCOL_LABELS } from "../types/api";

export function ProviderKeyPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch } = useWizard();
  const providerMeta = state.status?.providers[state.provider];
  const apiBaseUrl = providerMeta?.base_url || "";
  const providerHasKey = Boolean(providerMeta?.has_key);
  const canProbe = providerHasKey;
  const desktop = state.setupKind === "desktop" && state.status
    ? selectedDesktopApp(state.status, state.selectedAgentIds)
    : undefined;
  const probeAgentIds = desktop
    ? [profileAgentIdForDesktop(desktop)]
    : state.selectedAgentIds;
  // Continuing requires a successful probe, not just a non-empty key: a wrong
  // key must not reach the model step. canProbe stays separate so the test
  // button remains clickable while the verdict is still outstanding.
  const canContinue = canProbe;

  // The selected Agents decide which protocols get tested; a model that serves
  // Chat Completions may still refuse Responses, so do not imply a single one.
  const protocols = useMemo(() => {
    if (desktop) return [desktopProtocol(desktop) as ProtocolId].filter(Boolean);
    const byId = new Map(state.status?.catalog.map((item) => [item.id, item]) ?? []);
    const selected = probeAgentIds
      .map((id) => byId.get(id)?.protocol)
      .filter((value): value is ProtocolId => Boolean(value));
    return [...new Set(selected)].sort();
  }, [desktop, probeAgentIds, state.status]);

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
        apiBaseUrl: "",
        // The backend resolves an empty request key from the saved Provider.
        apiKey: "",
        // This is only a connectivity hint. The model selected on the next
        // step remains the Profile's model and is never edited here.
        model: state.probeModel,
        agents: probeAgentIds,
      });
      dispatch({ type: "CONNECTION_RESULT", result });
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", failure: describeError(error, t("连接测试失败")) });
    }
  };

  const openRegistration = async () => {
    // Either URL is enough; the backend applies the same precedence and does the
    // scheme validation, so no URL is passed from here.
    if (!providerMeta?.key_management_url && !providerMeta?.home) return;
    try {
      await api.openRegister(state.provider, probeAgentIds);
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", failure: describeError(error, t("无法打开注册页面")) });
    }
  };

  return (
    <PageScaffold
      title={t("连接模型服务")}
      description={t("将使用 Provider 已保存的 Key")}
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
        protocol={protocols.length === 1 ? protocols[0] : ""}
      />

      <div className="provider-form">
        {!providerHasKey ? (
          <div className="notice notice-warning">
            <span>{t("这个 Provider 还没有 Key，先到 Provider 页面填写")}</span>
            <button className="button button-secondary" type="button" onClick={() => navigate(`/providers?returnTo=${encodeURIComponent("/setup/provider")}`)}>
              <ExternalLink size={15} />
              {t("前往 Provider")}
            </button>
          </div>
        ) : null}
        <div className="provider-identity-row">
          <div>
            <strong>{providerMeta?.name}</strong>
            <span>{providerMeta?.base_url}</span>
          </div>
          {providerMeta?.key_management_url || providerMeta?.home ? (
            <button className="button button-secondary" type="button" onClick={() => void openRegistration()}>
              <ExternalLink size={15} />
              {t("获取 API Key")}
            </button>
          ) : null}
        </div>

        <div className="field-stack">
          <label htmlFor="provider-probe-model">{t("自定义模型名称（可选）")}</label>
          <input
            id="provider-probe-model"
            className="text-field"
            value={state.probeModel}
            onChange={(event) => dispatch({ type: "SET_PROBE_MODEL", value: event.target.value })}
            placeholder={t("例如 deepseek/deepseek-v4-pro")}
            spellCheck={false}
          />
          <small>{t("可选，仅用于测试连接；实际配置模型在下一步选择")}</small>
        </div>

        <div className="connection-row">
          <button className="button button-secondary" type="button" onClick={() => void testConnection()} disabled={!canProbe || state.connectionState === "loading"}>
            <FlaskConical size={16} />
            {t("测试连接")}
          </button>
          <ConnectionStatus state={state.connectionState} result={state.connection} />
        </div>
        {providerHasKey && state.connectionState === "idle" && (
          <small>{t("连接测试是可选的，可以直接继续选择模型")}</small>
        )}
      </div>
    </PageScaffold>
  );
}
