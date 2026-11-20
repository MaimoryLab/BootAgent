import { ArrowLeftRight, ExternalLink, FlaskConical, Link2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderModelPicker } from "../components/ProviderModelPicker";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { useI18n } from "../i18n";
import { useFeedback } from "../state/FeedbackContext";
import { probeOffersConversion, providerSuggestsConversion } from "../state/conversion";
import { desktopProtocol, profileAgentIdForDesktop, selectedDesktopApp } from "../state/desktopSetup";
import { useWizard } from "../state/WizardContext";
import { wizardNeedsModel } from "../state/wizardReducer";
import type { ProtocolId, ProviderId } from "../types/api";
import { PROTOCOL_LABELS } from "../types/api";

export function ProviderKeyPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, secret, refreshStatus } = useWizard();
  const [savingKey, setSavingKey] = useState(false);
  const feedback = useFeedback();
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

  // Compatibility mode is offered where activation can carry it out: a
  // command-line Agent, or Claude Desktop, whose adapter path already exists.
  // Other desktop apps share another Agent's settings and stay as they are.
  const conversionProtocol = protocols.length === 1 && (!desktop || desktop.id === "claude-desktop") ? protocols[0] : undefined;
  const conversionOffered = state.connectionState === "error" && probeOffersConversion(state.connection, conversionProtocol);
  // Read from the Provider's protocol table, with no request sent. It speaks
  // only until a connection test has: a test's verdict replaces the guess.
  const conversionSuggested = state.connectionState === "idle" && providerSuggestsConversion(providerMeta, conversionProtocol);
  const agentName = desktop?.name || state.status?.catalog.find((item) => item.id === state.selectedAgentIds[0])?.name || "";
  // A new key is persisted before continuing; probing remains optional. A test
  // that found compatibility mode would help is the exception: continuing past
  // it unanswered installs an Agent that fails on its first request. The
  // untested suggestion is not: nothing has shown the Provider cannot serve it.
  const canContinue = (canProbe || (builtInProvider && state.hasApiKey)) && (!conversionOffered || state.conversion);

  const endpoint = useMemo(() => {
    if (!apiBaseUrl) return "";
    const base = apiBaseUrl.replace(/\/$/, "");
    if (!protocols.length) return `${base}/v1/chat/completions`;
    if (state.conversion) return `${base} · ${t("兼容模式")}`;
    return `${base} · ${protocols.map((p) => PROTOCOL_LABELS[p]).join(" + ")}`;
  }, [apiBaseUrl, protocols, state.conversion, t]);

  const changeProvider = (provider: ProviderId) => {
    secret.clearApiKey();
    dispatch({ type: "SET_PROVIDER", value: provider });
  };

  // Where the provider step hands off: the model step, unless every selected
  // Agent owns its own model choice, in which case there is nothing to ask and
  // the wizard goes straight to confirm.
  const needsModel = wizardNeedsModel(state);
  const afterProvider = needsModel ? "/setup/model" : "/setup/review";

  const continueSetup = async () => {
    // Only when there is nothing to save. Returning early on providerHasKey alone
    // discarded a key typed in this session -- the field is rendered when no key
    // is saved yet, so a save that lands mid-visit (a probe, another tab) turned
    // the user's input into a silent no-op.
    if (providerHasKey && !secret.keyRef.current) {
      navigate(afterProvider);
      return;
    }
    if (!providerMeta || !builtInProvider || !secret.keyRef.current) return;
    setSavingKey(true);
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
      navigate(afterProvider);
    } catch (error) {
      feedback.failure(error, t("无法保存模型服务"), { key: "setup:provider" });
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
      stepper
      onBack={() => navigate("/setup/agents")}
      // The next step is the model unless the Agent owns that choice, in which
      // case the label would be promising a screen the wizard is about to skip.
      primaryLabel={needsModel ? t("继续选择模型") : t("继续确认")}
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
        // A Provider with only an OpenAI address can still serve Claude Code
        // here: the connection test will offer compatibility mode for it.
        convertible={Boolean(conversionProtocol)}
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
          required={false}
          preferDropUp
        />

        <div className="connection-row">
          <button className="button button-secondary" type="button" onClick={() => void testConnection()} disabled={!canProbe || state.connectionState === "loading"}>
            <FlaskConical size={16} />
            {t("测试连接")}
          </button>
          {/* Replaced rather than joined: the plain failure would tell the user
              to switch API type, which the wizard has no control for, right
              beside the one choice that fixes it. */}
          {conversionOffered ? null : <ConnectionStatus state={state.connectionState} result={state.connection} />}
        </div>
        {conversionSuggested && !state.conversion ? (
          <div className="notice notice-info conversion-offer">
            <span>
              {t("尚未确认 {provider} 支持 {agent} 的请求格式（{protocol}），可以改用兼容模式，由 BootAgent 在本机转换", {
                provider: providerMeta?.name ?? "",
                agent: agentName,
                protocol: conversionProtocol ? PROTOCOL_LABELS[conversionProtocol] : "",
              })}
            </span>
            <div className="conversion-offer-actions">
              <button className="button button-secondary" type="button" onClick={() => dispatch({ type: "SET_CONVERSION", value: true })}>
                <ArrowLeftRight size={15} aria-hidden="true" />
                {t("使用兼容模式")}
              </button>
            </div>
          </div>
        ) : null}
        {conversionOffered && !state.conversion ? (
          <div className="notice notice-warning conversion-offer" role="alert">
            <span>{t("{agent} 不能直接使用这个模型服务，BootAgent 可以在本机转换后使用", { agent: agentName })}</span>
            <div className="conversion-offer-actions">
              <button className="button button-primary" type="button" onClick={() => dispatch({ type: "SET_CONVERSION", value: true })}>
                <ArrowLeftRight size={15} aria-hidden="true" />
                {t("使用兼容模式")}
              </button>
              <button className="button button-secondary" type="button" onClick={() => document.getElementById("provider-select")?.focus()}>
                {t("换一个模型服务")}
              </button>
            </div>
          </div>
        ) : null}
        {state.conversion ? (
          <div className="notice notice-success conversion-offer" role="status">
            <span>{t("已选择兼容模式，使用 {agent} 时需要保持 BootAgent 运行", { agent: agentName })}</span>
            <div className="conversion-offer-actions">
              <button className="button button-secondary" type="button" onClick={() => dispatch({ type: "SET_CONVERSION", value: false })}>
                {t("不用兼容模式")}
              </button>
            </div>
          </div>
        ) : null}
      </div>
    </PageScaffold>
  );
}
