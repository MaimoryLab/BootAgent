import { ExternalLink, FlaskConical, Link2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderModelPicker } from "../components/ProviderModelPicker";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { useI18n } from "../i18n";
import { desktopProtocol, profileAgentIdForDesktop, selectedDesktopApp } from "../state/desktopSetup";
import { useWizard } from "../state/WizardContext";
import type { ProtocolId, ProviderId } from "../types/api";
import { PROTOCOL_LABELS } from "../types/api";

export function ProviderKeyPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, secret, refreshStatus } = useWizard();
  const [savingKey, setSavingKey] = useState(false);
  const [saveFailure, setSaveFailure] = useState("");
  const providerMeta = state.status?.providers[state.provider];
  const apiBaseUrl = providerMeta?.base_url || "";
  const providerHasKey = Boolean(providerMeta?.has_key);
  const builtInProvider = Boolean(providerMeta && !providerMeta.custom);
  const canProbe = providerHasKey;
  const desktop = state.setupKind === "desktop" && state.status
    ? selectedDesktopApp(state.status, state.selectedAgentIds)
    : undefined;
  const probeAgentIds = desktop
    ? [profileAgentIdForDesktop(desktop)]
    : state.selectedAgentIds;
  // A new key is persisted before continuing; probing remains optional.
  const canContinue = canProbe || (builtInProvider && state.hasApiKey);

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
    secret.clearApiKey();
    setSaveFailure("");
    dispatch({ type: "SET_PROVIDER", value: provider });
  };

  const continueSetup = async () => {
    // Only when there is nothing to save. Returning early on providerHasKey alone
    // discarded a key typed in this session -- the field is rendered when no key
    // is saved yet, so a save that lands mid-visit (a probe, another tab) turned
    // the user's input into a silent no-op.
    if (providerHasKey && !secret.keyRef.current) {
      navigate("/setup/model");
      return;
    }
    if (!providerMeta || !builtInProvider || !secret.keyRef.current) return;
    setSavingKey(true);
    setSaveFailure("");
    try {
      await api.saveProvider({
        id: state.provider,
        name: providerMeta.name,
        home: providerMeta.home,
        base_url: providerMeta.base_url,
        anthropic_base_url: providerMeta.anthropic_base_url || "",
        api_key: secret.keyRef.current,
        create: false,
      });
      secret.clearApiKey();
      await refreshStatus();
      navigate("/setup/model");
    } catch (error) {
      setSaveFailure(describeFailure(error, t("无法保存模型服务"), t).message);
    } finally {
      setSavingKey(false);
    }
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
      dispatch({ type: "CONNECTION_FAILED", failure: describeFailure(error, t("连接测试失败"), t) });
    }
  };

  const openRegistration = async () => {
    // Either URL is enough; the backend applies the same precedence and does the
    // scheme validation, so no URL is passed from here.
    if (!providerMeta?.key_management_url && !providerMeta?.home) return;
    try {
      await api.openRegister(state.provider, probeAgentIds);
    } catch (error) {
      dispatch({ type: "CONNECTION_FAILED", failure: describeFailure(error, t("无法打开注册页面"), t) });
    }
  };

  return (
    <PageScaffold
      title={t("连接模型服务")}
      description={t("将使用模型服务已保存的 Key")}
      stepper
      onBack={() => navigate("/setup/agents")}
      primaryLabel={t("继续选择模型")}
      onPrimary={() => void continueSetup()}
      primaryDisabled={!canContinue || state.connectionState === "loading"}
      primaryBusy={savingKey}
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
        {!providerHasKey && builtInProvider ? (
          <SecureKeyField value="" onChange={secret.setApiKey} resetKey={state.provider} />
        ) : !providerHasKey ? (
          <div className="notice notice-warning">
            <span>{t("这个模型服务还没有 Key，先到模型服务页面填写")}</span>
            <button className="button button-secondary" type="button" onClick={() => navigate(`/providers?provider=${encodeURIComponent(state.provider)}&returnTo=${encodeURIComponent("/setup/provider")}`)}>
              <ExternalLink size={15} />
              {t("前往模型服务")}
            </button>
          </div>
        ) : null}
        {saveFailure ? <p className="agent-manage-error">{saveFailure}</p> : null}
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

        {/* The same discovery the Profile editors use, so the models this Key can
            reach are selectable here instead of having to be typed from memory.
            Not required: leaving it empty lets the backend pick a model for the
            probe, which is the common path. */}
        <ProviderModelPicker
          key={`${protocols.join(",")}:${state.provider}`}
          provider={state.provider}
          protocol={protocols[0] || ""}
          hasKey={providerHasKey}
          value={state.probeModel}
          onChange={(value) => dispatch({ type: "SET_PROBE_MODEL", value })}
          inputId="provider-probe-model"
          inputLabel={t("测试用模型（可选）")}
          hint={t("仅用于测试这个模型服务是否连得通，不会写入任何配置。真正使用的模型在下一步选择")}
          required={false}
        />

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
