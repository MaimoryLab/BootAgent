import { ChevronDown, ChevronUp, CircleHelp, Power } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import { api } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { StatusBadge } from "../components/StatusBadge";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import { agentsUsingConversion, providerDisplayName } from "../state/conversion";
import { confirmAction } from "../state/confirmDelete";
import { useFeedback } from "../state/FeedbackContext";
import type { ConversionConfig } from "../types/api";

/**
 * Compatibility mode: the local adapter that lets an Agent speaking its own
 * format use a Chat Completions-only model service.
 *
 * Mostly a status page. Agents are switched to it where the problem shows --
 * the setup wizard's connection test, an Agent's Profile list, a failed model
 * check -- so this page says what is running and, for each Agent, which model
 * service its requests go to. Each Agent has its own route; changing one is
 * done on that Agent's page and changes nothing for the others.
 */
export function ConversionPage() {
  const navigate = useNavigate();
  const { locale, t } = useI18n();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const [config, setConfig] = useState<ConversionConfig | null>(null);
  const [saved, setSaved] = useState<ConversionConfig | null>(null);
  const feedback = useFeedback();
  // Stable across receipts, unlike `feedback`, so the load effect can depend on it.
  const reportFailure = feedback.failure;
  const [saving, setSaving] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const autostartPrompted = useRef(false);
  const separator = locale === "en" ? ", " : "、";

  const maybeAskAutostart = async (enabled: boolean) => {
    if (!enabled || autostartPrompted.current) return;
    autostartPrompted.current = true;
    try {
      const settings = await api.getSettings();
      if (settings.autostart) return;
      if (!await confirmAction({
        title: t("开机自启动"),
        message: t("兼容模式需要 BootAgent 保持运行。要让 BootAgent 随系统启动吗？"),
        confirmLabel: t("开机自启动"),
        cancelLabel: t("暂不启用"),
      })) return;
      await api.saveSettings({ autostart: true });
    } catch (error) {
      feedback.failure(error, t("无法保存开机自启动设置"), { key: "autostart" });
    }
  };

  // Retryable from the receipt: a failed load leaves the page with nothing to
  // show, and the receipt is the only control on it.
  const load = useCallback(() => {
    void api.getConversion().then((loaded) => {
      setConfig(loaded);
      setSaved(loaded);
      void maybeAskAutostart(loaded.enabled);
    }).catch((error) => reportFailure(error, t("无法读取兼容模式设置"), { key: "conversion-load", action: { label: t("重试"), run: load } }));
    // maybeAskAutostart only reads a ref and stable calls.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reportFailure, t]);
  useEffect(load, [load]);

  const persist = async (next: ConversionConfig, done: string, fallback: string) => {
    setSaving(true);
    try {
      const result = await api.saveConversion(next);
      setConfig(result);
      setSaved(result);
      feedback.show({ key: "conversion", tone: "success", message: done });
      await maybeAskAutostart(result.enabled);
      void refreshStatus();
    } catch (error) {
      feedback.failure(error, fallback, { key: "conversion" });
    } finally {
      setSaving(false);
    }
  };

  const regenerateKey = async () => {
    setSaving(true);
    try {
      const result = await api.regenerateConversionKey();
      setConfig(result);
      setSaved(result);
      feedback.show({ key: "conversion-key", tone: "success", message: t("已重新生成本地 Key") });
      void refreshStatus();
    } catch (error) {
      feedback.failure(error, t("无法重新生成本地 Key"), { key: "conversion-key" });
    } finally {
      setSaving(false);
    }
  };

  const users = agentsUsingConversion(status);
  const dirty = Boolean(config && saved && (
    config.listen !== saved.listen
    || config.anthropic_model !== saved.anthropic_model
    || config.responses_model !== saved.responses_model
    || config.chat_model !== saved.chat_model
  ));

  const stop = async () => {
    if (!config) return;
    if (users.length && !await confirmAction({
      title: t("停止兼容模式"),
      message: t("停止后 {agents} 将无法使用，直到重新开启兼容模式或换一个配置模版。", { agents: users.map((user) => user.name).join(separator) }),
      confirmLabel: t("停止"),
      cancelLabel: t("取消"),
    })) return;
    await persist({ ...config, enabled: false }, t("兼容模式已停止"), t("无法停止兼容模式"));
  };

  // One primary action. Unsaved changes: save them, leaving it running or not.
  // Otherwise stopped: turn it on, which only makes sense once an Agent uses
  // it. Stopping is the secondary action, and only exists while it runs.
  const running = Boolean(config?.enabled);
  const savesChanges = dirty || running;
  const primaryLabel = savesChanges ? t("保存更改") : t("开启兼容模式");
  const onPrimary = () => {
    if (!config) return;
    if (savesChanges) void persist(config, t("已保存兼容模式设置"), t("无法保存兼容模式设置"));
    else void persist({ ...config, enabled: true }, t("兼容模式已开启"), t("无法开启兼容模式"));
  };

  return (
    <PageScaffold
      title={t("兼容模式")}
      description={t("让只支持特定格式的 Agent 也能用你的模型服务，在本机转换，不经过其他服务器。")}
      bodyClassName="management-page conversion-page"
      secondaryAction={running ? (
        <button className="button button-secondary conversion-action is-running" type="button" onClick={() => void stop()} disabled={saving}>
          <Power size={15} />{t("停止兼容模式")}
        </button>
      ) : null}
      primaryLabel={primaryLabel}
      onPrimary={onPrimary}
      primaryDisabled={!config || (savesChanges ? !dirty : users.length === 0)}
      primaryBusy={saving}
    >
      {config ? <div className="provider-editor conversion-editor">
        <section className={`conversion-status${running ? " is-running" : " is-stopped"}`} aria-label={t("兼容模式状态")}>
          <span className="conversion-status-dot" aria-hidden="true" />
          <div>
            <strong>{running ? t("兼容模式运行中") : t("兼容模式未开启")}</strong>
            <span>{running ? t("使用期间需要保持 BootAgent 运行") : users.length ? t("开启后这些 Agent 才能使用") : t("在 Agent 的配置页选择配置模版时会自动开启")}</span>
          </div>
        </section>

        <section className="conversion-agents" aria-labelledby="conversion-agents-title">
          <div className="conversion-section-heading">
            <h3 id="conversion-agents-title">{t("正在使用的 Agent")}</h3>
          </div>
          {users.length === 0 ? (
            <div className="conversion-empty-action">
              <span>{t("还没有 Agent 使用兼容模式")}</span>
              <Link to="/overview">{t("查看环境总览")}</Link>
            </div>
          ) : (
            <ul className="conversion-agent-list">
              {users.map((user) => (
                <li key={user.id}>
                  <span className="conversion-agent-name">{user.name}</span>
                  <span className="conversion-agent-protocol">
                    {user.target
                      ? `${status ? providerDisplayName(status.providers, user.target.provider, t) : user.target.provider} · ${user.target.model}`
                      : t("使用的配置模版已删除")}
                  </span>
                  {running ? <StatusBadge tone="success">{t("运行中")}</StatusBadge> : <StatusBadge tone="neutral">{t("已停止")}</StatusBadge>}
                  <button className="button button-secondary" type="button" onClick={() => navigate(`/agents/${encodeURIComponent(user.id)}`)} aria-label={t("为 {name} 更换模型", { name: user.name })}>
                    {t("更换")}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>

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
                <label>{t("本地鉴权")}</label>
                <div className="conversion-key-status">
                  <StatusBadge tone={config.has_api_key ? "success" : "neutral"}>
                    {config.has_api_key ? t("已安全生成") : t("启动时自动生成")}
                  </StatusBadge>
                  {config.has_api_key ? (
                    <button className="button button-secondary" type="button" onClick={() => void regenerateKey()} disabled={saving}>
                      {t("重新生成 Key")}
                    </button>
                  ) : null}
                </div>
              </div>
              {/* What each Agent is told the model is called. The model that
                  actually answers is the one chosen above. */}
              <div className="field-stack">
                <label htmlFor="conversion-anthropic-model">{t("Claude Code 看到的模型名")}</label>
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
                <label htmlFor="conversion-responses-model">{t("Codex 看到的模型名")}</label>
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
                <label htmlFor="conversion-chat-model">{t("其他 Agent 看到的模型名")}</label>
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

        <details className="conversion-guidance">
          <summary><CircleHelp size={15} />{t("了解兼容模式")}</summary>
          <div>
            <p>{t("Codex 发出的是 OpenAI Responses 格式，Claude Code 是 Anthropic Messages 格式，很多模型服务只支持 OpenAI Chat Completions。BootAgent 在本机把前两种转换成 Chat Completions，再发往每个 Agent 各自选择的模型。")}</p>
            <p>{t("每个 Agent 用自己的本机 Key 连接，互不影响；模型和 Key 仍由各自的配置模版管理。")}</p>
          </div>
        </details>
      </div> : null}
    </PageScaffold>
  );
}
