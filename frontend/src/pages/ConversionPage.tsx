import { ChevronDown, ChevronUp, Power } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { api, describeFailure } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { StatusBadge } from "../components/StatusBadge";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import { isConverterID } from "../state/conversion";
import { confirmAction } from "../state/confirmDelete";
import { PROTOCOL_LABELS, type ConversionConfig, type ProtocolId } from "../types/api";

export function ConversionPage() {
  const { t } = useI18n();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const [config, setConfig] = useState<ConversionConfig | null>(null);
  const [failure, setFailure] = useState("");
  const [saving, setSaving] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const autostartPrompted = useRef(false);

  const maybeAskAutostart = async (enabled: boolean) => {
    if (!enabled || autostartPrompted.current) return;
    autostartPrompted.current = true;
    try {
      const settings = await api.getSettings();
      if (settings.autostart) return;
      if (!await confirmAction({
        title: t("开机自启动"),
        message: t("API 转换已启用，是否让 BootAgent 随系统启动？"),
        confirmLabel: t("开机自启动"),
        cancelLabel: t("暂不启用"),
      })) return;
      await api.saveSettings({ autostart: true });
    } catch (error) {
      setFailure(describeFailure(error, t("无法保存开机自启动设置"), t).message);
    }
  };

  useEffect(() => {
    void api.getConversion().then((loaded) => {
      setConfig(loaded);
      void maybeAskAutostart(loaded.enabled);
    }).catch((error) => setFailure(describeFailure(error, t("无法读取格式转换设置"), t).message));
  }, [t]);

  const save = async () => {
    if (!config) return;
    setSaving(true);
    setFailure("");
    try {
      const saved = await api.saveConversion(config);
      setConfig(saved);
      await maybeAskAutostart(saved.enabled);
      void refreshStatus();
    }
    catch (error) { setFailure(describeFailure(error, t("无法保存格式转换设置"), t).message); }
    finally { setSaving(false); }
  };

  const toggle = async () => {
    if (!config) return;
    setSaving(true);
    setFailure("");
    try {
      const saved = await api.saveConversion({ ...config, enabled: !config.enabled });
      setConfig(saved);
      await maybeAskAutostart(saved.enabled);
      void refreshStatus();
    }
    catch (error) { setFailure(describeFailure(error, t("无法切换格式转换"), t).message); }
    finally { setSaving(false); }
  };

  const profiles = (status?.profiles ?? []).filter((profile) => profile.protocol === "openai" && profile.model);
  // Only auto-configured Agents can be pointed anywhere, and an Agent already
  // speaking Chat Completions has nothing to gain: the adapter converts *to* that
  // protocol, so listing one would suggest a benefit it cannot deliver.
  const adaptableAgents = (status?.catalog ?? []).filter(
    (agent) => agent.configMode === "auto" && agent.protocol && agent.protocol !== "openai",
  );
  const converterProfileIDs = new Set((status?.profiles ?? []).filter((profile) => isConverterID(profile.id)).map((profile) => profile.id));
  const usesAdapter = (agentID: string) => {
    const boundProfile = status?.agents[agentID]?.profileId;
    return Boolean(boundProfile && converterProfileIDs.has(boundProfile));
  };
  return (
    <PageScaffold
      title={t("API 协议适配")}
      description={t("让只支持某一种 API 协议的 Agent 也能用上这个模型服务")}
      bodyClassName="management-page conversion-page"
      footerNote={config ? <span className={`conversion-footer-status${config.enabled ? " is-running" : " is-stopped"}`}>{config.enabled ? t("适配服务运行中") : t("适配服务已停止")}</span> : null}
      secondaryAction={config ? <button className={`button button-secondary conversion-action${config.enabled ? " is-running" : " is-stopped"}`} type="button" onClick={() => void toggle()} disabled={saving || !config.target_profile}><Power size={15} />{config.enabled ? t("停止服务") : t("启动服务")}</button> : null}
      primaryLabel={t("保存设置")}
      onPrimary={() => void save()}
      primaryDisabled={!config || !config.target_profile}
      primaryBusy={saving}
    >
      {failure ? <p className="settings-field-error" role="status">{failure}</p> : null}
      {config ? <div className="provider-editor conversion-editor">
        <section className="conversion-explainer">
          <h3>{t("为什么需要它")}</h3>
          <p>{t("不同 Agent 要求的 API 协议不一样：Codex 只发 OpenAI Responses 请求，Claude Code 只发 Anthropic Messages 请求，OpenCode 和 Aider 发 Chat Completions 请求。而一个模型服务通常只支持其中一部分。")}</p>
          <p>{t("开启后，BootAgent 会在本机监听一个地址，把这三种协议的请求都翻译成 Chat Completions 再转发给下面选中的模型服务。这样协议不匹配的 Agent 也能连上它。")}</p>
        </section>

        <section className="conversion-agents">
          <h3>{t("哪些 Agent 会用到它")}</h3>
          {adaptableAgents.length === 0 ? (
            <p className="conversion-hint">{t("没有可用的 Agent，先在环境总览里安装一个")}</p>
          ) : (
            <ul className="conversion-agent-list">
              {adaptableAgents.map((agent) => (
                <li key={agent.id}>
                  <span className="conversion-agent-name">{agent.name}</span>
                  <span className="conversion-agent-protocol">
                    {PROTOCOL_LABELS[agent.protocol as ProtocolId] ?? agent.protocol ?? ""}
                  </span>
                  {usesAdapter(agent.id) ? <StatusBadge tone="success">{t("已指向适配器")}</StatusBadge> : null}
                </li>
              ))}
            </ul>
          )}
        </section>

        <div className="provider-editor-grid">
          <div className="field-stack provider-editor-wide">
            <label htmlFor="conversion-target">{t("请求最终发往")}</label>
            <SelectField
              id="conversion-target"
              label={t("请求最终发往")}
              value={config.target_profile}
              onChange={(value) => setConfig({ ...config, target_profile: value })}
              options={profiles.map((profile) => ({ value: profile.id, label: profile.label || profile.id }))}
            />
            {profiles.length === 0 && (
              <small className="conversion-hint">{t("还没有 Chat Completions 协议的配置模版，先去配置模版页建一个")}</small>
            )}
          </div>
        </div>

        <section className="conversion-advanced">
          <button
            type="button"
            className="button button-secondary conversion-advanced-toggle"
            onClick={() => setShowAdvanced(!showAdvanced)}
            aria-expanded={showAdvanced}
          >
            {showAdvanced ? <ChevronUp size={15} /> : <ChevronDown size={15} />}
            {t("高级设置")}
          </button>

          {showAdvanced ? (
            <div className="provider-editor-grid" style={{ marginTop: "1rem" }}>
              <div className="field-stack">
                <label htmlFor="conversion-listen">{t("监听地址")}</label>
                <input
                  id="conversion-listen"
                  value={config.listen}
                  onChange={(event) => setConfig({ ...config, listen: event.target.value })}
                  placeholder="127.0.0.1:8787"
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="none"
                />
              </div>
              <div className="field-stack">
                <label htmlFor="conversion-key">{t("本地 API Key")}</label>
                <input
                  id="conversion-key"
                  type="password"
                  value={config.api_key}
                  onChange={(event) => setConfig({ ...config, api_key: event.target.value })}
                  autoComplete="off"
                />
              </div>
              <div className="field-stack">
                <label htmlFor="conversion-anthropic-model">{t("Anthropic 模型")}<span className="conversion-field-note">{t("对 Claude Code 声明的名字")}</span></label>
                <input
                  id="conversion-anthropic-model"
                  value={config.anthropic_model}
                  onChange={(event) => setConfig({ ...config, anthropic_model: event.target.value })}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="none"
                />
              </div>
              <div className="field-stack">
                <label htmlFor="conversion-responses-model">{t("Responses 模型")}<span className="conversion-field-note">{t("对 Codex 声明的名字")}</span></label>
                <input
                  id="conversion-responses-model"
                  value={config.responses_model}
                  onChange={(event) => setConfig({ ...config, responses_model: event.target.value })}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="none"
                />
              </div>
              <div className="field-stack">
                <label htmlFor="conversion-chat-model">{t("Chat Completions 模型")}<span className="conversion-field-note">{t("对 OpenCode / Aider 声明的名字")}</span></label>
                <input
                  id="conversion-chat-model"
                  value={config.chat_model}
                  onChange={(event) => setConfig({ ...config, chat_model: event.target.value })}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="none"
                />
              </div>
            </div>
          ) : null}
        </section>
      </div> : null}
    </PageScaffold>
  );
}
